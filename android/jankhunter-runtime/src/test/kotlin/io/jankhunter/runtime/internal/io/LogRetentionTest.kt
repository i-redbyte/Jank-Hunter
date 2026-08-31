package io.jankhunter.runtime.internal.io

import java.io.File
import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class LogRetentionTest {
    @Test
    fun unsupportedFileNamesDoNotEnterTheJhlogBudget() {
        val directory = Files.createTempDirectory("jankhunter-retention-unsupported").toFile()
        try {
            val unsupported = segment(directory, "jh-session-log.2027-01-01.7.jhlog", bytes = 20, modifiedAt = 100)
            val current = segment(
                directory,
                SessionLogName.create("2027-01-02", CURRENT_RUN, 0L, 0L),
                bytes = 20,
                modifiedAt = 300,
            )

            val result = SessionLogRetention.enforce(
                directory,
                currentRunId = SessionLogName.runIdHex(CURRENT_RUN),
                protectedPaths = setOf(current.absolutePath),
                historyLimitBytes = 25,
            )

            assertTrue(unsupported.exists())
            assertTrue(current.exists())
            assertEquals(20L, result.totalBefore)
            assertEquals(20L, result.totalAfter)
            assertEquals(0L, result.deletedBytes)
            assertTrue(result.fits)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun deletesEverySegmentOfTheOldestRunUntilBudgetFits() {
        val directory = Files.createTempDirectory("jankhunter-retention").toFile()
        try {
            val oldest = segment(directory, SessionLogName.create("2027-01-01", OLD_RUN, 0L, 0L), bytes = 10, modifiedAt = 100)
            val newer = segment(directory, SessionLogName.create("2027-01-01", OLD_RUN, 0L, 1L), bytes = 10, modifiedAt = 200)
            val current = segment(directory, SessionLogName.create("2027-01-02", CURRENT_RUN, 0L, 0L), bytes = 10, modifiedAt = 300)
            val unrelated = segment(directory, "unrelated.jhlog", bytes = 100, modifiedAt = 100)

            val result = SessionLogRetention.enforce(
                directory,
                currentRunId = SessionLogName.runIdHex(CURRENT_RUN),
                protectedPaths = setOf(current.absolutePath),
                historyLimitBytes = 25,
            )

            assertFalse(oldest.exists())
            assertFalse(newer.exists())
            assertTrue(current.exists())
            assertTrue(unrelated.exists())
            assertTrue(result.fits)
            assertTrue(result.totalAfter == 10L)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun keepsCurrentSegmentWhenBudgetIsSmallerThanCurrentFile() {
        val directory = Files.createTempDirectory("jankhunter-retention").toFile()
        try {
            val current = segment(directory, SessionLogName.create("2027-01-01", CURRENT_RUN, 0L, 0L), bytes = 32, modifiedAt = 100)

            val result = SessionLogRetention.enforce(
                directory,
                currentRunId = SessionLogName.runIdHex(CURRENT_RUN),
                protectedPaths = setOf(current.absolutePath),
                historyLimitBytes = 8,
            )

            assertTrue(current.exists())
            assertFalse(result.fits)
            assertTrue(result.currentRunBytes == 32L)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun hprofDoesNotConsumeJhlogArchiveBudget() {
        val directory = Files.createTempDirectory("jankhunter-retention-hprof").toFile()
        try {
            val old = segment(directory, SessionLogName.create("2027-01-01", OLD_RUN, 0L, 0L), 20, 100)
            val current = segment(directory, SessionLogName.create("2027-01-02", CURRENT_RUN, 0L, 0L), 20, 300)
            val heap = segment(directory, "retained-1.hprof", 1_000, 50)

            val result = SessionLogRetention.enforce(
                directory,
                currentRunId = SessionLogName.runIdHex(CURRENT_RUN),
                protectedPaths = setOf(current.absolutePath),
                historyLimitBytes = 25,
            )

            assertFalse(old.exists())
            assertTrue(current.exists())
            assertTrue(heap.exists())
            assertTrue(result.fits)
            assertTrue(result.totalBefore == 40L)
        } finally {
            directory.deleteRecursively()
        }
    }

    private fun segment(directory: File, name: String, bytes: Int, modifiedAt: Long): File {
        val file = File(directory, name)
        file.writeBytes(ByteArray(bytes) { 1 })
        assertTrue(file.setLastModified(modifiedAt))
        return file
    }

    private companion object {
        val OLD_RUN = ByteArray(16) { 1 }
        val CURRENT_RUN = ByteArray(16) { 2 }
    }
}
