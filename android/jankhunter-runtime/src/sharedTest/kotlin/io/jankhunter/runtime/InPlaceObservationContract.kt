package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.io.File
import java.util.concurrent.Callable
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executor
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

abstract class InPlaceObservationContract {
    protected abstract fun testDirectory(): File
    @Test
    fun taskAndCoroutineStrategiesAreIndependentAtExecutionFactories() {
        for (tasks in JankHunterExecutionObservationMode.values()) {
            for (coroutines in JankHunterExecutionObservationMode.values()) {
                withSession(tasks, coroutines) { graph ->
                    val task = Runnable { }
                    val block: (Any?, Any?) -> Any? = { _, _ -> 1 }
                    assertEquals(tasks == JankHunterExecutionObservationMode.IN_PLACE,
                        task === graph.asyncTelemetry.wrapRunnable(task, "owner"))
                    assertEquals(coroutines == JankHunterExecutionObservationMode.IN_PLACE,
                        block === graph.asyncTelemetry.wrapCoroutineBlock(block, "owner"))
                }
            }
        }
    }

    @Test
    fun allTaskFactoriesPreserveOriginalIdentity() = withSession { graph ->
        val task = Runnable { }
        val callable = Callable { 7 }
        val executor = Executor { it.run() }
        val listener = android.view.View.OnClickListener { }
        assertSame(listener, graph.asyncTelemetry.wrapClickListener(listener, "owner"))
        assertSame(task, graph.asyncTelemetry.wrapRunnable(task, "owner"))
        assertSame(callable, graph.asyncTelemetry.wrapCallable(callable, "owner"))
        assertSame(executor, graph.asyncTelemetry.wrapExecutor(executor, "pool", "owner"))
        assertSame(task, graph.concurrencyTelemetry.prepareThreadTarget(task, 1L, "owner", 1, 1))
        val prepared = graph.instrumentationHooks.prepareExecutorRunnable(
            executor, task, 2L, "owner", "owner", 1, 1, 0L, 0L, null, 0)
        assertSame(task, prepared)
        graph.instrumentationHooks.executorHandoffReturned(prepared, null)
        assertSame(task, graph.concurrencyTelemetry.trackedRunnable(executor, task))
        graph.concurrencyTelemetry.executorHandoffReturned(task, null)
        val handlerPost = graph.concurrencyTelemetry.prepareTrackedHandlerPost(task, 3L, "owner", 1, 1)
        assertSame(task, handlerPost.callback)
        graph.concurrencyTelemetry.trackedHandlerPostReturned(handlerPost, true)
    }

    @Test
    fun coroutineFactoriesPreserveOriginalBlockAndSegmentHooksStillWork() = withSession { graph ->
        val block: (Any?, Any?) -> Any? = { _, _ -> 1 }
        assertSame(block, graph.asyncTelemetry.wrapCoroutineBlock(block, "owner"))
        assertSame(block, graph.asyncTelemetry.wrapCausalCoroutineBlock(block, 1L, "owner", 1, 1))
        val continuation = Any()
        val token = graph.instrumentationHooks.enterCausalCoroutineSegment(continuation, "owner", 1L, "owner", 1, 1)
        assertTrue(token > 0L)
        graph.instrumentationHooks.exitCoroutineSegment(token, continuation, 1, null)
        assertNull(graph.concurrencyTelemetry.captureCausalContext())
    }

    @Test
    fun originalExecutionRunsOnceAndRestoresContextOnFailure() = withSession { graph ->
        var calls = 0
        val failure = IllegalStateException("application failure")
        val task = object : Runnable {
            override fun run() {
                val token = graph.instrumentationHooks.enterRunnableRun(this, 4L, "owner", 1, 1)
                try {
                    calls++
                    assertNotNull(graph.concurrencyTelemetry.captureCausalContext())
                    throw failure
                } finally {
                    graph.instrumentationHooks.exitRunnableRun(token, 1)
                }
            }
        }
        try {
            checkNotNull(graph.asyncTelemetry.wrapRunnable(task, "owner")).run()
            error("expected application failure")
        } catch (actual: IllegalStateException) {
            assertSame(failure, actual)
        }
        assertEquals(1, calls)
        assertNull(graph.concurrencyTelemetry.captureCausalContext())
    }

