package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.Jhlog
import java.lang.ref.WeakReference
import java.util.concurrent.CancellationException
import java.util.concurrent.TimeoutException
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeDatabaseTelemetryTest {
    @Test
    fun failureTaxonomyUsesTypesAndNeverThrowableMessages() {
        assertEquals(DatabaseFailureKind.CANCELLED, databaseFailureKind(CancellationException("private-token")))
        assertEquals(DatabaseFailureKind.TIMEOUT, databaseFailureKind(TimeoutException("private-token")))
        assertEquals(DatabaseFailureKind.OTHER, databaseFailureKind(IllegalStateException("private-token")))
        assertEquals(
            DatabaseFailureKind.TIMEOUT,
            databaseFailureKind(IllegalStateException("private-token", TimeoutException("nested-secret"))),
        )
    }

    @Test
    fun preparedStatementRegistryUsesWeakIdentityAndStableTokens() {
        val registry = PreparedStatementRegistry(capacity = 4)
        val first = Any()
        val second = Any()

        val firstToken = registry.register(first, "SELECT value FROM item WHERE id = ?", 11L)
        assertTrue(firstToken > 0L)
        assertEquals(firstToken, registry.register(first, "SELECT value FROM item WHERE id = ?", 11L))
        assertEquals("SELECT value FROM item WHERE id = ?", registry.resolve(first)?.query)
        assertEquals(11L, registry.resolve(first)?.fingerprint)
        assertEquals(firstToken, registry.resolve(first)?.token)
        assertNull(registry.resolve(second))
        assertEquals(1, registry.retainedEntryCount())
    }

    @Test
    fun defaultPreparedStatementRegistryUsesExpandedSingleTable() {
        assertEquals(4_096, PreparedStatementRegistry().capacityForTest())
    }

    @Test
    fun preparedStatementRegistryReportsCapacityEvictionAndAffectedResolutionMiss() {
        val evictions = AtomicInteger()
        val misses = AtomicInteger()
        val registry = PreparedStatementRegistry(
            capacity = 2,
            onEviction = evictions::incrementAndGet,
            onResolutionMissAfterEviction = misses::incrementAndGet,
        )
        val statements = arrayOf(Any(), Any(), Any())

        statements.forEachIndexed { index, statement ->
            registry.register(statement, "SELECT $index", index.toLong())
        }
        val evicted = statements.first { registry.resolve(it) == null }
        registry.resolve(evicted)

        assertEquals(1, evictions.get())
        assertEquals(1L, registry.evictionCount())
        assertEquals(2, misses.get())
        assertEquals(2, registry.retainedEntryCount())
    }

    @Test
    fun transactionTrackerPreservesNestingOutcomeAndStatementCountsWithoutStrongDatabaseReference() {
        var nowNanos = 100L
        val completions = ArrayList<CompletedDatabaseTransaction>()
        val tracker = DatabaseTransactionTracker(
            completionSink = DatabaseTransactionCompletionSink { transactionId, parentId, _, _, _, outcome,
                _, durationNanos, statementCount, readCount, writeCount ->
                completions += CompletedDatabaseTransaction(
                    transactionId,
                    parentId,
                    outcome,
                    durationNanos,
                    statementCount,
                    readCount,
                    writeCount,
                )
            },
        ) { nowNanos }
        val database = Any()

        val outer = tracker.begin(database, 10L, "outer", Jhlog.DATABASE_TRANSACTION_IMMEDIATE)
        assertTrue(outer > 0L)
        assertEquals(0L, tracker.currentParentId())
        assertEquals(outer, tracker.recordStatement(Jhlog.DATABASE_OPERATION_QUERY))

        val inner = tracker.begin(database, 20L, "inner", Jhlog.DATABASE_TRANSACTION_EXCLUSIVE)
        assertEquals(outer, tracker.currentParentId())
        assertEquals(inner, tracker.recordStatement(Jhlog.DATABASE_OPERATION_UPDATE))
        assertTrue(tracker.markSuccessful(database))
        nowNanos = 200L
        assertTrue(tracker.finish(database, DatabaseFailureKind.OTHER, failed = false))
        val completedInner = completions.last()
        assertEquals(inner, completedInner.transactionId)
        assertEquals(outer, completedInner.parentId)
        assertEquals(Jhlog.DATABASE_TRANSACTION_SUCCESS, completedInner.outcome)
        assertEquals(1L, completedInner.statementCount)
        assertEquals(0L, completedInner.readCount)
        assertEquals(1L, completedInner.writeCount)
        assertEquals(100L, completedInner.durationNanos)

        nowNanos = 250L
        assertTrue(tracker.finish(database, DatabaseFailureKind.OTHER, failed = false))
        val completedOuter = completions.last()
        assertEquals(Jhlog.DATABASE_TRANSACTION_ROLLBACK, completedOuter.outcome)
        assertEquals(1L, completedOuter.readCount)
        assertEquals(0L, tracker.currentTransactionId())
        assertFalse(tracker.markSuccessful(database))
        assertFalse(tracker.finish(database, DatabaseFailureKind.OTHER, failed = false))
    }

    @Test
    fun idleApplicationThreadDoesNotStronglyRetainTransactionSlot() {
        val tracker = DatabaseTransactionTracker(nanoTime = RuntimeLongSource { 1L })
        val database = Any()
        tracker.begin(database, 1L, "source", Jhlog.DATABASE_TRANSACTION_IMMEDIATE)
        assertTrue(tracker.finish(database, DatabaseFailureKind.OTHER, failed = false))

        val field = DatabaseTransactionTracker::class.java.getDeclaredField("localSlot")
        field.isAccessible = true
        val holder = ((field.get(tracker) as ThreadLocal<*>).get() as Array<*>)

        assertTrue(holder[0] is WeakReference<*>)
        assertNull(holder[1])
    }

    private data class CompletedDatabaseTransaction(
        val transactionId: Long,
        val parentId: Long,
        val outcome: Long,
        val durationNanos: Long,
        val statementCount: Long,
        val readCount: Long,
        val writeCount: Long,
    )

    @Test
    fun manualTransactionTokenCompletesAcrossThreadsWithoutRetainingFinishedState() {
        val clock = AtomicLong(100L)
        val tracker = ManualDatabaseTransactionTracker(DatabaseTransactionIdGenerator(), clock::get)
        val token = tracker.begin(
            sourceId = 10L,
            sourceName = "custom.orm",
            mode = Jhlog.DATABASE_TRANSACTION_DEFERRED,
            automaticParentId = 0L,
        )
        assertEquals(token.id, tracker.recordStatement(Jhlog.DATABASE_OPERATION_QUERY))

        clock.set(200L)
        val completed = AtomicBoolean()
        Thread {
            completed.set(token.completeOnce())
            tracker.release(token)
        }.apply {
            start()
            join()
        }

        assertTrue(completed.get())
        assertEquals(1L, token.statementCount)
        assertEquals(1L, token.readCount)
        assertEquals(0L, token.writeCount)
        assertFalse(token.completeOnce())

        val next = tracker.begin(
            sourceId = 20L,
            sourceName = "custom.orm.next",
            mode = Jhlog.DATABASE_TRANSACTION_IMMEDIATE,
            automaticParentId = 0L,
        )
        assertEquals(0L, next.parentId)
        assertTrue(next.id > token.id)
    }

    @Test
    fun naturalDatabaseResultsAreBucketedWithoutInspectingCursor() {
        assertEquals(Jhlog.DATABASE_COUNT_ZERO.toInt(), databaseResultCountBucket(-1L, DATABASE_CAPTURE_INSERT_ROW_ID))
        assertEquals(Jhlog.DATABASE_COUNT_ONE.toInt(), databaseResultCountBucket(42L, DATABASE_CAPTURE_INSERT_ROW_ID))
        assertEquals(Jhlog.DATABASE_COUNT_ZERO.toInt(), databaseResultCountBucket(0L, DATABASE_CAPTURE_AFFECTED_ROWS))
        assertEquals(Jhlog.DATABASE_COUNT_TWO_TO_TEN.toInt(), databaseResultCountBucket(10L, DATABASE_CAPTURE_AFFECTED_ROWS))
        assertEquals(Jhlog.DATABASE_COUNT_OVER_HUNDRED.toInt(), databaseResultCountBucket(101L, DATABASE_CAPTURE_AFFECTED_ROWS))
    }

    @Test
    fun disabledManualDatabaseApiRunsDelegateWithoutCreatingTokens() {
        assertNull(
            JankHunterDatabaseTracing.beginCall(
                "custom.orm",
                "SELECT secret FROM account WHERE id = 42",
                JankHunterDatabaseOperation.QUERY,
            ),
        )
        assertEquals(
            7,
            JankHunterDatabaseTracing.traceCall(
                "custom.orm",
                "SELECT secret FROM account WHERE id = 42",
                JankHunterDatabaseOperation.QUERY,
            ) { 7 },
        )
    }

    @Test
    fun manualPhaseEvidenceIsBoundedAllocationFreeTokenState() {
        val token = JankHunterDatabaseCallToken(
            sourceId = 1L,
            sourceName = "custom.orm",
            query = "SELECT value FROM message",
            fingerprint = 2L,
            operation = Jhlog.DATABASE_OPERATION_QUERY.toInt(),
            boundary = Jhlog.DATABASE_BOUNDARY_MANUAL.toInt(),
            startedNanos = 10L,
        )

        assertTrue(token.recordPhase(JankHunterDatabasePhase.EXECUTE, 4_000L))
        assertTrue(token.recordPhase(JankHunterDatabasePhase.MATERIALIZE, 6_000L))
        assertEquals(
            Jhlog.DATABASE_PHASE_EXECUTE or Jhlog.DATABASE_PHASE_MATERIALIZE,
            token.validatedPhaseMask(totalDurationUs = 10L),
        )
        assertEquals(4L, token.phaseDurationUs(JankHunterDatabasePhase.EXECUTE))
        assertEquals(6L, token.phaseDurationUs(JankHunterDatabasePhase.MATERIALIZE))
        assertEquals(0L, token.validatedPhaseMask(totalDurationUs = 9L))

        assertTrue(token.completeOnce())
        assertFalse(token.recordPhase(JankHunterDatabasePhase.LOCK_WAIT, 1_000L))
    }

    @Test
    fun disabledManualPhaseBoundaryDoesNotCreateTimingEvidence() {
        assertEquals(0L, JankHunterDatabaseTracing.beginPhase(null))
        JankHunterDatabaseTracing.endPhase(null, JankHunterDatabasePhase.MATERIALIZE, 1L)
    }
}
