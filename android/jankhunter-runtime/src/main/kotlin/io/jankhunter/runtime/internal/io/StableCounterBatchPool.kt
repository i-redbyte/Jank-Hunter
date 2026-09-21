package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.internal.concurrent.BoundedMpscQueue

/** Bounded array-batch reuse between the runtime hook consumer and the writer worker. */
internal class StableCounterBatchPool(
    capacity: Int,
    private val batchCapacity: Int,
) {
    private val available = BoundedMpscQueue<StableCounterBatch>(capacity)

    init {
        require(batchCapacity > 0) { "Stable counter batch capacity must be positive" }
    }

    fun acquire(): StableCounterBatch {
        val batch = available.poll() ?: return StableCounterBatch(batchCapacity, this)
        batch.prepareForReuse()
        return batch
    }

    internal fun release(batch: StableCounterBatch) {
        available.tryOffer(batch)
    }
}
