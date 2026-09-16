package io.jankhunter.runtime

import java.util.concurrent.atomic.AtomicLong
import org.junit.Assert.*
import org.junit.Test

class RuntimeGraphStackBudgetTest {
    @Test
    fun omittedNestedEntriesNeverInventACallerAndRestoreTheExactCapturedPrefix() {
        val budget = RuntimeGraphStorageBudget(16_384L)
        assertTrue(budget.tryReserve(RuntimeGraphStorageBudget.INITIAL_PRODUCER_BYTES))
        val tokens = AtomicLong()
        val stack = RuntimeCallStack(budget) { tokens.decrementAndGet() }
        repeat(128) { assertTrue(stack.push(it.toLong(), "method", 0L, "screen", 7L)) }
        assertFalse(stack.push(128L, "omitted", 0L, null, 0L))
        val token = stack.skippedToken
        assertTrue(token < 0L)
        assertFalse(stack.hasCurrentMethod())
        assertEquals(0L, stack.currentMethodId())
        assertNull(stack.currentMethodName())
        assertFalse(stack.push(129L, "also omitted", 0L, null, 0L))
        assertTrue(stack.popSkipped(token))
        assertFalse(stack.hasCurrentMethod())
        assertTrue(stack.popSkipped(token))
        assertEquals(127L, stack.currentMethodId())
        assertFalse(stack.popSkipped(token))
        repeat(128) { assertTrue(stack.pop((127 - it).toLong())) }
        assertEquals(RuntimeGraphStorageBudget.METADATA_BYTES, budget.usedBytes())
        assertTrue(stack.push(1L, "recovered", 0L, null, 0L))
        assertTrue(stack.pop(1L))
        stack.releaseStorage()
        stack.releaseStorage()
        budget.release(RuntimeGraphStorageBudget.METADATA_BYTES)
        budget.close()
        assertEquals(0L, budget.usedBytes())
        assertTrue(budget.peakBytes() <= budget.limitBytes)
    }

    @Test
    fun oldSkippedExitCannotConsumeANewIntervalAfterOutOfOrderReset() {
        val budget = RuntimeGraphStorageBudget(16_384L)
        assertTrue(budget.tryReserve(RuntimeGraphStorageBudget.INITIAL_PRODUCER_BYTES))
        val tokens = AtomicLong()
        val stack = RuntimeCallStack(budget) { tokens.decrementAndGet() }
        fun fill(): Long {
            repeat(128) { assertTrue(stack.push(it.toLong(), "method", 0L, null, 0L)) }
            assertFalse(stack.push(128L, "omitted", 0L, null, 0L))
            return stack.skippedToken
        }
        val oldToken = fill()
        assertFalse(stack.pop(0L))
        val newToken = fill()
        assertNotEquals(oldToken, newToken)
        assertFalse(stack.popSkipped(oldToken))
        assertFalse(stack.hasCurrentMethod())
        assertTrue(stack.popSkipped(newToken))
        assertEquals(127L, stack.currentMethodId())
        stack.releaseStorage()
        budget.release(RuntimeGraphStorageBudget.METADATA_BYTES)
        budget.close()
        assertEquals(0L, budget.usedBytes())
    }

    @Test
    fun exhaustedGapIdentityNeverMakesAnUnverifiedParentVisible() {
        val budget = RuntimeGraphStorageBudget(16_384L)
        assertTrue(budget.tryReserve(RuntimeGraphStorageBudget.INITIAL_PRODUCER_BYTES))
        val stack = RuntimeCallStack(budget) { 0L }
        repeat(128) { assertTrue(stack.push(it.toLong(), "method", 0L, null, 0L)) }
        assertFalse(stack.push(128L, "omitted", 0L, null, 0L))
        assertFalse(stack.popSkipped(0L))
        assertFalse(stack.hasCurrentMethod())
        stack.releaseStorage()
        budget.release(RuntimeGraphStorageBudget.METADATA_BYTES)
        budget.close()
        assertEquals(0L, budget.usedBytes())
    }
}
