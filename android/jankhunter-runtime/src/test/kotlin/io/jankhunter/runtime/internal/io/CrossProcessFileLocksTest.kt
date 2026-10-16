package io.jankhunter.runtime.internal.io

import java.io.File
import java.io.IOException
import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Assert.assertThrows
import org.junit.Assert.fail
import org.junit.Test

class CrossProcessFileLocksTest {
    @Test
    fun nonblockingSelectionRejectsAnExistingFileLockWithoutRunningTheTransaction() {
        val directory = Files.createTempDirectory("jh-busy-selection").toFile()
        try {
            java.io.RandomAccessFile(File(directory, ".storage-directory.lock"), "rw").use { file ->
                file.channel.lock().use {
                    assertThrows(IOException::class.java) {
                        CrossProcessFileLocks.withoutLockWait {
                            CrossProcessFileLocks.withDirectoryLock(directory, ".storage-directory.lock") {
                                fail("busy storage must not be changed")
                            }
                        }
                    }
                }
            }
            assertEquals(0, processLockCount(directory))
            // The nonblocking mode must not escape into an unrelated transaction.
            CrossProcessFileLocks.withDirectoryLock(directory, ".storage-directory.lock") { }
        } finally { directory.deleteRecursively() }
    }

    @Test
    fun nonblockingSelectionRejectsABusyInProcessMutex() {
        val directory = Files.createTempDirectory("jh-busy-process-selection").toFile()
        val entered = java.util.concurrent.CountDownLatch(1)
        val release = java.util.concurrent.CountDownLatch(1)
        val owner = Thread {
            CrossProcessFileLocks.withDirectoryLock(directory, ".storage-directory.lock") {
                entered.countDown()
                check(release.await(5, java.util.concurrent.TimeUnit.SECONDS))
            }
        }
        try {
            owner.start()
            check(entered.await(5, java.util.concurrent.TimeUnit.SECONDS))
            assertThrows(IOException::class.java) {
                CrossProcessFileLocks.withoutLockWait {
                    CrossProcessFileLocks.withDirectoryLock(directory, ".storage-directory.lock") { fail("busy mutex") }
                }
            }
        } finally { release.countDown(); owner.join(5_000); directory.deleteRecursively() }
    }

    @Test
    fun completedScopesReleaseMonitorsWhileAnUnrelatedOwnerRemainsActive() {
        val directory = Files.createTempDirectory("jankhunter-unrelated-lock").toFile()
        try {
            CrossProcessFileLocks.acquireProcessLock(directory, ".active.lock").use {
                assertCompletedScopesReleaseMonitors()
                assertEquals(1, processLockCount(directory))
            }
            assertEquals(0, processLockCount(directory))
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun completedLockScopesDoNotAccumulateProcessMonitors() {
        assertCompletedScopesReleaseMonitors()
    }

    @Test
    fun nestedScopeFailurePreservesMonitorUntilLastOwnerCloses() {
        val directory = Files.createTempDirectory("jankhunter-nested-lock").toFile()
        val scope = ".nested.lock"
        val expected = IOException("scope failure")
        try {
            CrossProcessFileLocks.acquireProcessLock(directory, scope).use { owner ->
                val actual = assertThrows(IOException::class.java) {
                    CrossProcessFileLocks.withDirectoryLock(directory, scope) {
                        CrossProcessFileLocks.withDirectoryLock(File(directory, "."), scope) {
                            assertEquals(1, processLockCount(directory))
                            throw expected
                        }
                    }
                }
                assertSame(expected, actual)
                assertEquals(1, processLockCount(directory))
                CrossProcessFileLocks.acquireProcessLock(directory, scope).use { other ->
                    assertSame(owner.monitor, other.monitor)
                }
            }
            assertEquals(0, processLockCount(directory))
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun fileOpenFailureReleasesProcessMonitor() {
        val directory = Files.createTempDirectory("jankhunter-failed-lock").toFile()
        try {
            assertThrows(IOException::class.java) {
                CrossProcessFileLocks.withDirectoryLock(directory, ".") {
                    fail("A directory cannot be opened as a lock file")
                }
            }
            assertEquals(0, processLockCount(directory))
        } finally {
            directory.deleteRecursively()
        }
    }

    private fun assertCompletedScopesReleaseMonitors() {
        val directory = Files.createTempDirectory("jankhunter-lock-registry").toFile()
        try {
            repeat(512) { index ->
                CrossProcessFileLocks.withDirectoryLock(directory, ".scope-$index.lock") {
                    assertEquals(1, processLockCount(directory))
                }
                assertEquals(0, processLockCount(directory))
            }
        } finally {
            directory.deleteRecursively()
        }
    }

    private fun processLockCount(directory: File): Int {
        val owner = CrossProcessFileLocks
        val lockField = owner.javaClass.getDeclaredField("registryLock").apply { isAccessible = true }
        val locksField = owner.javaClass.getDeclaredField("processLocks").apply { isAccessible = true }
        val directoryPrefix = "${CrossProcessFileLocks.fileKey(directory)}\u0000"
        // Other components may legitimately keep handles open in this JVM.
        return synchronized(checkNotNull(lockField.get(owner))) {
            (locksField.get(owner) as Map<*, *>).keys.count { key ->
                key is String && key.startsWith(directoryPrefix)
            }
        }
    }
}
