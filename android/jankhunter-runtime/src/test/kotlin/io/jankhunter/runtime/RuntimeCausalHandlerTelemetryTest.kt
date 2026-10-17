package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.CausalKind
import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNotSame
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeCausalHandlerTelemetryTest {
    @Test
    fun passivePostDoesNotBindTheCallbackToAnInventedDispatch() = withSession { graph ->
        val handler = Any()
        val callback = Runnable { }
        val id = graph.concurrencyTelemetry.beginHandlerPost(
            handler, callback, 1L, "test.Owner#post()V", 3, 1,
        )
        assertTrue(id > 0L)
        graph.concurrencyTelemetry.handlerPostReturned(handler, callback, id, true)
        val registry = checkNotNull(graph.state.collectionEpochs.current?.causalRegistry)
        assertEquals(0L, registry.find(CausalKind.HANDLER_POST, callback))
        assertEquals(null, graph.concurrencyTelemetry.captureCausalContext())
    }

    @Test
    fun twoTrackedPostsOfOneRunnableHaveSeparateIdentityAndOnlyExecutedPlacementRuns() = withSession { graph ->
        var calls = 0
        var runningId = 0L
        val callback = Runnable {
            calls++
            assertEquals(CausalKind.HANDLER_POST, graph.concurrencyTelemetry.captureCausalContext()?.entityKind)
            assertEquals(runningId, graph.concurrencyTelemetry.captureCausalContext()?.entityId)
        }
        val first = graph.concurrencyTelemetry.prepareTrackedHandlerPost(
            callback, 2L, "test.Owner#post()V", 4, 1,
        )
        val second = graph.concurrencyTelemetry.prepareTrackedHandlerPost(
            callback, 3L, "test.Owner#postAgain()V", 5, 1,
        )
        assertTrue(first.tracked)
        assertTrue(second.tracked)
        assertNotEquals(first.id, second.id)
        assertNotSame(callback, first.callback)
        assertNotSame(first.callback, second.callback)
        val registry = checkNotNull(graph.state.collectionEpochs.current?.causalRegistry)
        assertEquals(first.id, registry.find(CausalKind.HANDLER_POST, first.callback))
        assertEquals(second.id, registry.find(CausalKind.HANDLER_POST, second.callback))

        graph.concurrencyTelemetry.trackedHandlerPostReturned(first, true)
        runningId = second.id
        second.callback.run()
        graph.concurrencyTelemetry.trackedHandlerPostReturned(second, true)
        assertEquals(1, calls)
        assertEquals(0L, registry.find(CausalKind.HANDLER_POST, second.callback))
        assertEquals(first.id, registry.find(CausalKind.HANDLER_POST, first.callback))
        assertEquals(null, graph.concurrencyTelemetry.captureCausalContext())
    }

    @Test
    fun failedPostReleasesOnlyItsOwnBinding() = withSession { graph ->
        val callback = Runnable { }
        val rejected = graph.concurrencyTelemetry.prepareTrackedHandlerPost(callback, 4L, "test.Owner#post()V", 6, 1)
        val accepted = graph.concurrencyTelemetry.prepareTrackedHandlerPost(callback, 5L, "test.Owner#post()V", 7, 1)
        graph.concurrencyTelemetry.trackedHandlerPostReturned(rejected, false)
        val registry = checkNotNull(graph.state.collectionEpochs.current?.causalRegistry)
        assertEquals(0L, registry.find(CausalKind.HANDLER_POST, rejected.callback))
        assertEquals(accepted.id, registry.find(CausalKind.HANDLER_POST, accepted.callback))
        assertFalse(rejected.callback === callback)
    }

    @Test
    fun callbackFailureRestoresContextAndPropagatesTheSameException() = withSession { graph ->
        val failure = IllegalStateException("callback")
        val callback = Runnable { throw failure }
        val prepared = graph.concurrencyTelemetry.prepareTrackedHandlerPost(callback, 6L, "test.Owner#post()V", 8, 1)
        val caught = try {
            prepared.callback.run()
            null
        } catch (throwable: Throwable) {
            throwable
        }
        assertSame(failure, caught)
        assertEquals(null, graph.concurrencyTelemetry.captureCausalContext())
        val registry = checkNotNull(graph.state.collectionEpochs.current?.causalRegistry)
        assertEquals(0L, registry.find(CausalKind.HANDLER_POST, prepared.callback))
    }

    private inline fun withSession(block: (RuntimeComponentGraph) -> Unit) {
        val directory = Files.createTempDirectory("jh-causal-handler").toFile()
        val config = JankHunterConfig.builder()
            .availableRuntimeFeatures(setOf(JankHunterRuntimeFeature.CONCURRENCY_DIAGNOSTICS))
            .build()
        val writer = AsyncLogWriterFactory().open(directory, config, "causal-handler")
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
        }
    }
}
