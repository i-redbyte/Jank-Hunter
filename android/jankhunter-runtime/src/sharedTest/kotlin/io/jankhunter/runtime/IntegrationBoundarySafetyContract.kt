package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.AsyncWriterTerminalObserver
import io.jankhunter.runtime.internal.io.LogQualityCounters
import java.io.File
import java.io.IOException
import java.util.concurrent.Callable
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

/** Identical native queue/future and writer-deadlock regressions run on JVM and ART. */
abstract class IntegrationBoundarySafetyContract {
    protected abstract fun newDirectory(name: String): File

    @Test
    fun rejectedSubmittedFutureIsCancelledByTheNativeHandler() {
        val started = CountDownLatch(1)
        val release = CountDownLatch(1)
        val pool = java.util.concurrent.ThreadPoolExecutor(1, 1, 0L, TimeUnit.MILLISECONDS,
            java.util.concurrent.SynchronousQueue(), java.util.concurrent.RejectedExecutionHandler { task, _ ->
                (task as? java.util.concurrent.Future<*>)?.cancel(false)
            })
        val graph = RuntimeComponentGraph({ 1L }, { 1L })
        graph.state.featureGate.activate(JankHunterConfig.builder().build())
        try {
            pool.execute { started.countDown(); check(release.await(5, TimeUnit.SECONDS)) }
            assertTrue(started.await(5, TimeUnit.SECONDS))
            val observed = checkNotNull(graph.asyncTelemetry.wrapExecutorService(pool, "cancel-rejected", null))
            val result = observed.submit(Callable { 42 })
            assertTrue("SDK hid the Future from the cancellation handler; get() would never finish", result.isCancelled)
        } finally {
            release.countDown()
            pool.shutdownNow()
        }
    }

    @Test
    fun explicitPriorityComparatorSeesOriginalTaskObjects() {
        val started = CountDownLatch(1)
        val release = CountDownLatch(1)
        val complete = CountDownLatch(2)
        val order = java.util.Collections.synchronizedList(mutableListOf<Int>())
        val queue = java.util.concurrent.PriorityBlockingQueue<Runnable>(11,
            Comparator { left, right -> (left as ComparatorTask).priority.compareTo((right as ComparatorTask).priority) })
        val pool = java.util.concurrent.ThreadPoolExecutor(1, 1, 0L, TimeUnit.MILLISECONDS, queue)
        val graph = RuntimeComponentGraph({ 1L }, { 1L })
        graph.state.featureGate.activate(JankHunterConfig.builder().build())
        try {
            pool.execute { started.countDown(); check(release.await(5, TimeUnit.SECONDS)) }
            assertTrue(started.await(5, TimeUnit.SECONDS))
            val observed = checkNotNull(graph.asyncTelemetry.wrapExecutor(pool, "priority-comparator", null))
            observed.execute(ComparatorTask(2) { order.add(2); complete.countDown() })
            observed.execute(ComparatorTask(1) { order.add(1); complete.countDown() })
            release.countDown()
            assertTrue(complete.await(5, TimeUnit.SECONDS))
            assertEquals(listOf(1, 2), order)
        } finally {
            release.countDown()
            pool.shutdownNow()
        }
    }

    @Test
    fun serializableCommandKeepsTheContractExpectedByItsExecutor() {
        var calls = 0
        val command = SerializableTask { calls++ }
        val delegate = java.util.concurrent.Executor { task ->
            assertSame(command, task)
            assertTrue(task is java.io.Serializable)
            task.run()
        }
        val graph = RuntimeComponentGraph({ 1L }, { 1L })
        graph.state.featureGate.activate(JankHunterConfig.builder().build())
        JankHunterExecutor(delegate, "serializable", null, { 1L }, graph.asyncTelemetry).execute(command)
        assertEquals(1, calls)
    }

    private class ComparatorTask(val priority: Int, private val action: () -> Unit) : Runnable {
        override fun run() = action()
    }

    private class SerializableTask(private val action: () -> Unit) : Runnable, java.io.Serializable {
        override fun run() = action()
    }

    @Test
    fun customNativeTaskFactoryRemainsInControlOfSubmissionAndFutureIdentity() {
        val factories = java.util.concurrent.atomic.AtomicInteger()
        val delegate = object : java.util.concurrent.ThreadPoolExecutor(1, 1, 0L, TimeUnit.MILLISECONDS,
            java.util.concurrent.LinkedBlockingQueue()) {
            override fun <T> newTaskFor(callable: Callable<T>): java.util.concurrent.RunnableFuture<T> {
                factories.incrementAndGet()
                return NativeFuture(callable)
            }
        }
        val graph = RuntimeComponentGraph({ 1L }, { 1L })
        graph.state.featureGate.activate(JankHunterConfig.builder().build())
        try {
            val observed = checkNotNull(graph.asyncTelemetry.wrapExecutorService(delegate, "custom", null))
            val future = observed.submit(Callable { 42 })
            assertEquals(42, future.get(5, TimeUnit.SECONDS))
            assertEquals("SDK bypassed the application's protected task factory", 1, factories.get())
            assertTrue(future is NativeFuture<*>)
            assertSame(delegate, observed)
        } finally {
            delegate.shutdownNow()
        }
    }

