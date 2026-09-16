package io.jankhunter.runtime

import java.util.ArrayDeque
import java.util.concurrent.Executor
import java.util.concurrent.ExecutionException
import java.util.concurrent.Callable
import java.util.concurrent.CountDownLatch
import java.util.concurrent.RunnableScheduledFuture
import java.util.concurrent.ScheduledThreadPoolExecutor
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Assert.assertFalse
import org.junit.Test

class ExecutorEnqueueContextTest {
    @Test
    fun runningCancellationDetachesMetadataWithoutChangingTheRunningContext() = withHarness { test ->
        val started = CountDownLatch(1)
        val release = CountDownLatch(1)
        var observed: JankHunterContext? = null
        val future = test.withContext(SUBMITTER) {
            test.scheduled.schedule({
                started.countDown()
                check(release.await(2L, TimeUnit.SECONDS))
                observed = test.capture()
            }, 0L, TimeUnit.MILLISECONDS)
        }
        val command = test.scheduledCommand()
        try {
            assertTrue(started.await(2L, TimeUnit.SECONDS))
            assertTrue(future.cancel(false))
            assertNoMetadata(command)
        } finally {
            release.countDown()
        }
        test.assertWorkerContext()
        assertEquals(SUBMITTER, observed)
    }

    @Test
    fun failingScheduledTaskPreservesItsContextThrowableAndCleanup() = withHarness { test ->
        val failure = IllegalStateException("scheduled failure")
        var observed: JankHunterContext? = null
        val future = test.withContext(SUBMITTER) {
            test.scheduled.schedule(Runnable { observed = test.capture(); throw failure }, 0L, TimeUnit.MILLISECONDS)
        }
        val command = test.scheduledCommand()
        assertSame(failure, assertThrows(ExecutionException::class.java) { future.get(2L, TimeUnit.SECONDS) }.cause)
        assertEquals(SUBMITTER, observed)
        assertNoMetadata(command)
        test.assertWorkerContext()
    }

    @Test
    fun scheduledRunnableCapturesSubmitterAndClearsMetadataAfterCompletion() = withHarness { test ->
        var observed: JankHunterContext? = null
        val future = test.withContext(SUBMITTER) { test.scheduled.schedule({ observed = test.capture() }, 0L, TimeUnit.MILLISECONDS) }
        val command = test.scheduledCommand()
        future.get(2L, TimeUnit.SECONDS)
        assertEquals(SUBMITTER, observed)
        assertNoMetadata(command)
        test.assertWorkerContext()
    }

    @Test
    fun scheduledCallablePreservesItsResultAndClearsTaskMetadata() = withHarness { test ->
        val future = test.withContext(SUBMITTER) { test.scheduled.schedule(Callable { test.capture() }, 0L, TimeUnit.MILLISECONDS) }
        val command = test.scheduledCommand()
        assertEquals(SUBMITTER, future.get(2L, TimeUnit.SECONDS))
        assertNoMetadata(command)
        assertEquals(SUBMITTER, future.get())
        test.assertWorkerContext()
    }

    @Test
    fun scheduledCancellationClearsMetadataEvenWhenDelegateRetainsTheCommand() = withHarness { test ->
        val future = test.withContext(SUBMITTER) { test.scheduled.schedule({}, 1L, TimeUnit.DAYS) }
        val command = test.scheduledCommand()
        assertTrue(future.cancel(false))
        assertNoMetadata(command)
    }

    @Test
    fun periodicRunsKeepSubmitterContextUntilCancellation() = withHarness { test ->
        val observed = java.util.concurrent.CopyOnWriteArrayList<JankHunterContext>()
        val twice = CountDownLatch(2)
        val future = test.withContext(SUBMITTER) {
            test.scheduled.scheduleWithFixedDelay({ observed += test.capture(); twice.countDown() }, 0L, 1L, TimeUnit.MILLISECONDS)
        }
        val command = test.scheduledCommand()
        try {
            assertTrue(twice.await(2L, TimeUnit.SECONDS))
        } finally {
            assertTrue(future.cancel(false))
        }
        assertTrue(observed.size >= 2)
        assertTrue("periodic task lost its enqueue context: $observed", observed.all { it == SUBMITTER })
        assertNoMetadata(command)
        test.assertWorkerContext()
    }

    private fun assertNoMetadata(command: Any) {
        var type: Class<*>? = command.javaClass
        while (type != null && type != Any::class.java) {
            for (field in type.declaredFields) {
                if (java.lang.reflect.Modifier.isStatic(field.modifiers)) continue
                field.isAccessible = true
                val value = field.get(command)
                assertFalse("terminal command retained enqueue metadata through ${field.name}",
                    value is JankHunterContext || value == SUBMITTER.screen || value == SUBMITTER.owner)
            }
            type = type.superclass
        }
    }

    @Test fun enqueueClockFailurePreservesApplicationFailure() = assertClockFailureIsolated(1)
    @Test fun queueWaitClockFailurePreservesApplicationFailure() = assertClockFailureIsolated(2)
    @Test fun serviceStartClockFailurePreservesApplicationFailure() = assertClockFailureIsolated(3)
    @Test fun serviceEndClockFailurePreservesApplicationFailure() = assertClockFailureIsolated(4)