    @Test
    fun newBodyMarkersAreInactiveInWrapperModeAndActiveInPlace() {
        for (mode in JankHunterExecutionObservationMode.values()) {
            withSession(mode) { graph ->
                val receiver = InPlaceObservationContract::class.java
                val token = graph.instrumentationHooks.enterRunnableRun(receiver, 6L, "body", 1, 1)
                try {
                    assertEquals(mode == JankHunterExecutionObservationMode.IN_PLACE, token > 0L)
                } finally {
                    graph.instrumentationHooks.exitRunnableRun(token, 0)
                }
                val callable = graph.instrumentationHooks.enterCallableCall(receiver, 7L, "body", 1, 1)
                try {
                    assertEquals(mode == JankHunterExecutionObservationMode.IN_PLACE, callable > 0L)
                } finally {
                    graph.instrumentationHooks.exitCallableCall(callable, 0)
                }
            }
        }
    }

    @Test
    fun waitingApplicationBodyHasNoPersistentWrapperInPlaceButLegacyControlDoes() {
        for (mode in JankHunterExecutionObservationMode.values()) {
            withSession(mode) { graph ->
                val entered = CountDownLatch(1)
                val release = CountDownLatch(1)
                val thread = AtomicReference<Thread>()
                // A plain pool permits wrapping; opaque delegated factories intentionally pass through.
                val pool = java.util.concurrent.ThreadPoolExecutor(1, 1, 0L, TimeUnit.MILLISECONDS,
                    java.util.concurrent.LinkedBlockingQueue())
                try {
                    val original = Runnable {
                        thread.set(Thread.currentThread())
                        entered.countDown()
                        check(release.await(5L, TimeUnit.SECONDS))
                    }
                    val prepared = graph.instrumentationHooks.prepareExecutorRunnable(
                        pool, original, 5L, "owner", "owner", 1, 1, 0L, 0L, null, 0)
                    pool.execute(checkNotNull(prepared))
                    graph.instrumentationHooks.executorHandoffReturned(prepared, null)
                    assertTrue(entered.await(2L, TimeUnit.SECONDS))
                    val stack = checkNotNull(thread.get()).stackTrace
                    val wrapper = stack.any { it.className == "io.jankhunter.runtime.JankHunterRunnable" }
                    assertEquals(stack.joinToString("\n"), mode == JankHunterExecutionObservationMode.WRAPPERS, wrapper)
                    assertTrue(stack.none { it.className == "io.jankhunter.runtime.RuntimeInstrumentationHooks" })
                } finally {
                    release.countDown()
                    pool.shutdown()
                    assertTrue(pool.awaitTermination(3L, TimeUnit.SECONDS))
                }
            }
        }
    }

    private inline fun withSession(
        mode: JankHunterExecutionObservationMode = JankHunterExecutionObservationMode.IN_PLACE,
        coroutineMode: JankHunterExecutionObservationMode = mode,
        block: (RuntimeComponentGraph) -> Unit,
    ) {
        val directory = testDirectory()
        val config = JankHunterConfig.builder()
            .taskObservationMode(mode)
            .coroutineObservationMode(coroutineMode).build()
        val writer = AsyncLogWriterFactory().open(directory, config, "in-place")
        val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1_000L })
        graph.state.bindCurrentConfiguration(config)
        graph.state.runtimeEnabled.set(true)
        graph.state.writer = writer
        graph.coordinator.markStarted(config)
        try {
            block(graph)
        } finally {
            graph.coordinator.beginStop()
            writer.close(1_000L)
            directory.deleteRecursively()
            assertNull(writer.terminalFailureCause())
        }
    }
}
