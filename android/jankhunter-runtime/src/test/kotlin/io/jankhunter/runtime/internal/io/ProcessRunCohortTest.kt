package io.jankhunter.runtime.internal.io

import java.io.File
import java.io.RandomAccessFile
import java.nio.file.Files
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assert.assertThrows
import org.junit.Test

class ProcessRunCohortTest {
    @Test
    fun liveParticipantsShareIdentityAndLastCloseEndsCohort() {
        val directory = Files.createTempDirectory("jh-run-cohort").toFile()
        try {
            val first = ProcessRunCohort.join(directory)
            val firstRunId = first.runId()
            val second = ProcessRunCohort.join(directory)
            assertArrayEquals(firstRunId, second.runId())

            first.close()
            val third = ProcessRunCohort.join(directory)
            assertArrayEquals(firstRunId, third.runId())

            second.close()
            third.close()
            ProcessRunCohort.join(directory).use { next ->
                assertFalse(firstRunId.contentEquals(next.runId()))
            }
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun corruptActiveLeaseFailsClosed() {
        val directory = Files.createTempDirectory("jh-run-corrupt").toFile()
        val file = File(directory, ".jh-run-cohort.corrupt.lease")
        try {
            RandomAccessFile(file, "rw").use { randomAccess ->
                randomAccess.write(byteArrayOf(1, 2, 3))
                randomAccess.channel.lock().use {
                    assertThrows(java.io.IOException::class.java) {
                        ProcessRunCohort.join(directory)
                    }
                }
            }
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun repeatedJoinDoesNotReleaseTheCurrentProcessLease() {
        val directory = Files.createTempDirectory("jh-run-own-lease").toFile()
        try {
            val first = ProcessRunCohort.join(directory)
            val firstLease = directory.listFiles { candidate -> candidate.name.endsWith(".lease") }
                .orEmpty()
                .single()
            val second = ProcessRunCohort.join(directory)

            assertFalse("cohort scan released its own POSIX lease", ExternalFileLockProbe.canAcquire(firstLease))
            first.close()
            assertTrue(ExternalFileLockProbe.canAcquire(firstLease))
            second.close()
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun failedDirectoryLockKeepsCohortLeaseActiveUntilCloseIsRetried() {
        val directory = Files.createTempDirectory("jh-run-close-failure").toFile()
        val lease = ProcessRunCohort.join(directory)
        val leaseFile = directory.listFiles { candidate -> candidate.name.endsWith(".lease") }
            .orEmpty()
            .single()
        val lockPath = File(directory, COHORT_DIRECTORY_LOCK)
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

    private companion object {
        const val COHORT_DIRECTORY_LOCK = ".jh-run-cohort.lock"
    }
}
