package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterAgentEventBatch
import io.jankhunter.runtime.JankHunterAgentEventType
import io.jankhunter.runtime.JankHunterConfig
import java.io.File
import java.nio.file.Files
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class CurrentAgentEventSinkTest {
    @Test
    fun binaryWriterAcceptsAgentSemanticRecords() {
        val directory = Files.createTempDirectory("jankhunter-agent-encode").toFile()
        try {
            val file = File(directory, "agent.jhlog")
            BinaryLogWriter(file).use { writer ->
                writer.agentEventFromBatchWords(buildGcBatch().copyPackedWords(), 0)
                writer.agentContextDefinition(77L, "Feed", "FeedImages", "Feed scroll", null)
                writer.agentMethodDefinition(44L, "Landroid/os/Looper;->loop()V")
            }
            assertTrue(file.length() > 0L)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun sinkPublishesThroughAsyncWriter() {
        val directory = Files.createTempDirectory("jankhunter-agent-sink").toFile()
        try {
            val config = JankHunterConfig.builder().flushIntervalMs(5L).build()
            val async = AsyncLogWriter(
                directory = directory,
                config = config,
                processName = "main",
                expectedProcesses = setOf("main"),
                rosterDeclarationComplete = true,
                sessionStartMs = 1L,
                sessionLocalDate = "2026-09-22",
                collectorStartElapsedUs = 1L,
                currentTimeMs = { 1L },
                quality = LogQualityCounters(),
                prepareSession = { null },
                onTerminalStop = AsyncWriterTerminalObserver.NONE,
            )
            val sink = CurrentAgentEventSink { async }
            assertTrue(sink.tryPublish(buildGcBatch()))
            assertTrue(
                sink.tryPublishContext(
                    contextToken = 77L,
                    screen = "Feed",
                    owner = "FeedImages",
                    flow = "Feed scroll",
                    step = null,
                ),
            )
            assertTrue(sink.tryPublishMethodDefinition(44L, "Landroid/os/Looper;->loop()V"))
            assertTrue(async.close())
            val bytes = directory.walkTopDown().filter { it.extension == "jhlog" }.sumOf { it.length() }
            assertTrue(bytes > 0L)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun sinkRejectsWhenWriterUnavailable() {
        val sink = CurrentAgentEventSink { null }
        assertFalse(sink.tryPublish(buildGcBatch()))
        assertFalse(sink.tryPublishContext(1L, "a", "b", null, null))
    }

    private fun buildGcBatch(): JankHunterAgentEventBatch {
        val batch = JankHunterAgentEventBatch(1)
        assertTrue(
            batch.tryAppend(
                type = JankHunterAgentEventType.GC_INTERVAL,
                schemaVersion = 1,
                flags = 0,
                producerSequence = 4L,
                monotonicNs = 1_050_000_000L,
                producerId = 0L,
                threadToken = 0L,
                contextToken = 0L,
                payload0 = 1_050_000_000L,
                payload1 = 100_000_000L,
                payload2 = 0L,
                payload3 = 0L,
            ),
        )
        return batch
    }
}
