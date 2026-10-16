package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executor
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.RejectedExecutionException
import java.util.concurrent.ThreadPoolExecutor
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotSame
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeCausalExecutorTelemetryTest {
    @Test
    fun nestedTrackedExecutorHandoffsLeaveTheirInstrumentedFramesForCallsiteHooks() = withSession { graph ->
        lateinit var tracked: JankHunterTrackedExecutor
        var delegates = 0
        val nested = Runnable { }
        tracked = JankHunterTrackedExecutor(Executor {
            delegates++
            if (delegates == 1) {
                val preparedNested = graph.concurrencyTelemetry.prepareExecutorRunnable(
                    tracked, nested, nested, 21L, "test.Owner#nested()V", 21, 1,
                    0L, 0L, null, 0,
                )
                tracked.execute(checkNotNull(preparedNested))
                graph.concurrencyTelemetry.executorHandoffReturned(preparedNested, null)
            }
        }, graph.concurrencyTelemetry)
        val outer = Runnable { }
        val preparedOuter = graph.concurrencyTelemetry.prepareExecutorRunnable(
            tracked, outer, outer, 20L, "test.Owner#outer()V", 20, 1,
            0L, 0L, null, 0,
        )
        tracked.execute(checkNotNull(preparedOuter))
        val future = Any()
        graph.concurrencyTelemetry.executorHandoffReturned(preparedOuter, future)

        assertEquals(2, delegates)
        val registry = checkNotNull(graph.state.collectionEpochs.current?.causalRegistry)
        assertTrue(registry.find(2, future) > 0L)
    }

    @Test
    fun fixedRateSubmissionSerializesWithoutWriterFailure() = withSession { graph ->
        val executor = Executor { }
        val command = Runnable { }
        val prepared = graph.concurrencyTelemetry.prepareExecutorRunnable(
            executor, command, command, 10L, "test.Owner#scheduleAtFixedRate()V", 10, 1,
            0L, 20L, TimeUnit.MILLISECONDS, 2,
        )
        graph.concurrencyTelemetry.executorHandoffReturned(prepared, Any())
    }

    @Test
    fun passiveSubmissionPreservesCommandIdentityForRemoveAndShutdownNow() = withSession { graph ->
        val occupied = CountDownLatch(1)
        val release = CountDownLatch(1)
        val pool = ThreadPoolExecutor(1, 1, 0L, TimeUnit.MILLISECONDS, LinkedBlockingQueue())
        try {
            pool.execute {
                occupied.countDown()
                release.await()
            }
            assertTrue(occupied.await(2L, TimeUnit.SECONDS))
            val removed = Runnable { error("removed command must not run") }
            val preparedRemoved = graph.concurrencyTelemetry.prepareExecutorRunnable(
                pool, removed, removed, 1L, "test.Owner#remove()V", 1, 1, 0L, 0L, null, 0,
            )
            assertSame(removed, preparedRemoved)
            pool.execute(checkNotNull(preparedRemoved))
            graph.concurrencyTelemetry.executorHandoffReturned(preparedRemoved, null)
            assertTrue(pool.remove(removed))

            val drained = Runnable { error("drained command must not run") }
            val preparedDrained = graph.concurrencyTelemetry.prepareExecutorRunnable(
                pool, drained, drained, 2L, "test.Owner#drain()V", 2, 1, 0L, 0L, null, 0,
            )
            assertSame(drained, preparedDrained)
            pool.execute(checkNotNull(preparedDrained))
            graph.concurrencyTelemetry.executorHandoffReturned(preparedDrained, null)
            assertSame(drained, pool.shutdownNow().single())
        } finally {
            release.countDown()
            pool.shutdownNow()
            assertTrue(pool.awaitTermination(2L, TimeUnit.SECONDS))
        }
    }

    @Test
    fun rejectionHandlerSeesTheOriginalCommandAndNoRunIsInvented() = withSession { graph ->
        val command = Runnable { error("rejected command must not run") }
        var received: Runnable? = null
        val rejecting = Executor { submitted ->
            received = submitted
            throw RejectedExecutionException("full")
        }
        val prepared = graph.concurrencyTelemetry.prepareExecutorRunnable(
            rejecting, command, command, 3L, "test.Owner#submit()V", 3, 1, 0L, 0L, null, 0,
        )
        try {
            rejecting.execute(checkNotNull(prepared))
            error("expected rejection")
        } catch (_: RejectedExecutionException) {
            graph.concurrencyTelemetry.executorHandoffThrew(prepared)
        }
        assertSame(command, received)
        assertEquals(null, graph.concurrencyTelemetry.captureCausalContext())
    }

    @Test
    fun manualCurrentThreadRunIsDirectExecutionNotThreadRoot() = withSession { graph ->
        val current = Thread.currentThread()
        val token = graph.concurrencyTelemetry.enterRunnableRun(current, 4L, "test.Owner#run()V", 4, 1)
        assertTrue(token > 0L)
        assertEquals(10, graph.concurrencyTelemetry.captureCausalContext()?.entityKind)
        graph.concurrencyTelemetry.exitRunnableRun(token, 0)
        assertEquals(null, graph.concurrencyTelemetry.captureCausalContext())
    }

    @Test
    fun explicitTrackedCommandRestoresExactTaskContextAndRunsDelegateOnce() = withSession { graph ->
        val executor = Executor { command -> command.run() }
        var calls = 0
        val original = Runnable {
            calls++
            assertEquals(2, graph.concurrencyTelemetry.captureCausalContext()?.entityKind)
        }
        val prepared = graph.concurrencyTelemetry.prepareExecutorRunnable(
            executor, original, original, 5L, "test.Owner#tracked()V", 5, 1, 0L, 0L, null, 0,
        )
        val tracked = graph.concurrencyTelemetry.trackedRunnable(executor, checkNotNull(prepared))
        assertNotSame(original, tracked)
        executor.execute(tracked)
        graph.concurrencyTelemetry.executorHandoffReturned(original, null)
        assertEquals(1, calls)
        assertEquals(null, graph.concurrencyTelemetry.captureCausalContext())
    }

    @Test
    fun nestedInvalidHandoffDoesNotConsumeTheOuterPlacement() = withSession { graph ->
        val executor = Executor { }
        val outer = Runnable { }
        graph.concurrencyTelemetry.prepareExecutorRunnable(
            executor, outer, outer, 6L, "test.Owner#outer()V", 6, 1, 0L, 0L, null, 0,
        )
        graph.concurrencyTelemetry.prepareExecutorRunnable(
            executor, null, null, 7L, "test.Owner#inner()V", 7, 1, 0L, 0L, null, 0,
        )
        graph.concurrencyTelemetry.executorHandoffThrew(null)
        val future = Any()
        graph.concurrencyTelemetry.executorHandoffReturned(outer, future)
        val registry = checkNotNull(graph.state.collectionEpochs.current?.causalRegistry)
        assertTrue(registry.find(2, future) > 0L)
    }

    @Test
    fun queuedTrackedCommandsCannotRetainMoreBindingsThanRegistryCapacity() = withSession(4) { graph ->
        val queued = ArrayList<Runnable>()
        val executor = Executor(queued::add)
        var instrumented = 0
        repeat(100) { index ->
            val original = Runnable { }
            val prepared = graph.concurrencyTelemetry.prepareExecutorRunnable(
                executor, original, original, index.toLong() + 10L, "test.Owner#queue()V", 9, 1,
                0L, 0L, null, 0,
            )
            val tracked = graph.concurrencyTelemetry.trackedRunnable(executor, checkNotNull(prepared))
            if (tracked !== original) instrumented++
            executor.execute(tracked)
            graph.concurrencyTelemetry.executorHandoffReturned(original, null)
        }
        val registry = checkNotNull(graph.state.collectionEpochs.current?.causalRegistry)
        assertTrue(instrumented in 1..3)
        assertTrue(registry.liveEntries() <= 4)

        queued.first().run()
        assertTrue(registry.liveEntries() <= 3)
        val next = Runnable { }
        graph.concurrencyTelemetry.prepareExecutorRunnable(
            executor, next, next, 200L, "test.Owner#queue()V", 10, 1, 0L, 0L, null, 0,
        )
        val replacement = graph.concurrencyTelemetry.trackedRunnable(executor, next)
        assertNotSame(next, replacement)
        graph.concurrencyTelemetry.executorHandoffReturned(next, null)
    }

    @Test
    fun completedOneShotReleasesFutureAliasWithAndWithoutInlineExecution() = withSession(4) { graph ->
        val executor = Executor { }
        val registry = checkNotNull(graph.state.collectionEpochs.current?.causalRegistry)
        repeat(100) { index ->
            val original = Runnable { }
            graph.concurrencyTelemetry.prepareExecutorRunnable(
                executor, original, original, index.toLong() + 300L, "test.Owner#submit()V", 11, 1,
                0L, 0L, null, 0,
            )
            val tracked = graph.concurrencyTelemetry.trackedRunnable(executor, original)
            val future = Any()
            if (index % 2 == 0) {
                tracked.run()
                graph.concurrencyTelemetry.executorHandoffReturned(original, future)
            } else {
                graph.concurrencyTelemetry.executorHandoffReturned(original, future)
                assertTrue(registry.find(2, future) > 0L)
                tracked.run()
            }
            assertEquals(0L, registry.find(2, future))
            assertTrue(registry.liveEntries() <= 1)
        }
    }

    private inline fun withSession(capacity: Int = 4096, block: (RuntimeComponentGraph) -> Unit) {
        val directory = Files.createTempDirectory("jh-causal-executor").toFile()
        val config = JankHunterConfig.builder()
            .availableRuntimeFeatures(setOf(JankHunterRuntimeFeature.CONCURRENCY_DIAGNOSTICS))
            .concurrencyMaxTrackedEntities(capacity)
            .build()
        val writer = AsyncLogWriterFactory().open(directory, config, "causal-executor")
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
            val failure = writer.terminalFailureCause()
            directory.deleteRecursively()
            assertNull(failure?.stackTraceToString(), failure)
        }
    }
}
