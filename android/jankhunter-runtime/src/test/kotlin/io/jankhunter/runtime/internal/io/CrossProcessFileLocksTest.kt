package io.jankhunter.runtime.internal.io

import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Test

class CrossProcessFileLocksTest {
    @Test
    fun completedLockScopesDoNotAccumulateProcessMonitors() {
        val directory = Files.createTempDirectory("jankhunter-lock-registry").toFile()
        try {
            repeat(512) { index ->
                CrossProcessFileLocks.withDirectoryLock(directory, ".scope-$index.lock") { index }
            }

            assertEquals(0, processLockCount())
        } finally {
            directory.deleteRecursively()
        }
    }

    private fun processLockCount(): Int {
        val field = CrossProcessFileLocks::class.java.getDeclaredField("processLocks")
        field.isAccessible = true
        return (field.get(CrossProcessFileLocks) as Map<*, *>).size
    }
}
