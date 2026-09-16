package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.Jhlog
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class DatabaseTransactionRollbackTest {
    @Test
    fun allSixteenTrackedLevelsPreserveSuccessOrPropagateTheInnermostRollback() {
        for (innerSuccess in listOf(false, true)) {
            val fixture = Fixture()
            val database = Any()
            val ids = List(16) { fixture.begin(database) }
            if (innerSuccess) fixture.tracker.markSuccessful(database)
            fixture.finish(database)
            repeat(15) {
                fixture.tracker.markSuccessful(database)
                fixture.finish(database)
            }
            val expected = if (innerSuccess) Jhlog.DATABASE_TRANSACTION_SUCCESS else Jhlog.DATABASE_TRANSACTION_ROLLBACK
            ids.forEach { assertEquals(expected, fixture.outcomes[it]) }
            assertEquals(0L, fixture.tracker.currentTransactionId())
        }
    }

    @Test
    fun nestedRollbackCannotBecomeSuccessWhenAncestorsAreMarkedSuccessful() {
        val fixture = Fixture()
        val database = Any()
        val outer = fixture.begin(database)
        val middle = fixture.begin(database)
        val inner = fixture.begin(database)
        fixture.finish(database)
        assertTrue(fixture.tracker.markSuccessful(database))
        fixture.finish(database)
        assertTrue(fixture.tracker.markSuccessful(database))
        fixture.finish(database)
        for (id in listOf(inner, middle, outer)) {
            assertEquals("rollback lost at transaction $id", Jhlog.DATABASE_TRANSACTION_ROLLBACK, fixture.outcomes[id])
        }
    }

    @Test
    fun failedChildOnlyPoisonsAncestorsOfTheSameDatabase() {
        val fixture = Fixture()
        val first = Any()
        val second = Any()
        val outer = fixture.begin(first)
        val unrelated = fixture.begin(second)
        val inner = fixture.begin(first)
        fixture.tracker.markSuccessful(first)
        fixture.tracker.finish(first, DatabaseFailureKind.OTHER, failed = true)
        fixture.tracker.markSuccessful(second)
        fixture.finish(second)
        fixture.tracker.markSuccessful(first)
        fixture.finish(first)
        assertEquals(Jhlog.DATABASE_TRANSACTION_FAILURE, fixture.outcomes[inner])
        assertEquals(Jhlog.DATABASE_TRANSACTION_SUCCESS, fixture.outcomes[unrelated])
        assertEquals(Jhlog.DATABASE_TRANSACTION_ROLLBACK, fixture.outcomes[outer])
    }

    @Test
    fun rollbackStateSurvivesRemovalOfAnUnrelatedEarlierTransaction() {
        val fixture = Fixture()
        val first = Any()
        val second = Any()
        val unrelated = fixture.begin(first)
        val outer = fixture.begin(second)
        fixture.begin(second)
        fixture.finish(second)
        fixture.tracker.markSuccessful(first)
        fixture.finish(first)
        fixture.tracker.markSuccessful(second)
        fixture.finish(second)
        assertEquals(Jhlog.DATABASE_TRANSACTION_SUCCESS, fixture.outcomes[unrelated])
        assertEquals(Jhlog.DATABASE_TRANSACTION_ROLLBACK, fixture.outcomes[outer])
        val next = fixture.begin(second)
        fixture.tracker.markSuccessful(second)
        fixture.finish(second)
        assertEquals("poisoned pooled state leaked into next transaction", Jhlog.DATABASE_TRANSACTION_SUCCESS, fixture.outcomes[next])
    }

    private class Fixture {
        val outcomes = linkedMapOf<Long, Long>()
        val tracker = DatabaseTransactionTracker(
            completionSink = DatabaseTransactionCompletionSink { id, _, _, _, _, outcome, _, _, _, _, _ -> outcomes[id] = outcome },
            nanoTime = { 1L },
        )
        fun begin(database: Any): Long = tracker.begin(database, 1L, "test", Jhlog.DATABASE_TRANSACTION_DEFERRED)
        fun finish(database: Any) = tracker.finish(database, DatabaseFailureKind.OTHER, failed = false)
    }
}
