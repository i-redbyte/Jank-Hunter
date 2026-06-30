package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterConfig
import io.jankhunter.runtime.JankHunterLogBucket
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.EOFException
import java.io.File
import java.nio.charset.StandardCharsets
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.zip.GZIPInputStream
import kotlin.concurrent.thread
import org.junit.Assert.assertEquals
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
                .build()
            val asyncWriter = AsyncLogWriter.open(directory, config, "main")

            repeat(128) { index ->
                asyncWriter.counter("closequeue$index", index.toLong())
            }
            asyncWriter.close()

            val text = logText(directory)
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
                .build()
            val asyncWriter = AsyncLogWriter.open(directory, config, "main")
            val actionStarted = CountDownLatch(1)
            val releaseAction = CountDownLatch(1)

            enqueue(asyncWriter) { writer ->
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
                .build()
            val asyncWriter = AsyncLogWriter.open(directory, config, "main")
            val actionStarted = CountDownLatch(1)
            val releaseAction = CountDownLatch(1)

            enqueue(asyncWriter) { writer ->
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
                .build()
            val asyncWriter = AsyncLogWriter.open(directory, config, "main")

            asyncWriter.counter("badnegativecounter", -1)
            asyncWriter.gauge("badnegativegauge", -5)
            assertTrue(asyncWriter.flushBlocking())
            asyncWriter.close()

            val text = logText(directory)
            assertTrue(text.contains("jankhunter.metric.invalid_negative.counter.count"))
            assertTrue(text.contains("jankhunter.metric.invalid_negative.gauge.count"))
            assertFalse(text.contains("badnegativecounter"))
            assertFalse(text.contains("badnegativegauge"))
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun dailyBucketCreatesNewSegmentForEveryRuntimeStart() {
        val directory = Files.createTempDirectory("jankhunter-async-writer").toFile()
        try {
            val config = JankHunterConfig.builder()
                .flushIntervalMs(1)
                .logBucket(JankHunterLogBucket.DAILY)
                .build()
            val nowMs = { 1_800_000_000_000L }

            AsyncLogWriter.open(directory, config, "main", nowMs).apply {
                counter("first.daily.runtime", 1)
                close()
            }
            AsyncLogWriter.open(directory, config, "main", nowMs).apply {
                counter("second.daily.runtime", 1)
                close()
            }

            val files = logFiles(directory).sortedBy { it.name }
            assertEquals(files.joinToString { it.name }, 2, files.size)
            assertTrue(files.all { it.name.startsWith("daily-main-") })
            assertTrue(files[0].name.endsWith("-1.jhlog"))
            assertTrue(files[1].name.endsWith("-2.jhlog"))

            val text = logText(directory)
            assertTrue(text.contains("first.daily.runtime"))
            assertTrue(text.contains("second.daily.runtime"))
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun dailyBucketRollsOverWhenLocalDayChanges() {
        val directory = Files.createTempDirectory("jankhunter-async-writer").toFile()
        try {
            var nowMs = 1_800_000_000_000L
            val config = JankHunterConfig.builder()
                .flushIntervalMs(1)
                .logBucket(JankHunterLogBucket.DAILY)
                .build()

            AsyncLogWriter.open(directory, config, "main") { nowMs }.apply {
                counter("first.day", 1)
                close()
            }
            nowMs += TimeUnit.HOURS.toMillis(26)
            AsyncLogWriter.open(directory, config, "main") { nowMs }.apply {
                counter("second.day", 1)
                close()
            }

            val bucketPrefixes = logFiles(directory)
                .map { it.name.substringBeforeLast("-") }
                .toSet()
            assertEquals(bucketPrefixes.joinToString(), 2, bucketPrefixes.size)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun sessionBucketKeepsRuntimeStartsInSeparateSessionBuckets() {
        val directory = Files.createTempDirectory("jankhunter-async-writer").toFile()
        try {
            var nowMs = 1_800_000_000_000L
            val config = JankHunterConfig.builder()
                .flushIntervalMs(1)
                .logBucket(JankHunterLogBucket.SESSION)
                .build()

            AsyncLogWriter.open(directory, config, "main") { nowMs }.apply {
                counter("first.session.runtime", 1)
                close()
            }
            nowMs += 1L
            AsyncLogWriter.open(directory, config, "main") { nowMs }.apply {
                counter("second.session.runtime", 1)
                close()
            }

            val bucketPrefixes = logFiles(directory)
                .onEach { file -> assertTrue(file.name.startsWith("session-main-")) }
                .map { it.name.substringBeforeLast("-") }
                .toSet()
            assertEquals(bucketPrefixes.joinToString(), 2, bucketPrefixes.size)
        } finally {
            directory.deleteRecursively()
        }
    }

    private fun logText(directory: File): String {
        return logFiles(directory)
            .joinToString(separator = "\n") { file ->
                logFileText(file)
            }
    }

    private fun logFileText(file: File): String {
        val bytes = file.readBytes()
        if (bytes.size <= MAGIC_SIZE) return ""
        val gzipIn = GZIPInputStream(ByteArrayInputStream(bytes, MAGIC_SIZE, bytes.size - MAGIC_SIZE))
        val out = ByteArrayOutputStream()
        val buffer = ByteArray(8192)
        try {
            while (true) {
                val read = gzipIn.read(buffer)
                if (read < 0) break
                out.write(buffer, 0, read)
            }
        } catch (_: EOFException) {
            // flushBlocking can expose a valid gzip prefix before close writes the trailer.
        }
        return String(out.toByteArray(), StandardCharsets.ISO_8859_1)
    }

    private fun logFiles(directory: File): List<File> {
        return directory
            .listFiles { file -> file.isFile && file.name.endsWith(".jhlog") }
            .orEmpty()
            .toList()
    }

    private fun enqueue(asyncWriter: AsyncLogWriter, action: AsyncLogWriter.Action) {
        val offer = AsyncLogWriter::class.java.getDeclaredMethod("offer", AsyncLogWriter.Action::class.java).apply {
            isAccessible = true
        }
        offer.invoke(asyncWriter, action)
    }

    @Test
    fun flushBlockingWaitsUntilQueuedWritesReachDisk() {
        val directory = Files.createTempDirectory("jankhunter-async-writer").toFile()
        try {
            val config = JankHunterConfig.builder()
                .flushIntervalMs(60_000)
                .build()
            val asyncWriter = AsyncLogWriter.open(directory, config, "main")

            asyncWriter.counter("before.close", 1)
            assertTrue(asyncWriter.flushBlocking())

            val text = logText(directory)
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
                .build()
            val asyncWriter = AsyncLogWriter.open(directory, config, "main")
            val writerField = AsyncLogWriter::class.java.getDeclaredField("writer").apply {
                isAccessible = true
            }
            (writerField.get(asyncWriter) as BinaryLogWriter).close()

            asyncWriter.counter("afterclosedwriter", 1)
            assertTrue(asyncWriter.flushBlocking())
            asyncWriter.close()

            val text = logText(directory)
            assertTrue(text.contains("jankhunter.writer_io_error.count"))
            assertTrue(text.contains("afterclosedwriter"))
        } finally {
            directory.deleteRecursively()
        }
    }

    private companion object {
        const val MAGIC_SIZE = 8
    }
}
