package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.Jhlog
import java.io.File
import java.nio.file.Files
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeFrameSessionTest {
    @Test
    fun finalFrameWindowKeepsItsWriterAfterRuntimeSessionReplacement() {
        val root = Files.createTempDirectory("jankhunter-frame-session").toFile()
        val config = JankHunterConfig.builder().autoStartCollectors(false).build()
        val oldDirectory = root.resolve("old")
        val newDirectory = root.resolve("new")
        val oldWriter = AsyncLogWriterFactory().open(oldDirectory, config, "old")
        val newWriter = AsyncLogWriterFactory().open(newDirectory, config, "new")
        val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1_000L })
        graph.state.config = config
        graph.state.featureGate.activate(config)
        graph.state.writer = oldWriter
        val callbacks = graph.collectorTelemetry.bindFrameCallbacks()
        try {
            graph.state.writer = newWriter
            val buckets = LongArray(13).also { it[2] = 2L }
            callbacks.recordUiWindow("old-screen", 32L, 2L, 0L, 16L, Jhlog.UI_SOURCE_JANKSTATS, 16_000L, buckets)
            assertTrue(oldWriter.close(1_000L))
            assertTrue(newWriter.close(1_000L))
            assertTrue("old frame window did not reach its original writer", oldDirectory.walkTopDown().any { it.extension == "jhlog" })
            assertFalse("old frame contaminated the new session", newDirectory.walkTopDown().any { it.extension == "jhlog" })
            System.getProperty("jankhunter.test.fixtureDirectory")?.let { output ->
                val source = oldDirectory.walkTopDown().single { it.isFile && it.extension == "jhlog" }
                source.copyTo(File(output, "frame-old-session.jhlog").apply { parentFile?.mkdirs() }, overwrite = true)
            }
        } finally {
            oldWriter.close(1_000L)
            newWriter.close(1_000L)
            root.deleteRecursively()
        }
    }
}
