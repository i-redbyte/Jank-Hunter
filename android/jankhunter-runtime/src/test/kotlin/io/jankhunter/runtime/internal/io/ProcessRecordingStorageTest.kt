package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterBinaryArtifact
import io.jankhunter.runtime.JankHunterBinaryStorage
import io.jankhunter.runtime.JankHunterBinaryWriter
import io.jankhunter.runtime.JankHunterConfig
import io.jankhunter.runtime.JankHunterStorageSwitchResult
import java.io.File
import java.io.FileOutputStream
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.FutureTask
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ProcessRecordingStorageTest {
    @Test
    fun blockedExternalCloseReportsInProgressBeforeSuccessfulRestoration() {
        val root = Files.createTempDirectory("jh-storage-delayed-restore").toFile()
        val storage = TrackingStorage(File(root, "external"))
        val recording = ProcessRecordingSession()
        val writer = AsyncLogWriterFactory(recording).open(
            File(root, "internal"), JankHunterConfig.builder().autoStartCollectors(false).build(), "main",
        )
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val restore = FutureTask { writer.switchBinaryStorageBlocking(null, 100L) }
        val restoreThread = Thread(restore)
        try {
            writer.counter("before.delay", 1L)
            assertEquals(JankHunterStorageSwitchResult.SWITCHED, writer.switchBinaryStorageBlocking(storage, 5_000L))
            writer.counter("during.delay", 1L)
            storage.beforeClose = { entered.countDown(); release.await() }
            restoreThread.start()
            assertTrue(entered.await(5L, TimeUnit.SECONDS))
            assertEquals(JankHunterStorageSwitchResult.IN_PROGRESS, restore.get(5L, TimeUnit.SECONDS))
            release.countDown()
            assertNotNull(writer.captureSnapshotBlocking(5_000L))
            assertEquals(0, storage.listFiles().size)
            assertEquals(1, root.walkTopDown().count { it.isFile && it.extension == "jhlog" })
        } finally {
            release.countDown()
            storage.beforeClose = null
            restoreThread.join(5_000L)
            writer.close()
            recording.close()
            root.deleteRecursively()
        }
    }

    @Test
    fun nextEpochWaitsForPreviousWriterToReleaseTheFile() {
        val root = Files.createTempDirectory("jh-process-owner-handoff").toFile()
        val storage = TrackingStorage(File(root, "external"))
        val recording = ProcessRecordingSession()
        val factory = AsyncLogWriterFactory(recording = recording)
        val config = JankHunterConfig.builder().autoStartCollectors(false).binaryStorage(storage).build()
        val first = factory.open(File(root, "logs"), config, "main")
        first.counter("before", 1L)
        assertTrue(first.flushBlocking(5_000L))
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        storage.beforeClose = { entered.countDown(); release.await() }
        val closing = Thread { first.close() }
        closing.start()
        assertTrue(entered.await(5L, TimeUnit.SECONDS))
        val second = factory.open(File(root, "logs"), config, "main")
        try {
            second.counter("after", 1L)
            assertNull(second.captureSnapshotBlocking(25L))
            storage.beforeClose = null
            release.countDown()
            closing.join(5_000L)
            assertNotNull("next epoch must resume after previous close", second.captureSnapshotBlocking(5_000L))
            assertEquals(1, storage.listFiles().size)
        } finally {
            storage.beforeClose = null
            release.countDown()
            closing.join(5_000L)
            first.close()
            second.close()
            recording.close()
            root.deleteRecursively()
        }
    }

    @Test
    fun repeatedHandoffsRetainOneFileAndReleaseEveryProtection() {
        val root = Files.createTempDirectory("jh-process-handoffs").toFile()
        val storage = TrackingStorage(File(root, "external"))
        val recording = ProcessRecordingSession()
        val writer = AsyncLogWriterFactory(recording = recording).open(
            File(root, "internal"), JankHunterConfig.builder().autoStartCollectors(false).build(), "main",
        )
        try {
            writer.counter("handoff.before", 1L)
            repeat(5) {
                assertEquals(JankHunterStorageSwitchResult.SWITCHED, writer.switchBinaryStorageBlocking(storage, 5_000L))
                assertEquals(1, storage.protections)
                writer.counter("handoff.external", 1L)
                assertEquals(JankHunterStorageSwitchResult.SWITCHED, writer.switchBinaryStorageBlocking(null, 5_000L))
                assertEquals(0, storage.protections)
                writer.counter("handoff.internal", 1L)
            }
            assertTrue(writer.close())
            assertEquals(1, root.walkTopDown().count { it.isFile && it.extension == "jhlog" })
        } finally {
            writer.close()
            recording.close()
            assertEquals(0, storage.protections)
            root.deleteRecursively()
        }
    }

    private class TrackingStorage(private val root: File) : JankHunterBinaryStorage {
        @Volatile var beforeClose: (() -> Unit)? = null
        var protections = 0
            private set
        override val fileSizeLimitBytes = Long.MAX_VALUE
        override val archivesSizeLimitBytes = Long.MAX_VALUE
        init { root.mkdirs() }
        override fun openWriter(fileName: String): JankHunterBinaryWriter {
            val file = File(root, fileName)
            val output = FileOutputStream(file, true)
            return object : JankHunterBinaryWriter {
                override val path = file.absolutePath
                override fun bytesWritten(): Long = file.length()
                override fun writeByte(byte: Byte) = output.write(byte.toInt())
                override fun writeBytes(bytes: ByteArray, offset: Int, length: Int) = output.write(bytes, offset, length)
                override fun flush() = output.flush()
                override fun close() { beforeClose?.invoke(); output.close() }
            }
        }
        override fun createArtifact(fileName: String): JankHunterBinaryArtifact {
            protections++
            return object : JankHunterBinaryArtifact {
                override val path = File(root, fileName).absolutePath
                private var open = true
                override fun commit() { if (open) { open = false; protections-- } }
                override fun abort() { commit(); File(path).delete() }
            }
        }
        override fun cleanup(protectedPaths: Set<String>) = Unit
        override fun listFiles(): List<String> = root.listFiles().orEmpty().map(File::getAbsolutePath)
    }
}
