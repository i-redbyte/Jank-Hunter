package io.jankhunter.runtime.internal.io

import java.io.File
import java.io.IOException
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.nio.file.Files
import java.util.Collections
import java.util.concurrent.CountDownLatch
import kotlin.concurrent.thread
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class SessionLogAllocatorTest {
    @Test
    fun deletedHighestIndexIsNeverReused() {
        val directory = Files.createTempDirectory("jankhunter-sequence-delete").toFile()
        try {
            val first = SessionLogAllocator.reserve(directory, DATE)
            val firstFile = File(directory, first.fileName).apply { writeBytes(byteArrayOf(1)) }
            first.close()
            assertTrue(firstFile.delete())

            val second = SessionLogAllocator.reserve(directory, DATE)
            try {
                assertEquals(1L, second.index)
            } finally {
                second.close()
            }
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun fullCorruptSequenceRecordFailsClosed() {
        val directory = Files.createTempDirectory("jankhunter-sequence-corrupt").toFile()
        try {
            val corrupt = ByteBuffer.allocate(Long.SIZE_BYTES * 2)
                .order(ByteOrder.LITTLE_ENDIAN)
                .putLong(1L)
                .putLong(1L)
                .array()
            sequenceFile(directory).writeBytes(corrupt)

            try {
                SessionLogAllocator.reserve(directory, DATE)
                fail("corrupt sequence must not allocate an index")
            } catch (_: IOException) {
                // Expected: reusing an index is less safe than disabling this session.
            }
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun incompleteTrailingSequenceRecordIsDiscardedUnderLock() {
        val directory = Files.createTempDirectory("jankhunter-sequence-tail").toFile()
        try {
            sequenceFile(directory).writeBytes(byteArrayOf(1, 2, 3, 4))

            val allocation = SessionLogAllocator.reserve(directory, DATE)
            try {
                assertEquals(0L, allocation.index)
                assertEquals(Long.SIZE_BYTES * 2L, sequenceFile(directory).length())
            } finally {
                allocation.close()
            }
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun authoritativeExternalStorageReusesUnpublishedZeroReservation() {
        val directory = Files.createTempDirectory("jankhunter-external-sequence-reset").toFile()
        try {
            SessionLogAllocator.reserve(directory, DATE).close()

            val allocation = SessionLogAllocator.reserve(
                directory,
                DATE,
                authoritativeStoragePaths = emptyList(),
            )
            try {
                assertEquals(0L, allocation.index)
            } finally {
                allocation.close()
            }
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun authoritativeExternalStorageContinuesAfterVisibleZero() {
        val directory = Files.createTempDirectory("jankhunter-external-sequence-visible").toFile()
        try {
            SessionLogAllocator.reserve(directory, DATE).close()
            val visibleZero = "/external/storage/${SessionLogName.create(DATE, 0L)}"

            val allocation = SessionLogAllocator.reserve(
                directory,
                DATE,
                authoritativeStoragePaths = listOf(visibleZero),
            )
            try {
                assertEquals(1L, allocation.index)
            } finally {
                allocation.close()
            }
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun concurrentReservationsAreUniqueAndMonotonic() {
        val directory = Files.createTempDirectory("jankhunter-sequence-concurrent").toFile()
        val allocations = Collections.synchronizedList(mutableListOf<SessionLogAllocator.Allocation>())
        val start = CountDownLatch(1)
        try {
            val workers = List(32) {
                thread(start = true) {
                    start.await()
                    allocations += SessionLogAllocator.reserve(directory, DATE)
                }
            }
            start.countDown()
            workers.forEach { worker -> worker.join() }

            assertEquals((0L until 32L).toList(), allocations.map { it.index }.sorted())
        } finally {
            allocations.forEach { allocation -> allocation.close() }
            directory.deleteRecursively()
        }
    }

    @Test
    fun concurrentAuthoritativeExternalReservationsAreUniqueAndMonotonic() {
        val directory = Files.createTempDirectory("jankhunter-external-sequence-concurrent").toFile()
        val allocations = Collections.synchronizedList(mutableListOf<SessionLogAllocator.Allocation>())
        val start = CountDownLatch(1)
        try {
            val workers = List(32) {
                thread(start = true) {
                    start.await()
                    allocations += SessionLogAllocator.reserve(
                        directory,
                        DATE,
                        authoritativeStoragePaths = emptyList(),
                    )
                }
            }
            start.countDown()
            workers.forEach { worker -> worker.join() }

            assertEquals((0L until 32L).toList(), allocations.map { it.index }.sorted())
        } finally {
            allocations.forEach { allocation -> allocation.close() }
            directory.deleteRecursively()
        }
    }

    @Test
    fun retentionNeverDeletesFilesOwnedByActiveLeases() {
        val directory = Files.createTempDirectory("jankhunter-retention-leases").toFile()
        try {
            val active = SessionLogAllocator.reserve(directory, DATE)
            val activeFile = File(directory, active.fileName).apply { writeBytes(ByteArray(100)) }
            val stale = SessionLogAllocator.reserve(directory, DATE)
            val staleFile = File(directory, stale.fileName).apply { writeBytes(ByteArray(100)) }
            stale.close()
            val current = SessionLogAllocator.reserve(directory, DATE)
            val currentFile = File(directory, current.fileName).apply { writeBytes(ByteArray(100)) }

            try {
                SessionLogRetention.enforce(directory, currentFile, historyLimitBytes = 150L)

                assertTrue(activeFile.exists())
                assertTrue(currentFile.exists())
                assertFalse(staleFile.exists())
            } finally {
                current.close()
                active.close()
            }
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun activeLeasePublishesExternalProtectedPath() {
        val directory = Files.createTempDirectory("jankhunter-external-lease").toFile()
        try {
            val allocation = SessionLogAllocator.reserve(directory, DATE)
            val externalPath = "/external/storage/${allocation.fileName}"
            allocation.updateProtectedPath(externalPath)

            assertTrue(externalPath in SessionLogAllocator.activeLeases(directory).protectedPaths)
            allocation.close()
            assertTrue(SessionLogAllocator.activeLeases(directory).protectedPaths.isEmpty())
        } finally {
            directory.deleteRecursively()
        }
    }

    private fun sequenceFile(directory: File): File {
        return File(directory, ".${SessionLogName.PREFIX}$DATE.seq")
    }

    private companion object {
        const val DATE = "2027-01-02"
    }
}
