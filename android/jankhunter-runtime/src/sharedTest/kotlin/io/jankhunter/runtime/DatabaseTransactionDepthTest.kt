package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.Jhlog
import java.util.concurrent.atomic.AtomicReferenceArray
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class DatabaseTransactionDepthTest {
    @Test
    fun everyActiveLevelRetainsItsExactIdParentAndStatementCounts() {
        for (depth in intArrayOf(17, 33, 65, 10_000)) {
            val fixture = Fixture()
            val database = Any()
            val ids = LongArray(depth)
            for (level in ids.indices) {
                ids[level] = fixture.begin(database)
                assertNotEquals("depth ${level + 1} lost its ID", 0L, ids[level])
                assertEquals(ids[level], fixture.tracker.currentTransactionId())
                assertEquals(if (level == 0) 777L else ids[level - 1], fixture.tracker.currentParentId())
                assertEquals(ids[level], fixture.tracker.recordStatement(Jhlog.DATABASE_OPERATION_QUERY))
                assertEquals(ids[level], fixture.tracker.recordStatement(Jhlog.DATABASE_OPERATION_EXECUTE))
            }
            for (level in ids.indices.reversed()) {
                assertTrue(fixture.tracker.markSuccessful(database))
                assertTrue(fixture.finish(database))
                val event = checkNotNull(fixture.completed[ids[level]])
                assertEquals(if (level == 0) 777L else ids[level - 1], event.parent)
                assertEquals(Jhlog.DATABASE_TRANSACTION_SUCCESS, event.outcome)
                assertEquals(2L, event.statements)
                assertEquals(1L, event.reads)
                assertEquals(1L, event.writes)
                assertEquals(if (level == 0) 0L else ids[level - 1], fixture.tracker.currentTransactionId())
            }
            assertEquals(depth, fixture.completed.size)
            assertFalse(fixture.finish(database))
        }
    }

    @Test
    fun rollbackAboveBitMaskWidthPoisonsOnlyItsDatabaseAfterEarlierRemoval() {
        for (failed in booleanArrayOf(false, true)) {
            val fixture = Fixture()
            val first = Any()
            val second = Any()
            val firstId = fixture.begin(first)
            val ids = LongArray(70) { fixture.begin(second) }
            val child = fixture.begin(second)
            fixture.tracker.markSuccessful(second)
            if (!failed) {
                // A new unmarked child forces rollback even after explicit successful marking.
                fixture.begin(second)
                fixture.finish(second)
            }
            assertTrue(fixture.tracker.finish(second, DatabaseFailureKind.OTHER, failed))
            fixture.tracker.markSuccessful(first)
            fixture.finish(first)
            assertEquals(Jhlog.DATABASE_TRANSACTION_SUCCESS, fixture.completed[firstId]?.outcome)
            assertEquals(if (failed) Jhlog.DATABASE_TRANSACTION_FAILURE else Jhlog.DATABASE_TRANSACTION_ROLLBACK,
                fixture.completed[child]?.outcome)
            for (id in ids.reversedArray()) {
                fixture.tracker.markSuccessful(second)
                fixture.finish(second)
                assertEquals(Jhlog.DATABASE_TRANSACTION_ROLLBACK, fixture.completed[id]?.outcome)
            }
            assertEquals(0L, fixture.tracker.currentTransactionId())
            val next = fixture.begin(first)
            fixture.tracker.markSuccessful(first)
            fixture.finish(first)
            assertEquals(Jhlog.DATABASE_TRANSACTION_SUCCESS, fixture.completed[next]?.outcome)
        }
    }

    @Test
    fun aFullyUnwoundGrownStateIsNotRetainedInTheThreadSlotOrPool() {
        val fixture = Fixture()
        val database = Any()
        repeat(1_025) { assertNotEquals(0L, fixture.begin(database)) }
        val local = field(fixture.tracker, "localSlot") as ThreadLocal<*>
        val holder = local.get() as Array<*>
        val slot = checkNotNull(holder[1])
        val state = checkNotNull(field(slot, "active"))
        repeat(1_025) { fixture.finish(database) }
        assertNull("active slot retains grown storage", holder[1])
        assertNull("cached slot retains grown storage", field(slot, "active"))
        val pool = checkNotNull(field(fixture.tracker, "statePool"))
        val slots = field(pool, "slots") as AtomicReferenceArray<*>
        repeat(slots.length()) { assertTrue("pool retains the oversized state", slots.get(it) !== state) }
    }

    private fun field(owner: Any, name: String): Any? = owner.javaClass.getDeclaredField(name)
        .apply { isAccessible = true }.get(owner)

    private class Fixture {
        val completed = linkedMapOf<Long, Completion>()
        val tracker = DatabaseTransactionTracker(
            completionSink = DatabaseTransactionCompletionSink { id, parent, _, _, _, outcome, _, _, statements, reads, writes ->
                completed[id] = Completion(parent, outcome, statements, reads, writes)
            },
            nanoTime = { 1L },
        )
        fun begin(database: Any): Long = tracker.begin(database, 1L, "test", Jhlog.DATABASE_TRANSACTION_DEFERRED, 777L)
        fun finish(database: Any) = tracker.finish(database, DatabaseFailureKind.OTHER, failed = false)
    }

    private data class Completion(val parent: Long, val outcome: Long, val statements: Long, val reads: Long, val writes: Long)
}
