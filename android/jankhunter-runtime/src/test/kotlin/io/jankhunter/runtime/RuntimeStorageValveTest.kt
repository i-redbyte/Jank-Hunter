package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import java.util.concurrent.locks.LockSupport
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeStorageValveTest {
    @Test
    fun storageValveDoesNotDependOnTheWholeCollectorService() {
        assertFalse(RuntimeStorageValve::class.java.declaredConstructors.any { constructor ->
            constructor.parameterTypes.any(RuntimeCollectorService::class.java::isAssignableFrom)
        })
    }

    @Test
    fun completedSwitchDoesNotReportSuccessAfterLifecycleWasReplaced() {
        val root = Files.createTempDirectory("jankhunter-storage-valve-stale").toFile()
        try {
            val config = JankHunterConfig.builder().flushIntervalMs(60_000L).build()
            val writer = AsyncLogWriterFactory().open(File(root, "bootstrap"), config, "main")
            val state = RuntimeState().apply {
                this.config = config
                this.writer = writer
            }
            val target = BlockingFileStorage(File(root, "target"))
            val result = AtomicReference<JankHunterStorageSwitchResult>()
            val switchThread = Thread {
                result.set(RuntimeStorageValve(state).switchBinaryStorage(target, timeoutMs = 5_000L))
            }.also(Thread::start)

            assertTrue(target.awaitOpen())
            synchronized(state.lifecycleLock) {
                state.lifecycleGeneration++
            }
            target.releaseOpen()
            switchThread.join(5_000L)

            assertFalse(switchThread.isAlive)
            assertEquals(JankHunterStorageSwitchResult.FAILED, result.get())
            assertTrue(writer.close())
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun deferredSwitchCompletionDoesNotBlockWriterOnLifecycleLock() {
        val root = Files.createTempDirectory("jankhunter-storage-valve-deferred").toFile()
        try {
            val config = JankHunterConfig.builder().flushIntervalMs(60_000L).build()
            val writer = AsyncLogWriterFactory().open(File(root, "bootstrap"), config, "main")
            val state = RuntimeState().apply {
                this.config = config
                this.writer = writer
            }
            val target = BlockingFileStorage(File(root, "target"))
            val initialResult = AtomicReference<JankHunterStorageSwitchResult>()
            writer.counter("storage.valve.deferred", 1L)
            val switchThread = Thread {
                initialResult.set(RuntimeStorageValve(state).switchBinaryStorage(target, timeoutMs = 1_000L))
            }.also(Thread::start)

            assertTrue(target.awaitOpen())
            switchThread.join(5_000L)
            assertFalse(switchThread.isAlive)
            assertEquals(JankHunterStorageSwitchResult.IN_PROGRESS, initialResult.get())
            synchronized(state.lifecycleLock) {
                target.releaseOpen()
                assertTrue(target.awaitWriterCreated())
                assertTrue("writer completion callback waited for lifecycle lock", writer.close(timeoutMs = 1_000L))
            }
            assertTrue(state.config?.binaryStorage() === target)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun deferredSwitchReconciliationWaitsForStorageValveLock() {
        val root = Files.createTempDirectory("jankhunter-storage-valve-ordered").toFile()
        val config = JankHunterConfig.builder().flushIntervalMs(60_000L).build()
        val writer = AsyncLogWriterFactory().open(File(root, "bootstrap"), config, "main")
        val target = BlockingFileStorage(File(root, "target"))
        try {
            val state = RuntimeState().apply {
                this.config = config
                this.writer = writer
            }
            val initialResult = AtomicReference<JankHunterStorageSwitchResult>()
            val switchThread = Thread {
                initialResult.set(RuntimeStorageValve(state).switchBinaryStorage(target, timeoutMs = 1_000L))
            }.also(Thread::start)

            assertTrue(target.awaitOpen())
            switchThread.join(5_000L)
            assertEquals(JankHunterStorageSwitchResult.IN_PROGRESS, initialResult.get())

            synchronized(state.storageValveLock) {
                target.releaseOpen()
                assertTrue(target.awaitWriterCreated())
                val deadline = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(500L)
                while (state.config?.binaryStorage() !== target && System.nanoTime() < deadline) {
                    LockSupport.parkNanos(TimeUnit.MILLISECONDS.toNanos(1L))
                }
                assertFalse(
                    "deferred completion changed storage outside the valve lock",
                    state.config?.binaryStorage() === target,
                )
            }

            val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(5L)
            while (state.config?.binaryStorage() !== target && System.nanoTime() < deadline) {
                LockSupport.parkNanos(TimeUnit.MILLISECONDS.toNanos(1L))
            }
            assertTrue(state.config?.binaryStorage() === target)
            assertTrue(writer.close())
        } finally {
            target.releaseOpen()
            writer.close()
            root.deleteRecursively()
        }
    }

    private class BlockingFileStorage(
        private val directory: File,
    ) : JankHunterBinaryStorage {
        private val openStarted = CountDownLatch(1)
        private val allowOpen = CountDownLatch(1)
        private val writerCreated = CountDownLatch(1)

        override val fileSizeLimitBytes: Long = Long.MAX_VALUE
        override val archivesSizeLimitBytes: Long = Long.MAX_VALUE

        override fun openWriter(fileName: String): JankHunterBinaryWriter {
            openStarted.countDown()
            if (!allowOpen.await(5L, TimeUnit.SECONDS)) throw IOException("test storage open timed out")
            directory.mkdirs()
            return FileWriter(File(directory, fileName)).also { writerCreated.countDown() }
        }

        override fun createArtifact(fileName: String): JankHunterBinaryArtifact {
            directory.mkdirs()
            val file = File(directory, fileName)
            return object : JankHunterBinaryArtifact {
                override val path: String = file.absolutePath
                override fun commit() = Unit
                override fun abort() {
                    file.delete()
                }
            }
        }

        override fun cleanup(protectedPaths: Set<String>) = Unit

        override fun listFiles(): List<String> =
            directory.listFiles().orEmpty().map(File::getAbsolutePath)

        fun awaitOpen(): Boolean = openStarted.await(5L, TimeUnit.SECONDS)

        fun releaseOpen() {
            allowOpen.countDown()
        }

        fun awaitWriterCreated(): Boolean = writerCreated.await(5L, TimeUnit.SECONDS)
    }

    private class FileWriter(file: File) : JankHunterBinaryWriter {
        private val output = FileOutputStream(file, true)
        private var bytes = file.length()
        override val path: String = file.absolutePath

        override fun bytesWritten(): Long = bytes

        override fun writeByte(byte: Byte) {
            output.write(byte.toInt())
            bytes++
        }

        override fun writeBytes(bytes: ByteArray, offset: Int, length: Int) {
            output.write(bytes, offset, length)
            this.bytes += length
        }

        override fun flush() = output.flush()

        override fun close() = output.close()
    }
}
