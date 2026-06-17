package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterConfig
import java.nio.charset.StandardCharsets
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import kotlin.concurrent.thread
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
    fun closeWaitsForInFlightQueuedActionBeforeClosingWriter() {
        val directory = Files.createTempDirectory("jankhunter-async-writer").toFile()
        try {
            val config = JankHunterConfig.builder()
                .maxQueueSize(16)
                .flushIntervalMs(60_000)
                .logCompressionEnabled(false)
                .build()
            val asyncWriter = AsyncLogWriter.open(directory, config, "main")
            val actionStarted = CountDownLatch(1)
            val releaseAction = CountDownLatch(1)

            asyncWriter.enqueueForTest { writer ->
                actionStarted.countDown()
                if (!releaseAction.await(1, TimeUnit.SECONDS)) {
                    throw java.io.IOException("test action was not released")
                }
                writer.counter("slow.close.action", 1)
            }
            asyncWriter.counter("after.slow.close", 1)

            val closer = thread(start = true) {
                asyncWriter.close()
            }
            assertTrue(actionStarted.await(1, TimeUnit.SECONDS))
            Thread.sleep(100)
            assertTrue("close returned while a queued action was still running", closer.isAlive)

            releaseAction.countDown()
            closer.join(2_000)
            assertFalse("close did not finish after the queued action was released", closer.isAlive)

            val text = logText(directory)
            assertTrue(text.contains("slow.close.action"))
            assertTrue(text.contains("after.slow.close"))
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun closeReturnsWhenInFlightActionExceedsTimeout() {
        val directory = Files.createTempDirectory("jankhunter-async-writer").toFile()
        try {
            val config = JankHunterConfig.builder()
                .maxQueueSize(16)
                .flushIntervalMs(60_000)
                .logCompressionEnabled(false)
                .build()
            val asyncWriter = AsyncLogWriter.open(directory, config, "main")
            val actionStarted = CountDownLatch(1)
            val releaseAction = CountDownLatch(1)

            asyncWriter.enqueueForTest { writer ->
                actionStarted.countDown()
                releaseAction.await(5, TimeUnit.SECONDS)
                writer.counter("released.after.timeout", 1)
            }

            assertTrue(actionStarted.await(1, TimeUnit.SECONDS))
            val startNs = System.nanoTime()
            assertFalse(asyncWriter.close(timeoutMs = 100))
            val elapsedMs = TimeUnit.NANOSECONDS.toMillis(System.nanoTime() - startNs)
            assertTrue("close ignored timeout: elapsedMs=$elapsedMs", elapsedMs < 1_000)

            releaseAction.countDown()
            assertTrue(asyncWriter.close(timeoutMs = 2_000))
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

    private fun logText(directory: java.io.File): String {
        return directory
            .listFiles { file -> file.isFile && file.name.endsWith(".jhlog") }
            .orEmpty()
            .joinToString(separator = "\n") { file ->
                file.readBytes().toString(StandardCharsets.ISO_8859_1)
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
