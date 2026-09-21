package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterBinaryArtifact
import io.jankhunter.runtime.JankHunterBinaryStorage
import io.jankhunter.runtime.JankHunterBinaryWriter
import io.jankhunter.runtime.JankHunterConfig
import java.io.File
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertTrue
import org.junit.Assert.assertNull
import org.junit.Test

class AsyncWriterIdleDeadlineTest {
    @Test
    fun bufferedRecordFlushesAtActiveDeadlineWithoutAnotherEvent() = withWriter(100L) { writer, storage ->
        writer.counter("initial.record", 1L)
        assertTrue(writer.flushBlocking(1_000L))
        val flushed = CountDownLatch(1)
        storage.observedFlush.set(flushed)
        writer.counter("deadline.record", 1L)
        assertTrue("buffered record was not flushed at the active deadline", flushed.await(2L, TimeUnit.SECONDS))
    }

    @Test
    fun explicitFlushAndCloseWakeAWriterWithAMaximumPeriodicInterval() = withWriter(Long.MAX_VALUE) { writer, storage ->
        writer.counter("before.wait", 1L)
        val initialFlush = writer.flushBlocking(1_000L)
        assertTrue(writer.terminalFailureCause()?.stackTraceToString(), initialFlush)
        val flushed = CountDownLatch(1)
        storage.observedFlush.set(flushed)
        writer.counter("after.wait", 2L)
        assertTrue(writer.flushBlocking(1_000L))
        assertTrue(flushed.await(1L, TimeUnit.SECONDS))
        assertTrue(writer.close(1_000L))
    }

    private fun withWriter(intervalMs: Long, action: (AsyncLogWriter, Storage) -> Unit) {
        val directory = Files.createTempDirectory("jankhunter-writer-idle").toFile()
        val storage = Storage(directory)
        val writer = AsyncLogWriterFactory().open(
            directory, JankHunterConfig.builder().autoStartCollectors(false)
                .flushIntervalMs(intervalMs).binaryStorage(storage).build(), "idle",
        )
        try {
            action(writer, storage)
            assertNull(writer.terminalFailureCause())
        } finally {
            assertTrue(writer.close(1_000L))
            directory.deleteRecursively()
        }
    }

    private class Storage(private val directory: File) : JankHunterBinaryStorage {
        val observedFlush = AtomicReference<CountDownLatch?>()
        override val fileSizeLimitBytes = Long.MAX_VALUE
        override val archivesSizeLimitBytes = Long.MAX_VALUE

        override fun openWriter(fileName: String): JankHunterBinaryWriter = object : JankHunterBinaryWriter {
            override val path = File(directory, fileName).absolutePath
            private var written = 0L
            override fun bytesWritten(): Long = written
            override fun writeByte(byte: Byte) { written++ }
            override fun writeBytes(bytes: ByteArray, offset: Int, length: Int) { written += length }
            override fun flush() { observedFlush.get()?.countDown() }
            override fun close() = Unit
        }

        override fun createArtifact(fileName: String): JankHunterBinaryArtifact = object : JankHunterBinaryArtifact {
            override val path = File(directory, fileName).absolutePath
            override fun commit() = Unit
            override fun abort() = Unit
        }
        override fun cleanup(protectedPaths: Set<String>) = Unit
        override fun listFiles(): List<String> = emptyList()
    }
}
