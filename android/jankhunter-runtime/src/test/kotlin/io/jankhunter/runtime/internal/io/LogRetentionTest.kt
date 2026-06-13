package io.jankhunter.runtime.internal.io

import java.io.File
import java.nio.file.Files
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class LogRetentionTest {
    @Test
    fun deletesOldestSegmentsForCurrentProcessUntilBudgetFits() {
        val directory = Files.createTempDirectory("jankhunter-retention").toFile()
        try {
            val oldMain = segment(directory, "session-main-100-1.jhlog", bytes = 10, modifiedAt = 100)
            val newerMain = segment(directory, "session-main-200-2.jhlog", bytes = 10, modifiedAt = 200)
            val current = segment(directory, "session-main-300-3.jhlog", bytes = 10, modifiedAt = 300)
            val remote = segment(directory, "session-remote-100-1.jhlog", bytes = 100, modifiedAt = 100)

            LogRetention.enforce(directory, "session-main-", current, maxDirectoryBytes = 25)

            assertFalse(oldMain.exists())
            assertTrue(newerMain.exists())
            assertTrue(current.exists())
            assertTrue(remote.exists())
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun keepsCurrentSegmentWhenBudgetIsSmallerThanCurrentFile() {
        val directory = Files.createTempDirectory("jankhunter-retention").toFile()
        try {
            val current = segment(directory, "session-main-100-1.jhlog", bytes = 32, modifiedAt = 100)

            LogRetention.enforce(directory, "session-main-", current, maxDirectoryBytes = 8)

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
