package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.internal.concurrent.BoundedMpscQueue.OfferResult
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class AsyncEventLanesTest {
    @Test
    fun pollNextMergesReservedAndBulkLanesByGlobalSequence() {
        val lanes = AsyncEventLanes(bulkCapacity = 2, criticalCapacity = 2)
        val bulk = event(sequence = 2L)
        val critical = event(sequence = 1L)

        assertEquals(OfferResult.OFFERED, lanes.tryOffer(LogEventLane.BULK, bulk))
        assertEquals(OfferResult.OFFERED, lanes.tryOffer(LogEventLane.CRITICAL, critical))
        assertEquals(critical, lanes.pollNext())
        assertEquals(bulk, lanes.pollNext())
        assertNull(lanes.pollNext())
    }

    private fun event(sequence: Long): PendingLogEvent {
        return PendingLogEvent.Counter(null, "lane", sequence).also { event ->
            event.sequence = sequence
        }
    }
}
