package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.internal.concurrent.BoundedSegmentedQueue
import io.jankhunter.runtime.internal.concurrent.BoundedMpscQueue.OfferResult

internal enum class LogEventLane {
    CRITICAL,
    BULK,
}

/** Two bounded MPSC lanes merged by the global admission sequence without intermediate storage. */
internal class AsyncEventLanes(
    bulkCapacity: Int,
    criticalCapacity: Int = recommendedCriticalCapacity(bulkCapacity),
) {
    private val critical = BoundedSegmentedQueue<PendingLogEvent>(criticalCapacity)
    private val bulk = BoundedSegmentedQueue<PendingLogEvent>(bulkCapacity)
    private var nextSequence = 1L

    fun tryOffer(lane: LogEventLane, event: PendingLogEvent): OfferResult {
        return queue(lane).tryOffer(event)
    }

    fun hasEvents(): Boolean = !critical.isEmpty() || !bulk.isEmpty()

    fun hasCapacity(lane: LogEventLane): Boolean = queue(lane).hasCapacity()

    fun pollNext(): PendingLogEvent? {
        val criticalHead = critical.peek()
        val bulkHead = bulk.peek()
        val event = when {
            criticalHead?.sequence == nextSequence -> critical.poll()
            bulkHead?.sequence == nextSequence -> bulk.poll()
            // Head reads are not an atomic snapshot: an earlier critical publication can
            // appear between the two reads. Never advance the completion frontier past it.
            else -> null
        }
        if (event != null) nextSequence++
        return event
    }

    internal fun allocatedSlotCapacityForTest(): Int {
        return critical.retainedSlotCapacityForTest() + bulk.retainedSlotCapacityForTest()
    }

    private fun queue(lane: LogEventLane): BoundedSegmentedQueue<PendingLogEvent> {
        return if (lane == LogEventLane.CRITICAL) critical else bulk
    }

    companion object {
        private const val CRITICAL_QUEUE_CAPACITY_DIVISOR = 8
        private const val MIN_CRITICAL_QUEUE_CAPACITY = 16
        private const val MAX_CRITICAL_QUEUE_CAPACITY = 512

        private fun recommendedCriticalCapacity(bulkCapacity: Int): Int {
            val remainder = if (bulkCapacity % CRITICAL_QUEUE_CAPACITY_DIVISOR == 0) 0 else 1
            val proportional = bulkCapacity / CRITICAL_QUEUE_CAPACITY_DIVISOR + remainder
            return proportional.coerceIn(MIN_CRITICAL_QUEUE_CAPACITY, MAX_CRITICAL_QUEUE_CAPACITY)
        }
    }
}
