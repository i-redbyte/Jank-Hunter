package io.jankhunter.runtime

import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.QualityCounterId
import java.io.File
import java.util.concurrent.TimeUnit
import org.junit.Assert.*
import org.junit.Test

class RuntimeGraphBudgetArtTest {
    @Test
    fun recoveredStackWritesExactEdgesAndExplicitUnknownCoverageToJhlog() {
        val context = InstrumentationRegistry.getInstrumentation().context
        val root = File(context.cacheDir, "graph-storage-wire").apply { deleteRecursively() }
        val writer = AsyncLogWriterFactory().open(root, JankHunterConfig.builder().autoStartCollectors(false)
            .runtimeCallGraphEnabled(true).build(), "main")
        val graph = RuntimeCallGraph({ System.nanoTime() / 1_000_000L }, { "Screen" }, { 1L }, { 512 },
            admissionWaitNanos = { TimeUnit.SECONDS.toNanos(2L) },
            producerStorageLimitBytes = 32_768L)
        val tokens = LongArray(263)
        try {
            graph.resetFlushState(writer)
            for (index in tokens.indices) tokens[index] = graph.enter(index + 1L, "app.Method${index + 1}", true)
            assertTrue(tokens.take(256).all { it > 0L })
            assertTrue(tokens.drop(256).all { it < 0L })
            assertFalse(graph.hasCurrentMethod())
            for (index in 262 downTo 256) graph.exit(tokens[index], index + 1L)
            assertEquals(256L, graph.currentMethodId())
            for (index in 255 downTo 0) graph.exit(tokens[index], index + 1L)
            assertTrue(graph.flushBlocking(5_000L))
            assertEquals(255L, graph.attemptedForTest())
            assertEquals(255L, graph.acceptedForTest())
            assertEquals(255L, graph.emittedForTest())
            assertEquals(0L, graph.acceptedEventLossForTest())
            assertEquals(7L, generationQuality(writer, QualityCounterId.RUNTIME_GRAPH_STORAGE_SKIPPED_ENTRY_TOTAL))
            assertTrue(graph.storagePeakForTest() <= 32_768L)
            assertTrue(graph.flushForShutdown(5_000L))
            assertEquals(0L, graph.storageUsedForTest())
        } finally {
            graph.flushForShutdown(5_000L)
            graph.clear()
            writer.close()
        }
        val log = root.walkTopDown().single { it.isFile && it.extension == "jhlog" }
        log.copyTo(File(context.filesDir, "runtime-graph-storage-5.1.0.jhlog"), overwrite = true)
        root.deleteRecursively()
    }
}
