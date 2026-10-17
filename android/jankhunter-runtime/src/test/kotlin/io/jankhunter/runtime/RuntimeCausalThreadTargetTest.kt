package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.CausalKind
import java.nio.file.Files
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotSame
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeCausalThreadTargetTest {
    @Test
    fun constructorBoundTargetTracksActualRootWithoutAnInstrumentedStart() = withSession { graph ->
        val calls = AtomicInteger()
        val observedKind = AtomicInteger()
        val observedId = AtomicLong()
        val original = Runnable {
            calls.incrementAndGet()
            val context = graph.concurrencyTelemetry.captureCausalContext()
            observedKind.set(context?.entityKind ?: 0)
            observedId.set(context?.entityId ?: 0L)
        }
        val prepared = checkNotNull(graph.concurrencyTelemetry.prepareThreadTarget(
            original, 11L, "test.Owner#createThread()V", 12, 1,
        ))
        assertNotSame(original, prepared)
        val thread = Thread(prepared, "causal-target-test")
        graph.concurrencyTelemetry.bindThreadTarget(thread, prepared)

        thread.run()
        assertEquals(1, calls.get())
        assertEquals(0, observedKind.get())
        assertEquals(0L, observedId.get())

        thread.start()
        thread.join(2_000L)
        assertTrue(!thread.isAlive)
        assertEquals(2, calls.get())
        assertEquals(CausalKind.THREAD, observedKind.get())
        assertTrue(observedId.get() > 0L)
    }

    @Test
    fun nullTargetRemainsNull() = withSession { graph ->
        assertEquals(null, graph.concurrencyTelemetry.prepareThreadTarget(
            null, 12L, "test.Owner#createThread()V", 13, 1,
        ))
    }

    @Test
    fun endObservationRequiresTerminatedStateAndIsDeduplicated() = withSession { graph ->
        val thread = Thread { }
        val registry = checkNotNull(graph.state.collectionEpochs.current?.causalRegistry)
        graph.concurrencyTelemetry.observeThreadEnd(thread)
        assertEquals(0L, registry.find(CausalKind.THREAD, thread))

        thread.start()
        thread.join(2_000L)
        assertTrue(!thread.isAlive)
        graph.concurrencyTelemetry.observeThreadEnd(thread)
        val id = registry.find(CausalKind.THREAD, thread)
        assertTrue(id > 0L)
        graph.concurrencyTelemetry.observeThreadEnd(thread)
        assertEquals(0L, registry.markThreadEndObserved(thread))
    }

    private inline fun withSession(block: (RuntimeComponentGraph) -> Unit) {
        val directory = Files.createTempDirectory("jh-causal-thread-target").toFile()
        val config = JankHunterConfig.builder()
            .availableRuntimeFeatures(setOf(JankHunterRuntimeFeature.CONCURRENCY_DIAGNOSTICS))
            .build()
        val writer = AsyncLogWriterFactory().open(directory, config, "causal-thread-target")
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
