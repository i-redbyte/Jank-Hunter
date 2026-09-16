package io.jankhunter.runtime.integration

import androidx.metrics.performance.FrameData
import androidx.metrics.performance.JankStats
import com.sun.management.ThreadMXBean
import java.lang.management.ManagementFactory
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test

class JankStatsFrameAllocationTest {
    @Test
    fun forwardingReusedFramesDoesNotAllocatePerCallback() {
        assumeTrue(System.getProperty("jankhunter.benchmark") == "true")
        val bean = ManagementFactory.getThreadMXBean() as? ThreadMXBean
        assumeTrue(bean?.isThreadAllocatedMemorySupported == true)
        val allocation = checkNotNull(bean)
        allocation.isThreadAllocatedMemoryEnabled = true
        var count = 0L
        var duration = 0L
        var jank = 0L
        val listener = checkNotNull(JankHunterJankStats.createFrameListener { isJank, nanos ->
            count++
            duration += nanos
            if (isJank) jank++
        }) as JankStats.OnFrameListener
        val frames = arrayOf(FrameData(1L, 16_000_000L, false, emptyList()), FrameData(2L, 24_000_000L, true, emptyList()))
        repeat(10_000) { listener.onFrame(frames[it and 1]) }
        val threadId = Thread.currentThread().id
        val before = allocation.getThreadAllocatedBytes(threadId)
        val start = System.nanoTime()
        repeat(100_000) { listener.onFrame(frames[it and 1]) }
        val nanosPerFrame = (System.nanoTime() - start) / 100_000.0
        val bytesPerFrame = (allocation.getThreadAllocatedBytes(threadId) - before) / 100_000.0
        println("JankStats callback ns/op=$nanosPerFrame bytes/op=$bytesPerFrame")
        assertEquals(110_000L, count)
        assertEquals(55_000L, jank)
        assertEquals(2_200_000_000_000L, duration)
        assertTrue("JankStats allocates on every frame: $bytesPerFrame bytes/op", bytesPerFrame <= 0.1)
    }
}