    private fun assertClockFailureIsolated(failingRead: Int) {
        val reads = AtomicInteger()
        val failure = IllegalStateException("application failure")
        withHarness(clock = {
            if (reads.incrementAndGet() == failingRead) error("telemetry clock failure")
            1L
        }) { test ->
            var runs = 0
            test.withContext(SUBMITTER) { test.executor.execute { runs++; throw failure } }
            test.runNext(WORKER, failure)
            assertEquals("clock read $failingRead skipped application", 1, runs)
        }
    }

    @Test
    fun delegateRetryUsesTheOriginalEnqueueMetadataAgain() = withHarness { test ->
        val observed = mutableListOf<JankHunterContext>()
        val failure = IllegalStateException("retry")
        test.withContext(SUBMITTER) {
            test.executor.execute {
                observed += test.capture()
                if (observed.size == 1) throw failure
            }
        }
        test.runNext(WORKER, failure, repeats = 2)
        assertEquals(listOf(SUBMITTER, SUBMITTER), observed)
    }

    @Test
    fun executorRunsWithSubmitterContextAndRestoresWorkerContext() = withHarness { test ->
        var observed: JankHunterContext? = null
        test.withContext(SUBMITTER) { test.executor.execute { observed = test.capture() } }
        test.runNext(WORKER)
        assertEquals(SUBMITTER, observed)
    }

    @Test
    fun applicationFailurePreservesOriginalThrowableAndRestoresWorkerContext() = withHarness { test ->
        val failure = IllegalStateException("application failure")
        var observed: JankHunterContext? = null
        test.withContext(SUBMITTER) {
            test.executor.execute {
                observed = test.capture()
                throw failure
            }
        }
        test.runNext(WORKER, failure)
        assertEquals(SUBMITTER, observed)
    }

    @Test
    fun nestedSubmissionCapturesTheRunningTaskContext() = withHarness { test ->
        var outer: JankHunterContext? = null
        var inner: JankHunterContext? = null
        test.withContext(SUBMITTER) {
            test.executor.execute {
                outer = test.capture()
                test.executor.execute { inner = test.capture() }
            }
        }
        test.runNext(WORKER)
        test.runNext(JankHunterContext("other-screen", "other-worker", 99L))
        assertEquals(SUBMITTER, outer)
        assertEquals(SUBMITTER, inner)
    }

    @Test
    fun reusedWorkerReceivesEachTasksOwnEnqueueSnapshot() = withHarness { test ->
        val second = JankHunterContext("second-screen", "second-submitter", 43L)
        val observed = mutableListOf<JankHunterContext>()
        test.withContext(SUBMITTER) { test.executor.execute { observed += test.capture() } }
        test.withContext(second) { test.executor.execute { observed += test.capture() } }
        test.runNext(WORKER)
        test.runNext(WORKER)
        assertEquals(listOf(SUBMITTER, second), observed)
    }

    private fun withHarness(clock: RuntimeLongSource = RuntimeLongSource { 1L }, action: (Harness) -> Unit) {
        val test = Harness(clock)
        try {
            action(test)
        } finally {
            test.close()
        }
    }

    private class Harness(clock: RuntimeLongSource) {
        private val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1L })
        private val worker = RetainingScheduler(graph)
        private val queued = ArrayDeque<Runnable>()
        val executor = JankHunterExecutor(Executor { queued.addLast(it) }, "context", null, clock, graph.asyncTelemetry)
        val scheduled = JankHunterScheduledExecutorService(worker, "context", null, clock, graph.asyncTelemetry)

        init {
            val config = JankHunterConfig.builder().build()
            graph.state.config = config
            graph.state.featureGate.activate(config)
        }

        fun capture(): JankHunterContext = graph.contextTracker.capture()

        fun scheduledCommand(): Any = checkNotNull(worker.lastCommand)

        fun assertWorkerContext() {
            assertEquals(WORKER, worker.submit(Callable { capture() }).get(2L, TimeUnit.SECONDS))
        }

        fun <T> withContext(context: JankHunterContext, action: () -> T): T =
            graph.contextTracker.callWithContext(context, null, {}, action)

        fun runNext(workerContext: JankHunterContext, failure: Throwable? = null, repeats: Int = 1) {
            val task = queued.removeFirst()
            worker.submit {
                repeat(repeats) { attempt ->
                    withContext(workerContext) {
                        if (failure != null && attempt == 0) {
                            assertSame(failure, assertThrows(failure.javaClass) { task.run() })
                        } else {
                            task.run()
                        }
                        assertEquals("worker context was not restored", workerContext, capture())
                    }
                }
            }.get(2L, TimeUnit.SECONDS)
        }

        fun close() {
            worker.shutdownNow()
            check(worker.awaitTermination(2L, TimeUnit.SECONDS))
        }
    }

    private class RetainingScheduler(graph: RuntimeComponentGraph) : ScheduledThreadPoolExecutor(1, { task ->
        Thread({ graph.contextTracker.callWithContext(WORKER, null, {}, task::run) }, "enqueue-context-worker")
    }) {
        @Volatile var lastCommand: Any? = null

        override fun <V> decorateTask(command: Runnable, task: RunnableScheduledFuture<V>): RunnableScheduledFuture<V> {
            lastCommand = command
            return task
        }

        override fun <V> decorateTask(command: Callable<V>, task: RunnableScheduledFuture<V>): RunnableScheduledFuture<V> {
            lastCommand = command
            return task
        }
    }

    private companion object {
        val SUBMITTER = JankHunterContext("submit-screen", "submitter", 42L)
        val WORKER = JankHunterContext("worker-screen", "worker", 7L)
    }
}