    private class NativeFuture<T>(callable: Callable<T>) : java.util.concurrent.FutureTask<T>(callable)

    @Test
    fun priorityExecutorPreservesComparableTasksAndTheirOrdering() {
        val release = CountDownLatch(1)
        val started = CountDownLatch(1)
        val complete = CountDownLatch(2)
        val order = java.util.Collections.synchronizedList(mutableListOf<Int>())
        val delegate = java.util.concurrent.ThreadPoolExecutor(1, 1, 0L, TimeUnit.MILLISECONDS,
            java.util.concurrent.PriorityBlockingQueue<Runnable>())
        val graph = RuntimeComponentGraph({ 1L }, { 1L })
        graph.state.featureGate.activate(JankHunterConfig.builder().build())
        val wrapped = JankHunterExecutor(delegate, "priority", null, { 1L }, graph.asyncTelemetry)
        try {
            delegate.execute { started.countDown(); check(release.await(5, TimeUnit.SECONDS)) }
            assertTrue(started.await(5, TimeUnit.SECONDS))
            wrapped.execute(PriorityTask(2) { order.add(2); complete.countDown() })
            wrapped.execute(PriorityTask(1) { order.add(1); complete.countDown() })
            release.countDown()
            assertTrue(complete.await(5, TimeUnit.SECONDS))
            assertEquals(listOf(1, 2), order)
        } finally {
            release.countDown()
            delegate.shutdownNow()
        }
    }

    private class PriorityTask(val priority: Int, private val action: () -> Unit) : Runnable, Comparable<PriorityTask> {
        override fun compareTo(other: PriorityTask): Int = priority.compareTo(other.priority)
        override fun run() = action()
    }

    @Test
    fun closeDoesNotWaitForTerminalObserverNeedingTheCallersLifecycleLock() {
        val directory = newDirectory("terminal-observer-lock")
        val lifecycleLock = Any()
        val observerEntered = CountDownLatch(1)
        val observerCompleted = CountDownLatch(1)
        val writer = AsyncLogWriter(directory, JankHunterConfig.builder().build(), "app", setOf("app"), true,
            1L, "2026-10-15", 1L, { 1L }, LogQualityCounters(), prepareSession = { throw IOException("startup I/O failed") },
            onTerminalStop = { _, _, _ ->
                observerEntered.countDown()
                synchronized(lifecycleLock) { observerCompleted.countDown() }
            })
        try {
            synchronized(lifecycleLock) {
                writer.counter("fail.writer", 1L)
                assertTrue(observerEntered.await(5, TimeUnit.SECONDS))
                assertTrue("Closed I/O inherited a callback/lifecycle deadlock", writer.close(1_000L))
            }
            assertTrue(observerCompleted.await(5, TimeUnit.SECONDS))
        } finally {
            writer.close(1_000L)
            directory.deleteRecursively()
        }
    }

    @Test
    fun workerStorageCallbacksCannotWaitForTheirOwnControlLane() {
        val directory = newDirectory("writer-self-control")
        val completed = CountDownLatch(1)
        val worker = AtomicReference<Thread>()
        val outcomes = java.util.Collections.synchronizedList(mutableListOf<Boolean>())
        lateinit var writer: AsyncLogWriter
        writer = AsyncLogWriter(directory, JankHunterConfig.builder().build(), "app", setOf("app"), true,
            1L, "2026-10-15", 1L, { 1L }, LogQualityCounters(), prepareSession = {
                worker.set(Thread.currentThread())
                outcomes += writer.flushBlocking(10_000L)
                outcomes += writer.writeLogGrowthSummaryBlocking(10_000L)
                outcomes += writer.captureSnapshotBlocking(10_000L) != null
                completed.countDown()
                null
            }, onTerminalStop = AsyncWriterTerminalObserver.NONE)
        try {
            writer.counter("start.writer", 1L)
            assertTrue("Writer callback waited for work only it can perform", completed.await(1, TimeUnit.SECONDS))
            assertEquals(listOf(false, false, false), outcomes)
        } finally {
            if (completed.count != 0L) worker.get()?.interrupt()
            writer.close(1_000L)
            directory.deleteRecursively()
        }
    }

}
