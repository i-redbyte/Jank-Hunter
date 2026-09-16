package io.jankhunter.runtime.internal.concurrent

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class BoundedSegmentedQueueTest {
    @Test
    fun retainedStorageTracksOccupancyInsteadOfLifetimeTraffic() {
        val queue = BoundedSegmentedQueue<Int>(65_536)

        repeat(100_000) { value ->
            assertEquals(BoundedMpscQueue.OfferResult.OFFERED, queue.tryOffer(value))
            assertEquals(value, queue.poll())
        }

        assertTrue(queue.retainedSlotCapacityForTest() <= 512)
    }

    @Test
    fun fullQueueIsBoundedAndReusesReleasedCapacity() {
        val queue = BoundedSegmentedQueue<Int>(3)
        repeat(3) { value ->
            assertEquals(BoundedMpscQueue.OfferResult.OFFERED, queue.tryOffer(value))
        }
        assertEquals(BoundedMpscQueue.OfferResult.FULL, queue.tryOffer(3))
        assertEquals(0, queue.poll())
        assertEquals(BoundedMpscQueue.OfferResult.OFFERED, queue.tryOffer(3))
        assertEquals(listOf(1, 2, 3), listOf(queue.poll(), queue.poll(), queue.poll()))
    }
}
