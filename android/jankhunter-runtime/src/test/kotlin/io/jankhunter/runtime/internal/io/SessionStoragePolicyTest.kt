package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterConfig
import io.jankhunter.runtime.JankHunterStoragePolicy
import java.io.File
import java.io.IOException
import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SessionStoragePolicyTest {
    @Test
    fun hostPolicyOverridesSdkBudgetWithoutUnitConversion() {
        val root = Files.createTempDirectory("jh-policy-priority").toFile()
        try {
            val config = JankHunterConfig.builder().maxSessionLogSizeMiB(1)
                .sessionLogSizeLimitEnabled(false).storagePolicy(policy(root, total = 50_000_000L)).build()
            assertEquals(50_000_000L, effectiveArchiveLimitBytes(config, null))
        } finally { root.deleteRecursively() }
    }

    @Test
    fun rootRemainsPinnedForProcessLifetime() {
        val root = Files.createTempDirectory("jh-policy-root").toFile()
        val recording = ProcessRecordingSession()
        try {
            assertEquals(root.resolve("first").absoluteFile, recording.resolveDirectory(root.resolve("first")))
            assertEquals(root.resolve("first").absoluteFile, recording.resolveDirectory(root.resolve("later")))
        } finally { recording.close(); root.deleteRecursively() }
    }

    @Test
    fun totalBudgetRejectsHeapWhilePreservingEveryLogAndPendingSource() {
        val root = Files.createTempDirectory("jh-policy-heap").toFile()
        try {
            val process = process(root)
            val log = process.resolve(SessionLogName.create("2026-09-25", RUN, 0L, 0L))
                .apply { writeBytes(ByteArray(100)) }
            val pending = process.resolve(".heap.pending").apply { writeBytes(ByteArray(101)) }
            val destination = process.resolve("retained-1790352000000-Owner-1.hprof")
            assertFalse(SessionStorageBudget.publishHeapDump(root, policy(root, total = 200), pending, destination))
            assertTrue(pending.exists())
            assertFalse(destination.exists())
            assertEquals(100L, log.length())
        } finally { root.deleteRecursively() }
    }

    @Test
    fun heapExemptionDoesNotBypassTotalBudgetAndCompletedHeapIsReclaimedFirst() {
        val root = Files.createTempDirectory("jh-policy-reclaim").toFile()
        try {
            val process = process(root)
            val log = process.resolve(SessionLogName.create("2026-09-25", RUN, 0L, 0L))
                .apply { writeBytes(ByteArray(60)) }
            val previous = process.resolve("retained-1790352000000-Owner-1.hprof").apply { writeBytes(ByteArray(80)) }
            val pending = process.resolve(".heap.pending").apply { writeBytes(ByteArray(80)) }
            val destination = process.resolve("retained-1790352000001-Owner-2.hprof")
            assertTrue(SessionStorageBudget.publishHeapDump(root, policy(root, perFile = 70, total = 150), pending, destination))
            assertFalse(previous.exists())
            assertTrue(destination.exists())
            assertFalse(pending.exists())
            assertEquals(60L, log.length())
        } finally { root.deleteRecursively() }
    }

    @Test
    fun completedHeapWithoutExemptionCannotExceedPerFileLimit() {
        val root = Files.createTempDirectory("jh-policy-heap-cap").toFile()
        try {
            val process = process(root)
            val pending = process.resolve(".heap.pending").apply { writeBytes(ByteArray(80)) }
            val destination = process.resolve("retained-1790352000000-Owner-1.hprof")
            val policy = JankHunterStoragePolicy(root, 70L, 1_000L, setOf("jhlog", "hprof"), emptySet(), 1024, true)
            assertFalse(SessionStorageBudget.publishHeapDump(root, policy, pending, destination))
            assertTrue(pending.exists())
        } finally { root.deleteRecursively() }
    }

    @Test
    fun exportLeasePreventsReclamationUntilEveryReaderCloses() {
        val root = Files.createTempDirectory("jh-policy-export-lease").toFile()
        try {
            SessionArtifactReadLeases.acquire(root).use {
                SessionArtifactReadLeases.acquire(root).use {
                    assertFalse(SessionArtifactReadLeases.mutate(root, false) { true })
                }
                assertFalse(SessionArtifactReadLeases.mutate(root, false) { true })
            }
            assertTrue(SessionArtifactReadLeases.mutate(root, false) { true })
        } finally { root.deleteRecursively() }
    }

    @Test
    fun writerCannotSpendBytesReservedByOtherProcessesAndHeapCommit() {
        val root = Files.createTempDirectory("jh-policy-shared-quota").toFile()
        try {
            val policy = policy(root, total = 24_000)
            SessionStorageBudget.open(root, policy).use { first ->
                SessionStorageBudget.open(root, policy).use { second ->
                    first.claim(7_000, terminal = false)
                    try {
                        second.claim(1_000, terminal = false)
                        throw AssertionError("both terminal reservations must remain accounted")
                    } catch (expected: StorageBudgetExhaustedException) {
                        assertEquals(24_000L, expected.limitBytes)
                    }
                }
            }
        } finally { root.deleteRecursively() }
    }

    @Test
    fun smallerPolicyStopsAlreadyOpenWriterEvenWhenNewAdmissionFails() {
        val root = Files.createTempDirectory("jh-policy-smaller-quota").toFile()
        try {
            SessionStorageBudget.open(root, policy(root, total = 50_000)).use { first ->
                first.claim(10_000, terminal = false)
                try {
                    SessionStorageBudget.open(root, policy(root, total = 20_000)).close()
                    throw AssertionError("new writer cannot reserve terminal bytes")
                } catch (expected: StorageBudgetExhaustedException) {
                    assertEquals(20_000L, expected.limitBytes)
                }
                try {
                    first.claim(3_000, terminal = false)
                    throw AssertionError("old writer must observe the new host limit")
                } catch (expected: StorageBudgetExhaustedException) {
                    assertEquals(20_000L, expected.limitBytes)
                }
            }
        } finally { root.deleteRecursively() }
    }

    @Test
    fun diskFullRecoveryIsExplicitAndPreservesActiveLogs() {
        val root = Files.createTempDirectory("jh-policy-disk-full").toFile()
        try {
            val process = process(root)
            val log = process.resolve(SessionLogName.create("2026-09-25", RUN, 0L, 0L))
                .apply { writeBytes(ByteArray(100)) }
            val heap = process.resolve("retained-1790352000000-Owner-1.hprof").apply { writeBytes(ByteArray(80)) }
            val disabled = JankHunterStoragePolicy(root, 1000, 1000, setOf("jhlog", "hprof"), setOf("hprof"), 32, false)
            assertEquals(0L, SessionStorageBudget.recoverDiskFull(root, disabled, IOException("ENOSPC")))
            assertTrue(heap.exists())
            assertEquals(0L, SessionStorageBudget.recoverDiskFull(root, policy(root), IOException("EACCES")))
            assertEquals(80L, SessionStorageBudget.recoverDiskFull(root, policy(root), IOException("ENOSPC")))
            assertFalse(heap.exists())
            assertEquals(100L, log.length())
        } finally { root.deleteRecursively() }
    }

    @Test
    fun retentionDeletionReleasesBytesInTheAlreadyOpenQuota() {
        val root = Files.createTempDirectory("jh-policy-deleted-heap-accounting").toFile()
        try {
            val heap = process(root).resolve("retained-1790352000000-Owner-1.hprof").apply { writeBytes(ByteArray(2_000)) }
            SessionStorageBudget.open(root, policy(root, total = 30_000)).use { budget ->
                assertTrue(SessionStorageBudget.deleteHeapDump(root, heap))
                assertFalse(heap.exists())
                budget.claim(21_000, terminal = false)
            }
        } finally { root.deleteRecursively() }
    }

    private fun policy(root: File, perFile: Long = 100_000, total: Long = 1_000_000) =
        JankHunterStoragePolicy(root, perFile, total, setOf("jhlog", "hprof"), setOf("hprof"), 1024, true)

    private fun process(root: File): File =
        root.resolve(SessionArtifactPath.sessionDirectoryName(1_790_352_000_000, 0L, RUN))
            .resolve(SessionArtifactPath.processDirectoryName(PROCESS)).apply { mkdirs() }

    companion object {
        private val RUN = ByteArray(16) { (it + 1).toByte() }
        private val PROCESS = ByteArray(16) { (it + 33).toByte() }
    }
}
