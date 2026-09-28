package io.jankhunter.runtime.internal.io

import java.io.File
import java.io.RandomAccessFile
import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SessionArchiveRetentionTest {
    @Test
    fun oversizedSparseLedgerIsRejectedBeforeAllocatingItsDeclaredLength() {
        val root = tempDir()
        try {
            val retained = archive(root, START_MS, 0L, RUN_A, 50)
            RandomAccessFile(File(root, SessionArchiveRetention.LEDGER_FILE_NAME), "rw").use {
                it.setLength(1L shl 34)
            }

            val result = SessionArchiveRetention.enforce(root, Long.MAX_VALUE)

            assertEquals(0L, result.failed)
            assertTrue(retained.exists())
            assertTrue(File(root, SessionArchiveRetention.LEDGER_FILE_NAME).length() < 1_024L)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun evictsWholeOldestArchiveByPhysicalZipBytes() {
        val root = tempDir()
        try {
            val oldest = archive(root, START_MS, 0L, RUN_A, 70)
            val newest = archive(root, START_MS + 1_000L, 1L, RUN_B, 50)
            val oldestBytes = oldest.length()

            val result = SessionArchiveRetention.enforce(root, newest.length())

            assertEquals(1L, result.deletedArchives)
            assertEquals(oldestBytes, result.deletedBytes)
            assertFalse(oldest.exists())
            assertTrue(newest.exists())
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun oversizedNewestArchiveRemainsReadableWhenBudgetCannotFitAnyArchive() {
        val root = tempDir()
        try {
            val oldest = archive(root, START_MS, 0L, RUN_A, 70)
            val newest = archive(root, START_MS + 1_000L, 1L, RUN_B, 200)
            val oldestBytes = oldest.length()
            val budget = newest.length() - 1L

            val result = SessionArchiveRetention.enforce(root, budget)

            assertFalse(oldest.exists())
            assertTrue(newest.exists())
            assertEquals(1L, result.deletedArchives)
            assertEquals(oldestBytes, result.deletedBytes)
            assertEquals(newest.length(), result.remainingBytes)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun soleOversizedArchiveIsNotDeletedByRetention() {
        val root = tempDir()
        try {
            val archive = archive(root, START_MS, 0L, RUN_A, 200)

            val result = SessionArchiveRetention.enforce(root, archive.length() - 1L)

            assertTrue(archive.exists())
            assertEquals(0L, result.deletedArchives)
            assertEquals(archive.length(), result.remainingBytes)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun recentAccessMovesOldArchiveBehindNewerArchive() {
        val root = tempDir()
        try {
            val oldest = archive(root, START_MS, 0L, RUN_A, 50)
            val newest = archive(root, START_MS + 1_000L, 1L, RUN_B, 50)
            SessionArchiveRetention.enforce(root, Long.MAX_VALUE)
            SessionArchiveRetention.recordAccess(root, oldest)

            val result = SessionArchiveRetention.enforce(root, maxOf(oldest.length(), newest.length()))

            assertEquals(1L, result.deletedArchives)
            assertTrue(oldest.exists())
            assertFalse(newest.exists())
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun corruptLedgerFallsBackToDeterministicOldestSessionFirst() {
        val root = tempDir()
        try {
            val oldest = archive(root, START_MS, 0L, RUN_A, 50)
            val newest = archive(root, START_MS + 1_000L, 1L, RUN_B, 50)
            SessionArchiveRetention.enforce(root, Long.MAX_VALUE)
            File(root, SessionArchiveRetention.LEDGER_FILE_NAME).writeBytes(byteArrayOf(1, 2, 3))

            val result = SessionArchiveRetention.enforce(root, newest.length())

            assertTrue(result.usedOldestFallback)
            assertFalse(oldest.exists())
            assertTrue(newest.exists())
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun protectedArchiveIsNeverEvicted() {
        val root = tempDir()
        try {
            val protected = archive(root, START_MS, 0L, RUN_A, 70)
            val other = archive(root, START_MS + 1_000L, 1L, RUN_B, 50)
            val otherBytes = other.length()

            val result = SessionArchiveRetention.enforce(
                root,
                budgetBytes = protected.length(),
                protectedPaths = setOf(protected.absolutePath),
            )

            assertTrue(protected.exists())
            assertFalse(other.exists())
            assertEquals(otherBytes, result.deletedBytes)
            assertEquals(protected.length(), result.remainingBytes)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun ignoresActiveDirectoriesAndUnknownZipFiles() {
        val root = tempDir()
        try {
            val active = File(root, SessionArtifactPath.sessionDirectoryName(START_MS, 0L, RUN_A)).apply { mkdirs() }
            val unknown = File(root, "user.zip").apply { writeBytes(ByteArray(100)) }

            val result = SessionArchiveRetention.enforce(root, 1L)

            assertEquals(0L, result.deletedArchives)
            assertTrue(active.isDirectory)
            assertTrue(unknown.isFile)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun incompleteCanonicalArchiveIsNeverEvicted() {
        val root = tempDir()
        try {
            val incomplete = File(
                root,
                "${SessionArtifactPath.sessionDirectoryName(START_MS, 0L, RUN_A)}.jhlog.zip",
            ).apply { writeText("incomplete") }

            val result = SessionArchiveRetention.enforce(root, 0L)

            assertTrue(incomplete.exists())
            assertEquals(0L, result.deletedArchives)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun archiveCorruptedAfterLedgerRegistrationIsNeverEvicted() {
        val root = tempDir()
        try {
            val archive = archive(root, START_MS, 0L, RUN_A, 50)
            SessionArchiveRetention.enforce(root, Long.MAX_VALUE)
            archive.writeText("corrupted")

            val result = SessionArchiveRetention.enforce(root, 0L)

            assertTrue(archive.exists())
            assertEquals(0L, result.deletedArchives)
        } finally {
            root.deleteRecursively()
        }
    }

    private fun archive(root: File, startedAt: Long, index: Long, runId: ByteArray, bytes: Int): File {
        val sessionName = SessionArtifactPath.sessionDirectoryName(startedAt, index, runId)
        val processId = ByteArray(16) { byteIndex -> (runId[byteIndex].toInt() xor 0x55).toByte() }
        val process = File(File(root, sessionName), SessionArtifactPath.processDirectoryName(processId)).apply { mkdirs() }
        BinaryLogWriter(
            File(process, SessionLogName.create(DATE, runId, index, 0L)),
            maxDictionaryEntries = 100,
            maxDictionaryValueBytes = 1_024,
            fileHeader = BinaryLogFileHeader(
                runId = runId,
                processInstanceId = processId,
                sessionId = ByteArray(16) { byteIndex -> (byteIndex + 9).toByte() },
                segmentIndex = 0L,
                osPid = 1L,
                collectorStartElapsedUs = 1L,
                segmentStartElapsedUs = 1L,
                segmentStartUnixMs = startedAt,
                identitySource = 0L,
                processName = "main",
                symbolNamespace = ByteArray(0),
            ),
            quality = LogQualityCounters(),
        ).close()
        File(process, "retained-$startedAt-LeakedActivity-1.hprof")
            .writeBytes(ByteArray(bytes) { index.toByte() })
        val result = SessionArchiveCoordinator.maintain(root, SessionLogName.runIdHex(CURRENT_RUN_ID))
        assertEquals(1L, result.archived)
        return File(root, "$sessionName.jhlog.zip").also { archived -> assertTrue(archived.isFile) }
    }

    private fun tempDir(): File = Files.createTempDirectory("jankhunter-archive-retention").toFile()

    private companion object {
        const val START_MS = 1_799_000_000_000L
        const val DATE = "2027-01-02"
        val RUN_A = ByteArray(16) { index -> (index + 1).toByte() }
        val RUN_B = ByteArray(16) { index -> (index + 33).toByte() }
        val CURRENT_RUN_ID = ByteArray(16) { index -> (index + 97).toByte() }
    }
}
