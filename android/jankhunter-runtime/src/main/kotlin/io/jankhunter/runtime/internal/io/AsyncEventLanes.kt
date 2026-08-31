package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.internal.concurrent.BoundedMpscQueue
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
    private val critical = BoundedMpscQueue<PendingLogEvent>(criticalCapacity)
    private val bulk = BoundedMpscQueue<PendingLogEvent>(bulkCapacity)

    fun tryOffer(lane: LogEventLane, event: PendingLogEvent): OfferResult {
        return queue(lane).tryOffer(event)
    }

    fun hasEvents(): Boolean = !critical.isEmpty() || !bulk.isEmpty()

    fun hasCapacity(lane: LogEventLane): Boolean = queue(lane).hasCapacity()

    fun pollNext(): PendingLogEvent? {
        val criticalHead = critical.peek()
        val bulkHead = bulk.peek()
        return when {
            criticalHead == null -> bulk.poll()
            bulkHead == null -> critical.poll()
            criticalHead.sequence < bulkHead.sequence -> critical.poll()
            else -> bulk.poll()
        }
    }

    private fun queue(lane: LogEventLane): BoundedMpscQueue<PendingLogEvent> {
        return if (lane == LogEventLane.CRITICAL) critical else bulk
    }

    companion object {
        private const val CRITICAL_QUEUE_CAPACITY_DIVISOR = 8
        private const val MIN_CRITICAL_QUEUE_CAPACITY = 16
        private const val MAX_CRITICAL_QUEUE_CAPACITY = 256

        private fun recommendedCriticalCapacity(bulkCapacity: Int): Int {
            val remainder = if (bulkCapacity % CRITICAL_QUEUE_CAPACITY_DIVISOR == 0) 0 else 1
            val proportional = bulkCapacity / CRITICAL_QUEUE_CAPACITY_DIVISOR + remainder
            return proportional.coerceIn(MIN_CRITICAL_QUEUE_CAPACITY, MAX_CRITICAL_QUEUE_CAPACITY)
        }
    }
}
