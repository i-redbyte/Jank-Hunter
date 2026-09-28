package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterStoragePolicy
import java.io.File
import java.nio.file.Files
import java.util.zip.ZipEntry
import java.util.zip.ZipFile
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SessionArchiveCoordinatorTest {
    @Test
    fun archivesCompletedMultiprocessSessionWithoutHistoricalHeapOrManifest() {
        val root = tempDir()
        try {
            val session = createSession(root, RUN_ID, 0L, listOf(PROCESS_A, PROCESS_B), withHeapDump = true)
            val current = createSession(root, CURRENT_RUN_ID, 1L, listOf(PROCESS_A))

            val result = SessionArchiveCoordinator.maintain(root, SessionLogName.runIdHex(CURRENT_RUN_ID))

            assertEquals(1L, result.archived)
            assertFalse(session.exists())
            assertTrue(current.exists())
            val archive = File(root, "${session.name}.jhlog.zip")
            ZipFile(archive).use { zip ->
                val entries = zip.entries().asSequence().toList()
                assertEquals(2, entries.size)
                assertTrue(entries.all { entry -> entry.method == ZipEntry.STORED })
                assertTrue(entries.none { entry -> entry.name.endsWith(".hprof") })
                assertTrue(entries.none { entry -> entry.name.contains("manifest", ignoreCase = true) })
            }
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun activeCohortLeasePreventsArchiving() {
        val root = tempDir()
        try {
            ProcessRunCohort.join(root, DATE, startedAtUnixMs = START_MS).use { lease ->
                val session = createSession(root, lease.runId(), lease.dailySessionIndex(), listOf(PROCESS_A))

                val result = SessionArchiveCoordinator.maintain(root, SessionLogName.runIdHex(CURRENT_RUN_ID))

                assertEquals(0L, result.archived)
                assertEquals(1L, result.active)
                assertTrue(session.exists())
            }
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun mismatchedJhlogHeaderLeavesSessionIncomplete() {
        val root = tempDir()
        try {
            val session = createSession(root, RUN_ID, 0L, listOf(PROCESS_A), headerRunId = CURRENT_RUN_ID)

            val result = SessionArchiveCoordinator.maintain(root, SessionLogName.runIdHex(CURRENT_RUN_ID))

            assertEquals(1L, result.incomplete)
            assertTrue(session.exists())
            assertFalse(File(root, "${session.name}.jhlog.zip").exists())
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun recoveryPublishesVerifiedTemporaryArchiveAfterPrePublishFailure() {
        val root = tempDir()
        try {
            val session = createSession(root, RUN_ID, 0L, listOf(PROCESS_A), withHeapDump = true)
            val failed = runCatching {
                SessionArchiveCoordinator.maintain(
                    root,
                    SessionLogName.runIdHex(CURRENT_RUN_ID),
                    faultInjector = SessionArchiveCoordinator.FaultInjector { phase ->
                        if (phase == SessionArchiveCoordinator.FaultPhase.BEFORE_PUBLISH) error("crash")
                    },
                )
            }

            assertTrue(failed.isFailure)
            assertTrue(session.exists())
            assertTrue(root.listFiles().orEmpty().any { file -> file.name.endsWith(".jhlog.zip.tmp") })

            val recovered = SessionArchiveCoordinator.maintain(root, SessionLogName.runIdHex(CURRENT_RUN_ID))

            assertEquals(1L, recovered.recovered)
            assertFalse(session.exists())
            assertTrue(File(root, "${session.name}.jhlog.zip").isFile)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun recoveryDeletesSourceOnlyAfterPublishedArchiveWasVerified() {
        val root = tempDir()
        try {
            val session = createSession(root, RUN_ID, 0L, listOf(PROCESS_A))
            val failed = runCatching {
                SessionArchiveCoordinator.maintain(
                    root,
                    SessionLogName.runIdHex(CURRENT_RUN_ID),
                    faultInjector = SessionArchiveCoordinator.FaultInjector { phase ->
                        if (phase == SessionArchiveCoordinator.FaultPhase.AFTER_PUBLISH) error("crash")
                    },
                )
            }

            assertTrue(failed.isFailure)
            assertTrue(session.exists())
            assertTrue(File(root, "${session.name}.jhlog.zip").isFile)

            val recovered = SessionArchiveCoordinator.maintain(root, SessionLogName.runIdHex(CURRENT_RUN_ID))

            assertEquals(1L, recovered.recovered)
            assertFalse(session.exists())
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun recoveryFinishesPartiallyDeletedSourceFromVerifiedPublishedArchive() {
        val root = tempDir()
        try {
            val session = createSession(root, RUN_ID, 0L, listOf(PROCESS_A), withHeapDump = true)
            var deletedArtifacts = 0
            val failed = runCatching {
                SessionArchiveCoordinator.maintain(
                    root,
                    SessionLogName.runIdHex(CURRENT_RUN_ID),
                    faultInjector = SessionArchiveCoordinator.FaultInjector { phase ->
                        if (phase == SessionArchiveCoordinator.FaultPhase.AFTER_SOURCE_ARTIFACT_DELETE) {
                            deletedArtifacts++
                            if (deletedArtifacts == 1) error("crash")
                        }
                    },
                )
            }

            assertTrue(failed.isFailure)
            assertTrue(session.exists())
            assertTrue(File(root, "${session.name}.jhlog.zip").isFile)
            assertEquals(1, session.walkTopDown().count { file -> file.isFile })

            val recovered = SessionArchiveCoordinator.maintain(root, SessionLogName.runIdHex(CURRENT_RUN_ID))

            assertEquals(1L, recovered.recovered)
            assertFalse(session.exists())
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun unfinishedManagedHeapDoesNotStrandACompletedSession() {
        val root = tempDir()
        try {
            val session = createSession(root, RUN_ID, 0L, listOf(PROCESS_A))
            val process = File(session, SessionArtifactPath.processDirectoryName(PROCESS_A))
            val pending = File.createTempFile(".jh-heap-", ".pending", process).apply { writeText("unfinished") }
            val result = SessionArchiveCoordinator.maintain(root, SessionLogName.runIdHex(CURRENT_RUN_ID))
            assertEquals(1L, result.archived)
            assertFalse(pending.exists())
            assertFalse(session.exists())
            ZipFile(File(root, "${session.name}.jhlog.zip")).use { zip ->
                assertEquals(1, zip.size())
                assertTrue(zip.entries().nextElement().name.endsWith(".jhlog"))
            }
        } finally { root.deleteRecursively() }
    }

    @Test
    fun unknownPendingFileRemainsProtected() {
        val root = tempDir()
        try {
            val session = createSession(root, RUN_ID, 0L, listOf(PROCESS_A))
            val process = File(session, SessionArtifactPath.processDirectoryName(PROCESS_A))
            val unknown = File(process, ".jh-heap-user-data.pending").apply { writeText("keep") }
            val result = SessionArchiveCoordinator.maintain(root, SessionLogName.runIdHex(CURRENT_RUN_ID))
            assertEquals(1L, result.incomplete)
            assertTrue(unknown.exists())
            assertTrue(session.exists())
        } finally { root.deleteRecursively() }
    }

    @Test
    fun hostQuotaNeverPublishesArchiveAboveCommittedPhysicalLimit() {
        val root = tempDir()
        try {
            val session = createSession(root, RUN_ID, 0L, listOf(PROCESS_A))
            val limit = SessionStorageBudget.physicalBytes(root)
            val result = SessionArchiveCoordinator.maintain(root, SessionLogName.runIdHex(CURRENT_RUN_ID),
                policy = storagePolicy(root, limit))
            assertEquals(0L, result.archived)
            assertTrue(session.exists())
            assertFalse(File(root, "${session.name}.jhlog.zip").exists())
            assertTrue(SessionStorageBudget.physicalBytes(root) <= limit)
        } finally { root.deleteRecursively() }
    }

    @Test
    fun archiveReplacementUpdatesLiveQuotaWithoutLosingUnflushedClaims() {
        val root = tempDir()
        try {
            createSession(root, RUN_ID, 0L, listOf(PROCESS_A), withHeapDump = true)
            val policy = storagePolicy(root, 100_000)
            SessionStorageBudget.open(root, policy).use { budget ->
                budget.claim(5_000, terminal = false)
                val result = SessionArchiveCoordinator.maintain(root, SessionLogName.runIdHex(CURRENT_RUN_ID), policy = policy)
                assertEquals(1L, result.archived)
                val available = 100_000 - 5_000 - RunArchiveBudget.TERMINAL_RESERVE_BYTES - SessionStorageBudget.physicalBytes(root)
                SessionArtifactReadLeases.acquire(root).use {
                    budget.claim(available, terminal = false)
                    try {
                        budget.claim(1, terminal = false)
                        throw AssertionError("ZIP overhead and released source bytes must update the live ledger exactly")
                    } catch (expected: StorageBudgetExhaustedException) {
                        assertEquals(100_000L, expected.limitBytes)
                    }
                }
            }
        } finally { root.deleteRecursively() }
    }

    @Test
    fun pressureReclaimsCompletedDirectoryWhenArchiveCannotFitAndPreservesActiveCohort() {
        val root = tempDir()
        try {
            val old = createSession(root, RUN_ID, 0L, listOf(PROCESS_A))
            ProcessRunCohort.join(root, DATE, startedAtUnixMs = START_MS + 1).use { lease ->
                val active = createSession(root, lease.runId(), lease.dailySessionIndex(), listOf(PROCESS_B))
                val policy = storagePolicy(root, SessionStorageBudget.physicalBytes(root) + RunArchiveBudget.TERMINAL_RESERVE_BYTES)
                SessionStorageBudget.open(root, policy).use { budget -> budget.claim(1, terminal = false) }
                assertFalse(old.exists())
                assertTrue(active.exists())
            }
        } finally { root.deleteRecursively() }
    }

    private fun storagePolicy(root: File, total: Long) =
        JankHunterStoragePolicy(root, Long.MAX_VALUE, total, setOf("jhlog", "hprof"), setOf("hprof"), 1024, true)

    private fun createSession(
        root: File,
        runId: ByteArray,
        dailyIndex: Long,
        processes: List<ByteArray>,
        withHeapDump: Boolean = false,
        headerRunId: ByteArray = runId,
    ): File {
        val session = File(root, SessionArtifactPath.sessionDirectoryName(START_MS + dailyIndex, dailyIndex, runId))
        processes.forEachIndexed { index, processId ->
            val process = File(session, SessionArtifactPath.processDirectoryName(processId)).apply { mkdirs() }
            val segmentIndex = 0L
            val name = SessionLogName.create(DATE, runId, dailyIndex, segmentIndex)
            BinaryLogWriter(
                File(process, name),
                maxDictionaryEntries = 100,
                maxDictionaryValueBytes = 1_024,
                fileHeader = BinaryLogFileHeader(
                    runId = headerRunId,
                    processInstanceId = processId,
                    sessionId = ByteArray(16) { (index + 3).toByte() },
                    segmentIndex = segmentIndex,
                    osPid = index.toLong() + 1L,
                    collectorStartElapsedUs = 1L,
                    segmentStartElapsedUs = 1L,
                    segmentStartUnixMs = START_MS,
                    identitySource = 0L,
                    processName = "process-$index",
                    symbolNamespace = ByteArray(0),
                ),
                quality = LogQualityCounters(),
            ).close()
            if (withHeapDump && index == 0) {
                File(process, "retained-$START_MS-LeakedActivity-1.hprof").writeText("hprof")
            }
        }
        return session
    }

    private fun tempDir(): File = Files.createTempDirectory("jankhunter-session-archive").toFile()

    private companion object {
        const val DATE = "2027-01-02"
        const val START_MS = 1_799_000_000_000L
        val RUN_ID = ByteArray(16) { index -> (index + 1).toByte() }
        val CURRENT_RUN_ID = ByteArray(16) { index -> (index + 33).toByte() }
        val PROCESS_A = ByteArray(16) { index -> (index + 65).toByte() }
        val PROCESS_B = ByteArray(16) { index -> (index + 97).toByte() }
    }
}
