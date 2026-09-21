package io.jankhunter.runtime

import java.util.concurrent.CountDownLatch
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeAsyncTokenTableTest {
    @Test
    fun exactTransactionsAreIndependentOfAsyncSlotCapacity() {
        val table = RuntimeAsyncTokenTable(RuntimeAsyncTokenIds(), capacity = 1)
        val http = table.begin(RuntimeAsyncTokenTable.HTTP, 7L)
        val transactions = LongArray(70_000) { table.beginTransaction() }
        transactions.forEachIndexed { index, id ->
            assertTrue(id > 0L)
            if (index > 0) assertTrue(id > transactions[index - 1])
        }
        for (id in transactions.reversedArray()) {
            assertTrue(table.claimTransaction(id))
            table.releaseTransaction()
        }
        assertEquals(0L, table.begin(RuntimeAsyncTokenTable.HTTP, 8L))
        assertEquals(7L, table.claim(http, RuntimeAsyncTokenTable.HTTP))
        table.release(http)
        assertTrue(table.begin(RuntimeAsyncTokenTable.HTTP, 9L) > 0L)
        table.close { kind, active, completing ->
            if (kind == RuntimeAsyncTokenTable.DATABASE_TRANSACTION) {
                assertEquals(0L, active)
                assertEquals(0L, completing)
            }
        }
    }

    @Test
    fun slotReusePreservesNanosecondsAndRejectsOldAndDuplicateCompletions() {
        val table = RuntimeAsyncTokenTable(RuntimeAsyncTokenIds(), capacity = 1)
        val first = table.begin(RuntimeAsyncTokenTable.DATABASE, Long.MAX_VALUE)
        assertEquals(Long.MAX_VALUE, table.claim(first, RuntimeAsyncTokenTable.DATABASE))
        assertEquals(-1L, table.claim(first, RuntimeAsyncTokenTable.DATABASE))
        table.release(first)
        val second = table.begin(RuntimeAsyncTokenTable.DATABASE, 0L)
        assertNotEquals(first, second)
        assertEquals(-1L, table.claim(first, RuntimeAsyncTokenTable.DATABASE))
        assertEquals(0L, table.claim(second, RuntimeAsyncTokenTable.DATABASE))
        table.release(first)
        assertEquals(0L, table.begin(RuntimeAsyncTokenTable.WORKER, 1L))
        table.release(second)
        assertTrue(table.begin(RuntimeAsyncTokenTable.WORKER, 1L) > 0L)
    }

    @Test
    fun epochReplacementCannotResolveAnOldTokenEvenAtTheSameSlot() {
        val ids = RuntimeAsyncTokenIds()
        val old = RuntimeAsyncTokenTable(ids)
        val token = old.begin(RuntimeAsyncTokenTable.WORKER, 123L)
        old.close { _, _, _ -> }
        val replacement = RuntimeAsyncTokenTable(ids)
        val next = replacement.begin(RuntimeAsyncTokenTable.WORKER, 456L)
        assertEquals(-1L, replacement.claim(token, RuntimeAsyncTokenTable.WORKER))
        assertEquals(-1L, old.claim(token, RuntimeAsyncTokenTable.WORKER))
        assertEquals(456L, replacement.claim(next, RuntimeAsyncTokenTable.WORKER))
    }

    @Test
    fun closeReportsUnfinishedAndAlreadyCompletingSeparatelyExactlyOnce() {
        val table = RuntimeAsyncTokenTable(RuntimeAsyncTokenIds())
        table.begin(RuntimeAsyncTokenTable.HTTP, 11L)
        val completing = table.begin(RuntimeAsyncTokenTable.DATABASE, 22L)
        assertEquals(22L, table.claim(completing, RuntimeAsyncTokenTable.DATABASE))
        val calls = AtomicInteger()
        val unfinished = LongArray(RuntimeAsyncTokenTable.KIND_COUNT)
        val publishing = LongArray(RuntimeAsyncTokenTable.KIND_COUNT)
        table.close { kind, active, claimed ->
            calls.incrementAndGet()
            unfinished[kind] = active
            publishing[kind] = claimed
        }
        table.release(completing)
        table.close { _, _, _ -> calls.incrementAndGet() }
        assertEquals(RuntimeAsyncTokenTable.KIND_COUNT - 1, calls.get())
        assertEquals(1L, unfinished[RuntimeAsyncTokenTable.HTTP])
        assertEquals(0L, unfinished[RuntimeAsyncTokenTable.DATABASE])
        assertEquals(1L, publishing[RuntimeAsyncTokenTable.DATABASE])
        assertEquals(0L, table.begin(RuntimeAsyncTokenTable.HTTP, 33L))
    }

    @Test
    fun fullCapacityDoesNotEvictWorkAndPageBoundariesRemainExact() {
        val reasons = mutableListOf<Int>()
        val table = RuntimeAsyncTokenTable(RuntimeAsyncTokenIds(), capacity = 130, rejected = reasons::add)
        val tokens = LongArray(130) { table.begin(RuntimeAsyncTokenTable.HTTP, it.toLong()) }
        assertEquals(0L, table.begin(RuntimeAsyncTokenTable.HTTP, 999L))
        assertEquals(listOf(RuntimeAsyncTokenTable.REJECT_CAPACITY), reasons)
        for (index in tokens.indices.reversed()) {
            assertEquals(index.toLong(), table.claim(tokens[index], RuntimeAsyncTokenTable.HTTP))
            table.release(tokens[index])
        }
        repeat(130) { assertTrue(table.begin(RuntimeAsyncTokenTable.HTTP, it.toLong()) > 0L) }
    }

    @Test
    fun tokenExhaustionNeverWrapsOrReusesAnIdentity() {
        val ids = RuntimeAsyncTokenIds(RuntimeAsyncTokenIds.MAX_SEQUENCE - 1L)
        val reasons = mutableListOf<Int>()
        val table = RuntimeAsyncTokenTable(ids, rejected = reasons::add)
        val token = table.begin(RuntimeAsyncTokenTable.WORKER, 37L)
        assertTrue(token > 0L)
        assertEquals(37L, table.claim(token, RuntimeAsyncTokenTable.WORKER))
        table.release(token)
        assertEquals(0L, table.begin(RuntimeAsyncTokenTable.WORKER, 38L))
        assertEquals(listOf(RuntimeAsyncTokenTable.REJECT_ID_EXHAUSTED), reasons)
    }

    @Test
    fun concurrentDuplicateCallbacksHaveOnlyOneWinner() {
        val table = RuntimeAsyncTokenTable(RuntimeAsyncTokenIds())
        val token = table.begin(RuntimeAsyncTokenTable.WORKER, 91L)
        val ready = CountDownLatch(8)
        val start = CountDownLatch(1)
        val winners = AtomicInteger()
        val workers = List(8) {
            Thread {
                ready.countDown()
                start.await()
                if (table.claim(token, RuntimeAsyncTokenTable.WORKER) == 91L) winners.incrementAndGet()
            }.apply { start() }
        }
        assertTrue(ready.await(5, java.util.concurrent.TimeUnit.SECONDS))
        start.countDown()
        workers.forEach { it.join(5_000L); assertTrue(!it.isAlive) }
        assertEquals(1, winners.get())
    }
}
