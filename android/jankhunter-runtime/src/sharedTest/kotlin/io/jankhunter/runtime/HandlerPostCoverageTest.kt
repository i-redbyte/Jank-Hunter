package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Test

class HandlerPostCoverageTest {
    @Test
    fun onlySuccessfulObservedPostsWhileEnabledCountAsUnattributedCoverage() {
        val directory = Files.createTempDirectory("handler-coverage").toFile()
        val config = JankHunterConfig.builder().autoStartCollectors(false).build()
        val writer = AsyncLogWriterFactory().open(directory, config, "handler-coverage")
        val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1_000L })
        graph.state.writer = writer
        graph.state.config = config
        graph.coordinator.markStarted(config)
        val original = Runnable { throw AssertionError("post observation must never execute the callback") }
        try {
            graph.handlerHooks.onPostResult(original, original, posted = false)
            assertEquals(0L, generationQuality(writer, COVERAGE_ID))
            repeat(2) { graph.handlerHooks.onPostResult(original, original, posted = true) }
            assertEquals("successful posts without provable execution linkage were hidden", 2L, generationQuality(writer, COVERAGE_ID))
            graph.state.featureGate.deactivate()
            graph.handlerHooks.onPostResult(original, original, posted = true)
            assertEquals(2L, generationQuality(writer, COVERAGE_ID))
        } finally {
            graph.session.stop(clearInit = true)
            writer.close()
            directory.deleteRecursively()
        }
    }

    private companion object {
        const val COVERAGE_ID = 0x203d
    }
}
