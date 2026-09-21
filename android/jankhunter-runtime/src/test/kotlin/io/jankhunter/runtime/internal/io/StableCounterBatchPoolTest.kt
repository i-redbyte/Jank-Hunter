package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Test

class StableCounterBatchPoolTest {
    @Test
    fun recyclingClearsReferencesAndReusesTheBatch() {
        val pool = StableCounterBatchPool(capacity = 1, batchCapacity = 2)
        val first = pool.acquire()
        first.add(1L, "first", 2L)

        first.recycle()

        val reused = pool.acquire()
        assertSame(first, reused)
        assertEquals(0, reused.size)
        reused.add(3L, "second", 4L)
        assertEquals("second", reused.name(0))
    }

    @Test
    fun rejectedEnvelopeReturnsOnlyTheWrapperWhileAdmittedEnvelopeReturnsItsBatch() {
        val batchPool = StableCounterBatchPool(capacity = 1, batchCapacity = 2)
        val eventPool = PendingStableCountersEventPool(capacity = 1)
        val batch = batchPool.acquire().apply { add(1L, "method", 2L) }

        eventPool.acquire(null, batch).rejectBeforeAdmission()
        assertEquals(1, batch.size)

        eventPool.acquire(null, batch).recycle()
        assertSame(batch, batchPool.acquire())
    }
}
