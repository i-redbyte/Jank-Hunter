package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.internal.concurrent.BoundedMpscQueue

/** Bounded wrapper reuse for stable counters; batch ownership starts after queue admission. */
internal class PendingStableCountersEventPool(capacity: Int) {
    private val available = BoundedMpscQueue<PendingStableCountersEvent>(capacity)

    fun acquire(
        producerContext: LogEventContext?,
        batch: StableCounterBatch,
    ): PendingStableCountersEvent {
        val event = available.poll() ?: PendingStableCountersEvent(this)
        return event.initialize(producerContext, batch)
    }

    fun release(event: PendingStableCountersEvent, recycleBatch: Boolean) {
        val batch = event.clearForRecycle()
        if (recycleBatch) batch?.recycle()
        available.tryOffer(event)
    }
}
