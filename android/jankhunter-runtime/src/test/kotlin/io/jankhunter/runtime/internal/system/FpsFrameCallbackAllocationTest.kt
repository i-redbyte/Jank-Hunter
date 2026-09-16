package io.jankhunter.runtime.internal.system

import com.sun.management.ThreadMXBean
import io.jankhunter.runtime.RuntimeCollectorCallbacks
import java.lang.management.ManagementFactory
import java.lang.reflect.Proxy
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test

class FpsFrameCallbackAllocationTest {
    @Test
    fun mainCallbackDoesNotAllocateADispatchClosure() = measure(main = true, maximumBytes = 0.1)

    @Test
    fun workerCallbackAllocatesOnlyOneQueuedSnapshot() = measure(main = false, maximumBytes = 56.1)

    private fun measure(main: Boolean, maximumBytes: Double) {
        assumeTrue(System.getProperty("jankhunter.benchmark") == "true")
        val bean = ManagementFactory.getThreadMXBean() as? ThreadMXBean
        assumeTrue(bean?.isThreadAllocatedMemorySupported == true)
        val allocation = checkNotNull(bean)
        allocation.isThreadAllocatedMemoryEnabled = true
        var frames = 0L
        val proxy = Proxy.newProxyInstance(
            RuntimeCollectorCallbacks::class.java.classLoader,
            arrayOf(RuntimeCollectorCallbacks::class.java),
        ) { _, method, args ->
            if (method.name == "recordUiWindow") frames += checkNotNull(args)[2] as Long
            null
        }
        val monitor = FpsMonitor(
            Long.MAX_VALUE, 16L, checkNotNull(RuntimeCollectorCallbacks::class.java.cast(proxy)), exactAdmission = true,
            mainThread = object : FpsMainThreadDispatcher {
                @Volatile private var queued: Runnable? = null
                override fun isMainThread(): Boolean = main
                override fun post(task: Runnable): Boolean {
                    queued = task
                    checkNotNull(queued).run()
                    queued = null
                    return true
                }
            },
            nanoTime = { 1_000_000_000L },
        )
        monitor.start()
        monitor.setJankStatsActive(true)
        monitor.setWindowActive(true)
        repeat(10_000) { monitor.onJankStatsFrame("screen", 16_000_000L, false) }
        val threadId = Thread.currentThread().id
        val before = allocation.getThreadAllocatedBytes(threadId)
        repeat(100_000) { monitor.onJankStatsFrame("screen", 16_000_000L, false) }
        val bytes = (allocation.getThreadAllocatedBytes(threadId) - before) / 100_000.0
        assertTrue(monitor.stop())
        assertEquals(110_000L, frames)
        println("FPS callback main=$main bytes/op=$bytes")
        assertTrue("redundant callback closures: $bytes bytes/op", bytes <= maximumBytes)
    }
}
