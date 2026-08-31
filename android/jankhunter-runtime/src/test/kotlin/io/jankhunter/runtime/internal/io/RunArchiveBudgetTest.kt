package io.jankhunter.runtime.internal.io

import java.io.File
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReference
import kotlin.concurrent.thread
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class RunArchiveBudgetTest {
    @Test
    fun reclamationUsesPrimitivePort() {
        val field = RunArchiveBudget::class.java.getDeclaredField("reclaimBytesTo")

        assertFalse(field.type == Function1::class.java)
    }

    @Test
    fun activeSegmentsShareOneHardCrossProcessQuota() {
        val directory = Files.createTempDirectory("jankhunter-run-budget").toFile()
        try {
            val committed = AtomicLong()
            val first = budget(directory, 32L * 1024L, committed::get)
            val second = budget(directory, 32L * 1024L, committed::get)
            first.claim(15L * 1024L, terminal = false)

            try {
                second.claim(2L * 1024L, terminal = false)
                fail("combined committed bytes and terminal reserves must not exceed the run budget")
            } catch (error: StorageBudgetExhaustedException) {
                assertEquals(32L * 1024L, error.limitBytes)
                assertTrue(error.message.orEmpty().contains("storage_budget_exhausted"))
            }
            committed.addAndGet(15L * 1024L)

            first.claim(512L, terminal = true)
            committed.addAndGet(512L)
            second.claim(512L, terminal = true)
            committed.addAndGet(512L)
            first.close()
            second.close()
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun pressureSubtractsOnlyBytesActuallyReclaimedFromCompletedRuns() {
        val directory = Files.createTempDirectory("jankhunter-run-budget-reclaim").toFile()
        val reclaimed = AtomicLong(4L * 1024L)
        try {
            RunArchiveBudget.open(
                directory = directory,
                runId = RUN_ID,
                limitBytes = 32L * 1024L,
                actualArchiveBytes = { 4L * 1024L },
                reclaimBytesTo = { reclaimed.getAndSet(0L) },
            ).use { budget ->
                budget.claim(11L * 1024L, terminal = false)
                budget.claim(10L * 1024L, terminal = false)

                try {
                    budget.claim(4L * 1024L, terminal = false)
                    fail("in-flight and committed claims must remain authoritative after reclamation")
                } catch (_: StorageBudgetExhaustedException) {
                    // The completed-run bytes were reclaimed once; live claims still occupy the quota.
                }
            }
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun staleLedgerIsRebuiltFromRetainedRunBytes() {
        val directory = Files.createTempDirectory("jankhunter-run-budget-recovery").toFile()
        try {
            budget(directory, 64L * 1024L) { 0L }.use { budget ->
                budget.claim(20L * 1024L, terminal = false)
            }

            budget(directory, 64L * 1024L) { 1_024L }.use { recovered ->
                recovered.claim(48L * 1024L, terminal = false)
                recovered.claim(512L, terminal = true)
            }
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun nextRunRemovesCompletedQuotaStateAndPreservesUnrelatedFiles() {
        val directory = Files.createTempDirectory("jankhunter-run-budget-state-cleanup").toFile()
        try {
            budget(directory, RUN_ID, 64L * 1024L) { 0L }.close()
            val completedState = stateFile(directory, RUN_ID)
            val unrelated = File(directory, ".jh-archive-budget.manual.state").apply {
                writeBytes(byteArrayOf(1))
            }
            val uppercaseIdentity = File(directory, ".jh-archive-budget.$UPPERCASE_RUN_ID.state").apply {
                writeBytes(byteArrayOf(2))
            }
            val zeroIdentity = File(directory, ".jh-archive-budget.${"0".repeat(32)}.state").apply {
                writeBytes(byteArrayOf(3))
            }
            assertTrue(completedState.isFile)

            budget(directory, NEXT_RUN_ID, 64L * 1024L) { 0L }.use {
                assertFalse(completedState.exists())
                assertTrue(stateFile(directory, NEXT_RUN_ID).isFile)
                assertTrue(unrelated.isFile)
                assertTrue(uppercaseIdentity.isFile)
                assertTrue(zeroIdentity.isFile)
            }
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun nextRunPreservesQuotaStateWhileItsReservationLeaseIsActive() {
        val directory = Files.createTempDirectory("jankhunter-run-budget-active-state").toFile()
        val active = budget(directory, RUN_ID, 64L * 1024L) { 0L }
        try {
            budget(directory, NEXT_RUN_ID, 64L * 1024L) { 0L }.use {
                assertTrue(stateFile(directory, RUN_ID).isFile)
            }
        } finally {
            active.close()
            directory.deleteRecursively()
        }
    }

    @Test
    fun obsoleteCleanupCannotDeleteAStateBeforeItsReservationIsPublished() {
        val directory = Files.createTempDirectory("jankhunter-run-budget-open-race").toFile()
        val firstEntered = CountDownLatch(1)
        val allowFirstToPublish = CountDownLatch(1)
        val firstResult = AtomicReference<RunArchiveBudget?>()
        val secondResult = AtomicReference<RunArchiveBudget?>()
        val failure = AtomicReference<Throwable?>()
        val firstThread = thread(start = false, name = "archive-budget-first-open") {
            runCatching {
                budget(directory, RUN_ID, 64L * 1024L) {
                    firstEntered.countDown()
                    check(allowFirstToPublish.await(10L, TimeUnit.SECONDS))
                    0L
                }
            }.onSuccess(firstResult::set).onFailure { error -> failure.compareAndSet(null, error) }
        }
        val secondThread = thread(start = false, name = "archive-budget-second-open") {
            runCatching {
                budget(directory, NEXT_RUN_ID, 64L * 1024L) { 0L }
            }.onSuccess(secondResult::set).onFailure { error -> failure.compareAndSet(null, error) }
        }
        try {
            firstThread.start()
            assertTrue(firstEntered.await(10L, TimeUnit.SECONDS))
            secondThread.start()
            Thread.sleep(250L)

            assertTrue("a concurrent open deleted an unpublished active state", stateFile(directory, RUN_ID).isFile)

            allowFirstToPublish.countDown()
            firstThread.join(10_000L)
            secondThread.join(10_000L)
            failure.get()?.let { throw AssertionError("concurrent archive open failed", it) }
            assertFalse(firstThread.isAlive)
            assertFalse(secondThread.isAlive)
            assertTrue(stateFile(directory, RUN_ID).isFile)
            assertTrue(stateFile(directory, NEXT_RUN_ID).isFile)
        } finally {
            allowFirstToPublish.countDown()
            firstThread.join(1_000L)
            secondThread.join(1_000L)
            firstResult.get()?.close()
            secondResult.get()?.close()
            directory.deleteRecursively()
        }
    }

    @Test
    fun concurrentClaimsNeverCrossTheHardLimit() {
        val directory = Files.createTempDirectory("jankhunter-run-budget-concurrent").toFile()
        val workers = 8
        val limit = 256L * 1024L
        val budgets = ArrayList<RunArchiveBudget>()
        val committed = AtomicLong()
        try {
            repeat(workers) {
                budgets += budget(directory, limit, committed::get)
            }
            val start = CountDownLatch(1)
            val done = CountDownLatch(workers)
            val threads = budgets.map { budget ->
                thread {
                    try {
                        start.await()
                        repeat(1_000) {
                            budget.claim(1_024L, terminal = false)
                            committed.addAndGet(1_024L)
                        }
                    } catch (_: StorageBudgetExhaustedException) {
                        // The shared hard frontier is the expected stop condition.
                    } finally {
                        done.countDown()
                    }
                }
            }
            start.countDown()
            assertTrue(done.await(10L, TimeUnit.SECONDS))
            threads.forEach { worker -> worker.join(1_000L) }
            assertTrue(committed.get() + workers * RunArchiveBudget.TERMINAL_RESERVE_BYTES <= limit)
        } finally {
            budgets.forEach(RunArchiveBudget::close)
            directory.deleteRecursively()
        }
    }

    @Test
    fun reservationScanDoesNotReleaseTheCurrentProcessLease() {
        val directory = Files.createTempDirectory("jankhunter-run-budget-own-lease").toFile()
        val first = budget(directory, 64L * 1024L) { 0L }
        try {
            val firstLease = directory.listFiles { candidate -> candidate.name.endsWith(".lease") }
                .orEmpty()
                .single()
            val second = budget(directory, 64L * 1024L) { 0L }
            try {
                first.claim(48L * 1024L, terminal = false)

                assertFalse("reservation scan released its own POSIX lease", ExternalFileLockProbe.canAcquire(firstLease))
            } finally {
                second.close()
            }
            first.close()
            assertTrue(ExternalFileLockProbe.canAcquire(firstLease))
        } finally {
            first.close()
            directory.deleteRecursively()
        }
    }

    @Test
    fun failedDirectoryLockKeepsReservationLeaseActiveUntilCloseIsRetried() {
        val directory = Files.createTempDirectory("jankhunter-budget-close-failure").toFile()
        val active = budget(directory, 64L * 1024L) { 0L }
        val leaseFile = directory.listFiles { candidate -> candidate.name.endsWith(".lease") }
            .orEmpty()
            .single()
        val lockPath = File(directory, ARCHIVE_DIRECTORY_LOCK)
        try {
            assertTrue(lockPath.delete())
            assertTrue(lockPath.mkdir())

            val failure = runCatching { active.close() }.exceptionOrNull()

            assertTrue(failure is java.io.IOException)
            assertFalse("failed close released an unserialized reservation", ExternalFileLockProbe.canAcquire(leaseFile))

            assertTrue(lockPath.delete())
            active.close()
            assertTrue(ExternalFileLockProbe.canAcquire(leaseFile))
        } finally {
            if (lockPath.isDirectory) lockPath.delete()
            runCatching { active.close() }
            directory.deleteRecursively()
        }
    }

    private fun budget(directory: java.io.File, limit: Long, actualBytes: () -> Long): RunArchiveBudget {
        return budget(directory, RUN_ID, limit, actualBytes)
    }

    private fun budget(
        directory: File,
        runId: String,
        limit: Long,
        actualBytes: () -> Long,
    ): RunArchiveBudget {
        return RunArchiveBudget.open(directory, runId, limit, actualBytes) { 0L }
    }

    private fun stateFile(directory: File, runId: String): File =
        File(directory, ".jh-archive-budget.$runId.state")

    private companion object {
        const val ARCHIVE_DIRECTORY_LOCK = ".jh-archive-budget.lock"
        const val RUN_ID = "01000000000000000000000000000000"
        const val NEXT_RUN_ID = "02000000000000000000000000000000"
        const val UPPERCASE_RUN_ID = "A1000000000000000000000000000000"
    }
}
