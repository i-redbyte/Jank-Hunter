package io.jankhunter.runtime

import com.sun.management.ThreadMXBean
import java.lang.management.ManagementFactory
import java.util.concurrent.Executor
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test

class ExecutorTaskAllocationTest {
    @Test
    fun disabledExecutorAllocatesNoSdkTask() {
        assumeTrue(System.getProperty("jankhunter.benchmark") == "true")
        val bean = ManagementFactory.getThreadMXBean() as? ThreadMXBean
        assumeTrue(bean?.isThreadAllocatedMemorySupported == true)
        val allocation = checkNotNull(bean)
        allocation.isThreadAllocatedMemoryEnabled = true
        val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1L })
        graph.state.featureGate.activate(JankHunterConfig.builder().build())
        val delegate = LastTaskExecutor()
        val executor = JankHunterExecutor(delegate, "inactive", null, { 1L }, graph.asyncTelemetry)
        graph.state.featureGate.deactivate()
        var runs = 0
        val command = Runnable { runs++ }
        repeat(10_000) { executor.execute(command) }
        val threadId = Thread.currentThread().id
        val before = allocation.getThreadAllocatedBytes(threadId)
        repeat(100_000) { executor.execute(command) }
        val bytesPerTask = (allocation.getThreadAllocatedBytes(threadId) - before) / 100_000.0
        println("Inactive executor bytes/op=$bytesPerTask")
        delegate.last?.run()
        assertEquals(1, runs)
        assertTrue("disabled adapter allocated $bytesPerTask bytes/op", bytesPerTask <= 0.1)
    }

    @Test
    fun ordinaryExecutorDoesNotAllocateScheduledLifecycleState() {
        assumeTrue(System.getProperty("jankhunter.benchmark") == "true")
        val bean = ManagementFactory.getThreadMXBean() as? ThreadMXBean
        assumeTrue(bean?.isThreadAllocatedMemorySupported == true)
        val allocation = checkNotNull(bean)
        allocation.isThreadAllocatedMemoryEnabled = true
        val delegate = LastTaskExecutor()
        val executor = JankHunterExecutor(delegate, "allocation", null, { 1L }, activeExecutorTestCallbacks())
        var runs = 0
        val command = Runnable { runs++ }
        // Context capture has a longer warmup than the former queue-only adapter. Separate
        // transient startup allocations from the steady per-task guard; keep its bound unchanged.
        repeat(100_000) { executor.execute(command) }
        val threadId = Thread.currentThread().id
        val before = allocation.getThreadAllocatedBytes(threadId)
        repeat(100_000) { executor.execute(command) }
        val bytesPerTask = (allocation.getThreadAllocatedBytes(threadId) - before) / 100_000.0
        println("Executor queued task bytes/op=$bytesPerTask")
        delegate.last?.run()
        assertEquals(1, runs)
        assertTrue("ordinary task allocated scheduled-only state: $bytesPerTask bytes/op", bytesPerTask <= 40.1)
    }

    private class LastTaskExecutor : Executor {
        @Volatile var last: Runnable? = null
        override fun execute(command: Runnable) { last = command }
    }
}
