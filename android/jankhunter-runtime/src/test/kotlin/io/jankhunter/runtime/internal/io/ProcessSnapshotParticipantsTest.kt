package io.jankhunter.runtime.internal.io

import java.io.File
import java.io.RandomAccessFile
import java.nio.file.Files
import java.util.concurrent.TimeUnit
import kotlin.concurrent.thread
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ProcessSnapshotParticipantsTest {
    @Test
    fun activeReturnsEveryLockedProcessAndCloseRemovesIt() {
        val directory = Files.createTempDirectory("jankhunter-snapshot-participants").toFile()
        try {
            val main = ProcessSnapshotParticipants.join(directory, "com.app")
            val remote = ProcessSnapshotParticipants.join(directory, "com.app:remote")

            assertEquals(
                setOf("com.app", "com.app:remote"),
                ProcessSnapshotParticipants.active(directory).mapTo(hashSetOf()) { it.processName },
            )
            assertTrue(ProcessSnapshotParticipants.active(directory).all { it.id.length == 32 })

            remote.close()
            assertEquals(
                listOf("com.app"),
                ProcessSnapshotParticipants.active(directory).map { it.processName },
            )
            main.close()
            assertTrue(ProcessSnapshotParticipants.active(directory).isEmpty())
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun activeDoesNotReleaseTheCurrentProcessLease() {
        val directory = Files.createTempDirectory("jankhunter-snapshot-own-lease").toFile()
        try {
            val lease = ProcessSnapshotParticipants.join(directory, "com.app")
            val leaseFile = directory.listFiles { file -> file.name.endsWith(".lease") }.orEmpty().single()

            ProcessSnapshotParticipants.active(directory)
            ProcessSnapshotParticipants.active(directory)

            assertFalse("participant scan released its own POSIX lease", ExternalFileLockProbe.canAcquire(leaseFile))
            lease.close()
            assertTrue(ExternalFileLockProbe.canAcquire(leaseFile))
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun concurrentCloseCannotExposeTheLeaseToAnActiveScan() {
        val directory = Files.createTempDirectory("jankhunter-snapshot-close-race").toFile()
        val lease = ProcessSnapshotParticipants.join(directory, "main")
        val leaseFile = directory.listFiles { candidate -> candidate.name.endsWith(".lease") }
            .orEmpty()
            .single()
        val closeThread = thread(start = false, name = "snapshot-participant-close") { lease.close() }
        try {
            CrossProcessFileLocks.withDirectoryLock(directory, SNAPSHOT_DIRECTORY_LOCK) {
                closeThread.start()
                assertTrue(waitUntilBlocked(closeThread))

                ProcessSnapshotParticipants.active(directory)

                assertFalse("concurrent close exposed the lease during a scan", ExternalFileLockProbe.canAcquire(leaseFile))
            }
            closeThread.join(1_000L)
            assertFalse(closeThread.isAlive)
            assertTrue(ExternalFileLockProbe.canAcquire(leaseFile))
        } finally {
            closeThread.join(1_000L)
            lease.close()
            directory.deleteRecursively()
        }
    }

    @Test
    fun failedDirectoryLockKeepsParticipantLeaseActiveUntilCloseIsRetried() {
        val directory = Files.createTempDirectory("jankhunter-snapshot-close-failure").toFile()
        val lease = ProcessSnapshotParticipants.join(directory, "main")
        val leaseFile = directory.listFiles { candidate -> candidate.name.endsWith(".lease") }
            .orEmpty()
            .single()
        val lockPath = File(directory, SNAPSHOT_DIRECTORY_LOCK)
        try {
            assertTrue(lockPath.delete())
            assertTrue(lockPath.mkdir())

            val failure = runCatching { lease.close() }.exceptionOrNull()

            assertTrue(failure is java.io.IOException)
            assertFalse("failed close released an unserialized lease", ExternalFileLockProbe.canAcquire(leaseFile))

            assertTrue(lockPath.delete())
            lease.close()
            assertTrue(ExternalFileLockProbe.canAcquire(leaseFile))
        } finally {
            if (lockPath.isDirectory) lockPath.delete()
            runCatching { lease.close() }
            directory.deleteRecursively()
        }
    }

    private fun waitUntilBlocked(thread: Thread): Boolean {
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(5L)
        while (System.nanoTime() < deadline) {
            if (thread.state == Thread.State.BLOCKED) return true
            Thread.yield()
        }
        return false
    }

    private companion object {
        const val SNAPSHOT_DIRECTORY_LOCK = ".jh-snapshot-participants.lock"
    }

    @Test
    fun corruptActiveParticipantFailsClosed() {
        val directory = Files.createTempDirectory("jankhunter-snapshot-corrupt").toFile()
        try {
            val corrupt = directory.resolve(".jh-snapshot-participant.${"a".repeat(32)}.lease")
            corrupt.writeBytes(byteArrayOf(1, 2, 3))
            RandomAccessFile(corrupt, "rw").use { access ->
                access.channel.lock().use {
                    val failure = runCatching { ProcessSnapshotParticipants.active(directory) }.exceptionOrNull()
                    assertTrue(failure is java.io.IOException)
                }
            }
        } finally {
            directory.deleteRecursively()
        }
    }
}
