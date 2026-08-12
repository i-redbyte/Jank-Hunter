package io.jankhunter.sample

import android.os.SystemClock
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterConfig
import java.io.File
import java.lang.reflect.InvocationTargetException
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.concurrent.thread
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class ArtTiHardeningTest {
    @Test
    fun negotiatedSignalsOverloadAndTeardownRemainFailOpen() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val context = instrumentation.targetContext
        val logDir = File(context.filesDir, "jankhunter-artti-hardening")
        val config = JankHunterConfig.fromManifest(context).toBuilder()
            .flushIntervalMs(100)
            .logDirectory(logDir)
            .retainedHeapDumpEnabled(false)
            .build()
        instrumentation.runOnMainSync {
            JankHunter.shutdown()
            logDir.deleteRecursively()
            assertTrue(logDir.mkdirs())
            JankHunter.init(context, config)
        }

        var acceptedByOverload = -1
        try {
            waitForAgent()
            forceGarbageCollections()
            churnThreads()
            createMonitorContention()
            instrumentation.runOnMainSync { SystemClock.sleep(MAIN_STALL_MS) }

            acceptedByOverload = publishSynthetic(AGENT_GC_INTERVAL, OVERLOAD_EVENTS)
            assertTrue("synthetic overload must be fail-open", acceptedByOverload in 0..OVERLOAD_EVENTS)
            assertTrue("bounded queue should reject at least one overload event", acceptedByOverload < OVERLOAD_EVENTS)

            val mainResponsive = AtomicBoolean(false)
            instrumentation.runOnMainSync { mainResponsive.set(true) }
            assertTrue("main thread did not remain responsive after overload", mainResponsive.get())
            SystemClock.sleep(DRAIN_SETTLE_MS)
            instrumentation.runOnMainSync { JankHunter.flush() }
            SystemClock.sleep(FLUSH_SETTLE_MS)
        } finally {
            instrumentation.runOnMainSync { JankHunter.shutdown() }
        }

        val events = CommittedAgentEventScanner.scan(waitForLog(logDir))
        val types = events.mapTo(linkedSetOf()) { it.type }
        assertTrue("agent lifecycle/status missing: $types", AGENT_STATUS in types)
        val activeCapabilities = events.lastOrNull { it.type == AGENT_CAPABILITY }?.payload3 ?: 0L
        assertTrue("no negotiated active capabilities", activeCapabilities != 0L)
        assertNegotiatedSignal(activeCapabilities, CAP_GC, "GC") {
            events.any { it.type == AGENT_GC_INTERVAL && it.payload1 > 0L }
        }
        assertNegotiatedSignal(activeCapabilities, CAP_THREADS, "thread start/end") {
            AGENT_THREAD_START in types && AGENT_THREAD_END in types
        }
        assertNegotiatedSignal(activeCapabilities, CAP_CONTENTION, "monitor contention") {
            events.any { it.type == AGENT_CONTENTION_INTERVAL && it.payload1 >= MIN_EXPECTED_CONTENTION_NS }
        }
        assertNegotiatedSignal(activeCapabilities, CAP_STACKS, "triggered stack") {
            AGENT_STACK_SAMPLE in types && AGENT_STACK_DEFINITION in types
        }
        val quality = events.lastOrNull { it.type == AGENT_QUALITY }
            ?: failWith("final native quality snapshot missing")
        assertTrue("overload loss not visible in quality: $quality", quality.payload1 > 0L || quality.payload2 > 0L)
        assertTrue("queue high-watermark not recorded: $quality", quality.payload0 > 0L)
        assertTrue("clean native stopped status missing", events.any { it.type == AGENT_STATUS && it.payload0 == STATUS_STOPPED && it.payload1 == 0L })
        assertTrue("test did not exercise overload", acceptedByOverload >= 0)
    }

    private fun waitForAgent() {
        val deadline = SystemClock.elapsedRealtime() + ATTACH_TIMEOUT_MS
        while (SystemClock.elapsedRealtime() < deadline) {
            if (runCatching { publishSynthetic(AGENT_GC_INTERVAL, 0) }.getOrDefault(-1) == 0) return
            SystemClock.sleep(POLL_MS)
        }
        fail("ART TI agent did not become active")
    }

    private fun forceGarbageCollections() {
        repeat(3) {
            val pressure = Array(32) { ByteArray(128 * 1024) }
            assertEquals(32, pressure.size)
            Runtime.getRuntime().gc()
            SystemClock.sleep(GC_SETTLE_MS)
        }
    }

    private fun churnThreads() {
        repeat(THREAD_WAVES) { wave ->
            val workers = List(THREADS_PER_WAVE) { index ->
                thread(name = "JH-churn-$wave-$index") { Thread.yield() }
            }
            workers.forEach(Thread::join)
        }
        SystemClock.sleep(EVENT_SETTLE_MS)
    }

    private fun createMonitorContention() {
        val monitor = Any()
        val holderEntered = CountDownLatch(1)
        val waiterEntered = CountDownLatch(1)
        val releaseWaiter = CountDownLatch(1)
        val holder = thread(name = "JH-monitor-holder") {
            synchronized(monitor) {
                holderEntered.countDown()
                SystemClock.sleep(CONTENTION_HOLD_MS)
            }
        }
        assertTrue(holderEntered.await(2, TimeUnit.SECONDS))
        val waiter = thread(name = "JH-monitor-waiter") {
            synchronized(monitor) {
                waiterEntered.countDown()
                releaseWaiter.await(2, TimeUnit.SECONDS)
            }
        }
        holder.join()
        assertTrue(waiterEntered.await(2, TimeUnit.SECONDS))
        SystemClock.sleep(STACK_CAPTURE_SETTLE_MS)
        releaseWaiter.countDown()
        waiter.join()
        SystemClock.sleep(EVENT_SETTLE_MS)
    }

    private fun publishSynthetic(type: Int, count: Int): Int {
        val bridge = Class.forName("io.jankhunter.artti.internal.ArtTiNativeBridge")
        val instance = bridge.getDeclaredField("INSTANCE").get(null)
        val method = bridge.getDeclaredMethod("nativePublishSynthetic", Int::class.java, Int::class.java)
        return try {
            method.invoke(instance, type, count) as Int
        } catch (failure: InvocationTargetException) {
            throw failure.targetException
        }
    }

    private fun waitForLog(directory: File): File {
        val deadline = SystemClock.elapsedRealtime() + LOG_TIMEOUT_MS
        while (SystemClock.elapsedRealtime() < deadline) {
            val file = directory.listFiles { candidate -> candidate.extension == "jhlog" && candidate.length() > 0 }
                ?.maxByOrNull(File::lastModified)
            if (file != null) return file
            SystemClock.sleep(POLL_MS)
        }
        fail("no committed .jhlog in ${directory.absolutePath}")
        throw AssertionError("unreachable")
    }

    private fun assertNegotiatedSignal(capabilities: Long, bit: Long, label: String, predicate: () -> Boolean) {
        if (capabilities and bit != 0L) assertTrue("negotiated $label signal missing", predicate())
    }

    private fun failWith(message: String): Nothing {
        fail(message)
        throw AssertionError("unreachable")
    }

    private companion object {
        const val AGENT_STATUS = 1
        const val AGENT_CAPABILITY = 2
        const val AGENT_QUALITY = 3
        const val AGENT_THREAD_START = 4
        const val AGENT_THREAD_END = 5
        const val AGENT_GC_INTERVAL = 6
        const val AGENT_CONTENTION_INTERVAL = 7
        const val AGENT_STACK_SAMPLE = 8
        const val AGENT_STACK_DEFINITION = 9
        const val STATUS_STOPPED = 5L
        const val CAP_GC = 1L
        const val CAP_THREADS = 1L shl 1
        const val CAP_CONTENTION = 1L shl 2
        const val CAP_STACKS = 1L shl 3
        const val OVERLOAD_EVENTS = 100_000
        const val THREAD_WAVES = 4
        const val THREADS_PER_WAVE = 16
        const val MAIN_STALL_MS = 300L
        const val CONTENTION_HOLD_MS = 180L
        const val MIN_EXPECTED_CONTENTION_NS = 8_000_000L
        const val ATTACH_TIMEOUT_MS = 10_000L
        const val LOG_TIMEOUT_MS = 5_000L
        const val DRAIN_SETTLE_MS = 2_000L
        const val FLUSH_SETTLE_MS = 500L
        const val STACK_CAPTURE_SETTLE_MS = 750L
        const val EVENT_SETTLE_MS = 300L
        const val GC_SETTLE_MS = 250L
        const val POLL_MS = 100L
    }
}
