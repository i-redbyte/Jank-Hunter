package io.jankhunter.runtime

import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.system.MainThreadWatchdog
import java.io.File
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.locks.LockSupport
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class MainThreadStallArtTest {
    @Test
    fun realMainLooperRecoveryCompletesTheAlreadyVisibleIncident() = exercise(recover = true)

    @Test
    fun stoppingWithRealMainLooperBlockedPreservesInterruptedEvidence() = exercise(recover = false)

    private fun exercise(recover: Boolean) {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val label = if (recover) "recovered" else "interrupted"
        val directory = File(instrumentation.targetContext.cacheDir, "stall-$label-${System.nanoTime()}")
        val config = JankHunterConfig.builder().autoStartCollectors(false).flushIntervalMs(60_000L).build()
        val writer = AsyncLogWriterFactory().open(directory, config, "main")
        val graph = RuntimeComponentGraph(
            nowMs = { SystemClock.elapsedRealtime() },
            nowUs = { SystemClock.elapsedRealtimeNanos() / 1_000L },
        )
        graph.state.config = config
        graph.state.writer = writer
        graph.state.mainThreadContext = JankHunterContext("art.Screen", "art.Owner", 42L)
        val events = CopyOnWriteArrayList<Pair<Long, MainThreadStallState>>()
        val ongoing = CountDownLatch(1)
        val terminal = CountDownLatch(1)
        val reporter = graph.contextTelemetry.bindMainThreadStallCallbacks()
        val callbacks = object : MainThreadStallCallbacks by reporter {
            override fun recordMainThreadStall(
                context: JankHunterContextSnapshot, stackHint: String?, durationMs: Long,
                incidentId: Long, state: MainThreadStallState,
            ) {
                reporter.recordMainThreadStall(context, stackHint, durationMs, incidentId, state)
                events.add(incidentId to state)
                if (state == MainThreadStallState.ONGOING) ongoing.countDown() else terminal.countDown()
            }
        }
        val watchdog = MainThreadWatchdog(100L, callbacks)
        val blocked = CountDownLatch(1)
        val release = CountDownLatch(1)
        val mainFinished = CountDownLatch(1)
        Handler(Looper.getMainLooper()).post {
            blocked.countDown()
            try {
                release.await(10L, TimeUnit.SECONDS)
            } finally {
                mainFinished.countDown()
            }
        }
        try {
            assertTrue(blocked.await(2L, TimeUnit.SECONDS))
            writer.counter("art.stall.bootstrap", 1L)
            assertTrue(writer.flushBlocking(1_000L))
            val log = directory.walkTopDown().single { it.isFile && it.extension == "jhlog" }
            val initialBytes = log.length()
            watchdog.start()
            assertTrue("No evidence while the Android main looper is blocked", ongoing.await(2L, TimeUnit.SECONDS))
            assertEquals(1L, mainFinished.count)
            val writeDeadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(1L)
            while (log.length() == initialBytes && System.nanoTime() < writeDeadline) {
                LockSupport.parkNanos(TimeUnit.MILLISECONDS.toNanos(1L))
            }
            assertTrue("Watchdog did not publish evidence without an external flush", log.length() > initialBytes)
            log.copyTo(File(instrumentation.context.filesDir, "stall-$label-open.jhlog"), overwrite = true)
            if (recover) {
                release.countDown()
                assertTrue(terminal.await(2L, TimeUnit.SECONDS))
            } else {
                assertTrue("Watchdog failed to stop within its bounded join", watchdog.stop(1_000L))
                assertEquals(1L, mainFinished.count)
                assertTrue(terminal.await(1L, TimeUnit.SECONDS))
            }
            assertTrue(watchdog.stop(1_000L))
            assertEquals(listOf(1L, 1L), events.map { it.first })
            assertEquals(listOf(MainThreadStallState.ONGOING,
                if (recover) MainThreadStallState.RECOVERED else MainThreadStallState.INTERRUPTED), events.map { it.second })
            assertTrue(writer.close(1_000L))
            log.copyTo(File(instrumentation.context.filesDir, "stall-$label-final.jhlog"), overwrite = true)
        } finally {
            release.countDown()
            watchdog.stop(1_000L)
            writer.close(1_000L)
            assertTrue(mainFinished.await(2L, TimeUnit.SECONDS))
            directory.deleteRecursively()
        }
    }
}
