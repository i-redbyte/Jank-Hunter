package io.jankhunter.runtime.internal.io

import java.io.File
import java.io.RandomAccessFile
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.nio.file.Files
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assert.assertThrows
import org.junit.Test

class ProcessRunCohortTest {
    @Test
    fun liveParticipantsShareIdentityAndDailySessionIndex() {
        val directory = Files.createTempDirectory("jh-run-cohort").toFile()
        try {
            val first = ProcessRunCohort.join(directory, DATE)
            val firstRunId = first.runId()
            assertEquals(DATE, first.localDate())
            assertEquals(0L, first.dailySessionIndex())
            val second = ProcessRunCohort.join(directory, DATE)
            assertArrayEquals(firstRunId, second.runId())
            assertEquals(0L, second.dailySessionIndex())
            val afterMidnightParticipant = ProcessRunCohort.join(directory, NEXT_DATE)
            assertArrayEquals(firstRunId, afterMidnightParticipant.runId())
            assertEquals(DATE, afterMidnightParticipant.localDate())
            assertEquals(0L, afterMidnightParticipant.dailySessionIndex())

            first.close()
            val third = ProcessRunCohort.join(directory, DATE)
            assertArrayEquals(firstRunId, third.runId())
            assertEquals(0L, third.dailySessionIndex())

            second.close()
            third.close()
            afterMidnightParticipant.close()
            ProcessRunCohort.join(directory, DATE).use { next ->
                assertFalse(firstRunId.contentEquals(next.runId()))
                assertEquals(1L, next.dailySessionIndex())
            }
            ProcessRunCohort.join(directory, NEXT_DATE).use { nextDay ->
                assertEquals(NEXT_DATE, nextDay.localDate())
                assertEquals(0L, nextDay.dailySessionIndex())
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
                        ProcessRunCohort.join(directory, DATE)
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
            val first = ProcessRunCohort.join(directory, DATE)
            val firstLease = directory.listFiles { candidate -> candidate.name.endsWith(".lease") }
                .orEmpty()
                .single()
            val second = ProcessRunCohort.join(directory, DATE)

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
        val lease = ProcessRunCohort.join(directory, DATE)
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

    @Test
    fun corruptDailySequenceFailsClosed() {
        val directory = Files.createTempDirectory("jh-run-sequence-corrupt").toFile()
        try {
            val corrupt = ByteBuffer.allocate(Long.SIZE_BYTES * 2)
                .order(ByteOrder.LITTLE_ENDIAN)
                .putLong(1L)
                .putLong(1L)
                .array()
            File(directory, SessionLogName.sequenceFileName(DATE)).writeBytes(corrupt)

            assertThrows(java.io.IOException::class.java) {
                ProcessRunCohort.join(directory, DATE)
            }
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun incompleteDailySequenceTailIsDiscardedBeforeFirstSession() {
        val directory = Files.createTempDirectory("jh-run-sequence-tail").toFile()
        val sequence = File(directory, SessionLogName.sequenceFileName(DATE))
        try {
            sequence.writeBytes(byteArrayOf(1, 2, 3, 4))

            ProcessRunCohort.join(directory, DATE).use { lease ->
                assertEquals(0L, lease.dailySessionIndex())
                assertEquals(Long.SIZE_BYTES * 2L, sequence.length())
            }
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun absentSequenceContinuesAfterHighestAuthoritativeDailySession() {
        val directory = Files.createTempDirectory("jh-run-storage-sequence").toFile()
        val storedRunId = ByteArray(16) { 7 }
        val storedPath = "/external/storage/${SessionLogName.create(DATE, storedRunId, 7L, 0L)}"
        try {
            ProcessRunCohort.join(directory, DATE, listOf(storedPath)).use { lease ->
                assertEquals(8L, lease.dailySessionIndex())
            }
        } finally {
            directory.deleteRecursively()
        }
    }

    private companion object {
        const val COHORT_DIRECTORY_LOCK = ".jh-run-cohort.lock"
        const val DATE = "2027-01-02"
        const val NEXT_DATE = "2027-01-03"
    }
}
