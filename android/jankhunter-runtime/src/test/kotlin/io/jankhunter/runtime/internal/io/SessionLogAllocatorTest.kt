package io.jankhunter.runtime.internal.io

import java.io.File
import java.io.RandomAccessFile
import java.nio.file.Files
import java.util.Collections
import java.util.concurrent.CountDownLatch
import kotlin.concurrent.thread
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class SessionLogAllocatorTest {
    @Test
    fun currentNameDoesNotCoupleFilenameToWireVersion() {
        val fileName = SessionLogName.create(DATE, RUN_ID, dailySessionIndex = 7L, segmentIndex = 0L)
        val additionalSegment = SessionLogName.create(DATE, RUN_ID, dailySessionIndex = 7L, segmentIndex = 2L)

        assertEquals("jh-session-log.$DATE.${SessionLogName.runIdHex(RUN_ID)}.7.jhlog", fileName)
        assertEquals("jh-session-log.$DATE.${SessionLogName.runIdHex(RUN_ID)}.7-2.jhlog", additionalSegment)
        assertEquals(7L, requireNotNull(SessionLogName.parse(fileName)).dailySessionIndex)
        assertEquals(0L, requireNotNull(SessionLogName.parse(fileName)).segmentIndex)
        assertEquals(2L, requireNotNull(SessionLogName.parse(additionalSegment)).segmentIndex)
        assertNull(SessionLogName.parse("jh-session-log.v2.$DATE.${SessionLogName.runIdHex(RUN_ID)}.7.jhlog"))
        assertNull(SessionLogName.parse("jh-session-log.$DATE.${SessionLogName.runIdHex(RUN_ID)}.7-0.jhlog"))
    }

    @Test
    fun obsoleteNameWithoutRunIdIsRejected() {
        assertNull(SessionLogName.parse("jh-session-log.2027-01-02.41.jhlog"))
    }

    @Test
    fun malformedObsoleteNamesRemainRejected() {
        assertNull(SessionLogName.parse("jh-session-log.2027-01-02.01.jhlog"))
        assertNull(SessionLogName.parse("jh-session-log.2027-02-30.1.jhlog"))
        assertNull(SessionLogName.parse("jh-session-log.2027-01-02.-1.jhlog"))
        assertNull(SessionLogName.parse("jh-session-log.2027-01-02.jhlog"))
    }

    @Test
    fun authoritativeExternalStorageReusesUnpublishedZeroReservation() {
        val directory = Files.createTempDirectory("jankhunter-external-sequence-reset").toFile()
        try {
            SessionLogAllocator.reserve(directory, DATE, RUN_ID, dailySessionIndex = 3L).close()

            val allocation = SessionLogAllocator.reserve(
                directory,
                DATE,
                RUN_ID,
                dailySessionIndex = 3L,
                authoritativeStoragePaths = emptyList(),
            )
            try {
                assertEquals(0L, allocation.segmentIndex)
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
            SessionLogAllocator.reserve(directory, DATE, RUN_ID, dailySessionIndex = 3L).close()
            val visibleZero = "/external/storage/${SessionLogName.create(DATE, RUN_ID, 3L, 0L)}"

            val allocation = SessionLogAllocator.reserve(
                directory,
                DATE,
                RUN_ID,
                dailySessionIndex = 3L,
                authoritativeStoragePaths = listOf(visibleZero),
            )
            try {
                assertEquals(1L, allocation.segmentIndex)
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
                    allocations += SessionLogAllocator.reserve(directory, DATE, RUN_ID, dailySessionIndex = 3L)
                }
            }
            start.countDown()
            workers.forEach { worker -> worker.join() }

            assertEquals((0L until 32L).toList(), allocations.map { it.segmentIndex }.sorted())
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
                        RUN_ID,
                        dailySessionIndex = 3L,
                        authoritativeStoragePaths = emptyList(),
                    )
                }
            }
            start.countDown()
            workers.forEach { worker -> worker.join() }

            assertEquals((0L until 32L).toList(), allocations.map { it.segmentIndex }.sorted())
        } finally {
            allocations.forEach { allocation -> allocation.close() }
            directory.deleteRecursively()
        }
    }

    @Test
    fun retentionNeverDeletesFilesOwnedByActiveLeases() {
        val directory = Files.createTempDirectory("jankhunter-retention-leases").toFile()
        try {
            val active = SessionLogAllocator.reserve(directory, DATE, RUN_ID, dailySessionIndex = 3L)
            val activeFile = File(directory, active.fileName).apply { writeBytes(ByteArray(100)) }
            val stale = SessionLogAllocator.reserve(directory, DATE, OLD_RUN_ID, dailySessionIndex = 2L)
            val staleFile = File(directory, stale.fileName).apply { writeBytes(ByteArray(100)) }
            stale.close()
            val current = SessionLogAllocator.reserve(directory, DATE, RUN_ID, dailySessionIndex = 3L)
            val currentFile = File(directory, current.fileName).apply { writeBytes(ByteArray(100)) }

            try {
                SessionLogRetention.enforce(
                    directory,
                    currentRunId = SessionLogName.runIdHex(RUN_ID),
                    protectedPaths = SessionLogAllocator.activeLeases(directory).protectedPaths,
                    historyLimitBytes = 150L,
                )

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
            val allocation = SessionLogAllocator.reserve(directory, DATE, RUN_ID, dailySessionIndex = 3L)
            val externalPath = "/external/storage/${allocation.fileName}"
            allocation.updateProtectedPath(externalPath)

            assertTrue(externalPath in SessionLogAllocator.activeLeases(directory).protectedPaths)
            allocation.close()
            assertTrue(SessionLogAllocator.activeLeases(directory).protectedPaths.isEmpty())
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun obsoleteCleanerPreservesCurrentAndActivelyLeasedLogs() {
        val directory = Files.createTempDirectory("jankhunter-obsolete-cleaner").toFile()
        val current = File(directory, SessionLogName.create(DATE, RUN_ID, dailySessionIndex = 0L, segmentIndex = 0L))
        val obsoleteName = "jh-session-log.v2.$DATE.${SessionLogName.runIdHex(OLD_RUN_ID)}.0.jhlog"
        val obsolete = File(directory, obsoleteName)
        val currentWithLegacyName = File(
            directory,
            "jh-session-log.v2.$DATE.${SessionLogName.runIdHex(RUN_ID)}.9.jhlog",
        )
        val unrelated = File(directory, "application.jhlog")
        val future = File(directory, "jh-session-log.v4.$DATE.opaque.0.jhlog")
        val malformedCurrent = File(directory, "jh-session-log.$DATE.invalid.jhlog")
        val obsoleteSequence = File(directory, ".jh-session-log.v2.$DATE.seq")
        val legacyUnversionedSequence = File(directory, ".jh-session-log.$DATE.seq")
        val currentSequence = File(directory, ".jh-session-index.$DATE.seq")
        val futureSequence = File(directory, ".jh-session-log.v4.$DATE.seq")
        val lease = File(directory, ".${obsoleteName.removeSuffix(SessionLogName.SUFFIX)}.lease")
        try {
            current.writeBytes(Jhlog.FILE_MAGIC)
            obsolete.writeBytes(formatMagic(major = 2))
            currentWithLegacyName.writeBytes(Jhlog.FILE_MAGIC)
            unrelated.writeBytes(byteArrayOf(3))
            future.writeBytes(formatMagic(major = 4))
            malformedCurrent.writeBytes(byteArrayOf(5))
            obsoleteSequence.writeBytes(byteArrayOf(4))
            legacyUnversionedSequence.writeBytes(byteArrayOf(4))
            currentSequence.writeBytes(byteArrayOf(5))
            futureSequence.writeBytes(byteArrayOf(6))
            lease.writeText(obsoleteName)
            RandomAccessFile(lease, "rw").use { randomAccess ->
                val lock = randomAccess.channel.lock()
                try {
                    val first = ObsoleteSessionLogCleaner.clean(directory, storage = null)
                    assertEquals(2L, first.deleted)
                    assertEquals(1L, first.protected)
                    assertTrue(obsolete.exists())
                    assertFalse(obsoleteSequence.exists())
                    assertFalse(legacyUnversionedSequence.exists())
                } finally {
                    lock.release()
                }
            }

            val second = ObsoleteSessionLogCleaner.clean(directory, storage = null)
            assertEquals(1L, second.deleted)
            assertFalse(obsolete.exists())
            assertTrue(current.exists())
            assertTrue(currentWithLegacyName.exists())
            assertTrue(unrelated.exists())
            assertTrue(future.exists())
            assertTrue(malformedCurrent.exists())
            assertTrue(currentSequence.exists())
            assertTrue(futureSequence.exists())
        } finally {
            directory.deleteRecursively()
        }
    }

    private fun formatMagic(major: Int): ByteArray = Jhlog.FILE_MAGIC.copyOf().also { magic ->
        magic[8] = major.toByte()
        magic[9] = 0
        magic[10] = 0
    }

    private companion object {
        const val DATE = "2027-01-02"
        val RUN_ID = ByteArray(16) { index -> index.toByte() }
        val OLD_RUN_ID = ByteArray(16) { 42 }
    }
}
