package io.jankhunter.runtime.internal.io

import java.io.File
import java.nio.file.Files
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class LogRetentionTest {
    @Test
    fun deletesOldestCanonicalSessionUntilBudgetFits() {
        val directory = Files.createTempDirectory("jankhunter-retention").toFile()
        try {
            val oldest = segment(directory, SessionLogName.create("2027-01-01", 0L), bytes = 10, modifiedAt = 100)
            val newer = segment(directory, SessionLogName.create("2027-01-01", 1L), bytes = 10, modifiedAt = 200)
            val current = segment(directory, SessionLogName.create("2027-01-02", 0L), bytes = 10, modifiedAt = 300)
            val unrelated = segment(directory, "unrelated.jhlog", bytes = 100, modifiedAt = 100)

            SessionLogRetention.enforce(directory, current, historyLimitBytes = 25)

            assertFalse(oldest.exists())
            assertTrue(newer.exists())
            assertTrue(current.exists())
            assertTrue(unrelated.exists())
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun keepsCurrentSegmentWhenBudgetIsSmallerThanCurrentFile() {
        val directory = Files.createTempDirectory("jankhunter-retention").toFile()
        try {
            val current = segment(directory, SessionLogName.create("2027-01-01", 0L), bytes = 32, modifiedAt = 100)

            SessionLogRetention.enforce(directory, current, historyLimitBytes = 8)

            assertTrue(current.exists())
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
}
