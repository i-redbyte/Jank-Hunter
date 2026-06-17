package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterConfig
import java.nio.charset.StandardCharsets
import java.nio.file.Files
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AsyncLogWriterTest {
    @Test
    fun closeDrainsQueuedWritesBeforeReturning() {
        val directory = Files.createTempDirectory("jankhunter-async-writer").toFile()
        try {
            val config = JankHunterConfig.builder()
                .maxQueueSize(512)
                .flushIntervalMs(60_000)
                .logCompressionEnabled(false)
                .build()
            val asyncWriter = AsyncLogWriter.open(directory, config, "main")

            repeat(128) { index ->
                asyncWriter.counter("closequeue$index", index.toLong())
            }
            asyncWriter.close()

            val text = directory
                .listFiles { file -> file.isFile && file.name.endsWith(".jhlog") }
                .orEmpty()
                .joinToString(separator = "\n") { file ->
                    file.readBytes().toString(StandardCharsets.ISO_8859_1)
                }
            assertTrue(text.contains("closequeue0"))
            assertTrue(text.contains("closequeue127"))
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun negativeMetricsAreRecordedAsInvalidCounters() {
        val directory = Files.createTempDirectory("jankhunter-async-writer").toFile()
        try {
            val config = JankHunterConfig.builder()
                .flushIntervalMs(60_000)
                .logCompressionEnabled(false)
                .build()
            val asyncWriter = AsyncLogWriter.open(directory, config, "main")

            asyncWriter.counter("badnegativecounter", -1)
            asyncWriter.gauge("badnegativegauge", -5)
            assertTrue(asyncWriter.flushBlocking())
            asyncWriter.close()

            val text = directory
                .listFiles { file -> file.isFile && file.name.endsWith(".jhlog") }
                .orEmpty()
                .joinToString(separator = "\n") { file ->
                    file.readBytes().toString(StandardCharsets.ISO_8859_1)
            }
            assertTrue(text.contains("jankhunter.metric.invalid_negative.counter.count"))
            assertTrue(text.contains("jankhunter.metric.invalid_negative.gauge.count"))
            assertFalse(text.contains("badnegativecounter"))
            assertFalse(text.contains("badnegativegauge"))
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun flushBlockingWaitsUntilQueuedWritesReachDisk() {
        val directory = Files.createTempDirectory("jankhunter-async-writer").toFile()
        try {
            val config = JankHunterConfig.builder()
                .flushIntervalMs(60_000)
                .logCompressionEnabled(false)
                .build()
            val asyncWriter = AsyncLogWriter.open(directory, config, "main")

            asyncWriter.counter("before.close", 1)
            assertTrue(asyncWriter.flushBlocking())

            val text = directory
                .listFiles { file -> file.isFile && file.name.endsWith(".jhlog") }
                .orEmpty()
                .joinToString(separator = "\n") { file ->
                    file.readBytes().toString(StandardCharsets.ISO_8859_1)
                }
            assertTrue(text.contains("before.close"))

            asyncWriter.close()
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun recordsIoErrorsAfterWriterRecovery() {
        val directory = Files.createTempDirectory("jankhunter-async-writer").toFile()
        try {
            val config = JankHunterConfig.builder()
                .flushIntervalMs(1)
                .logCompressionEnabled(false)
                .build()
            val asyncWriter = AsyncLogWriter.open(directory, config, "main")
            val writerField = AsyncLogWriter::class.java.getDeclaredField("writer").apply {
                isAccessible = true
            }
            (writerField.get(asyncWriter) as BinaryLogWriter).close()

            asyncWriter.counter("afterclosedwriter", 1)
            assertTrue(asyncWriter.flushBlocking())
            asyncWriter.close()

            val text = directory
                .listFiles { file -> file.isFile && file.name.endsWith(".jhlog") }
                .orEmpty()
                .joinToString(separator = "\n") { file ->
                    file.readBytes().toString(StandardCharsets.ISO_8859_1)
                }
            assertTrue(text.contains("jankhunter.writer_io_error.count"))
            assertTrue(text.contains("afterclosedwriter"))
        } finally {
            directory.deleteRecursively()
        }
    }
}
