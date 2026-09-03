package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.internal.concurrent.BoundedMpscQueue

/** Bounded wrapper reuse for runtime batches; batch ownership starts only after queue admission. */
internal class PendingRuntimeCallsEventPool(capacity: Int) {
    private val available = BoundedMpscQueue<PendingRuntimeCallsEvent>(capacity)

    fun acquire(
        producerContext: LogEventContext?,
        batch: RuntimeCallBatch,
    ): PendingRuntimeCallsEvent {
        val event = available.poll() ?: PendingRuntimeCallsEvent(this)
        return event.initialize(producerContext, batch)
    }

    fun release(event: PendingRuntimeCallsEvent, recycleBatch: Boolean) {
        val batch = event.clearForRecycle()
        if (recycleBatch) batch?.recycle()
        available.tryOffer(event)
    }
}
