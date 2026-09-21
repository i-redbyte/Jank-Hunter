package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.internal.concurrent.BoundedMpscQueue

/** Lazily populated bounded pool shared by the graph consumer and the binary writer thread. */
internal class RuntimeCallBatchPool(
    capacity: Int,
    private val batchCapacity: Int,
) {
    private val available = BoundedMpscQueue<RuntimeCallBatch>(capacity)

    init {
        require(batchCapacity > 0) { "Runtime call batch capacity must be positive" }
    }

    fun acquire(): RuntimeCallBatch {
        val batch = available.poll() ?: return RuntimeCallBatch(batchCapacity, this)
        batch.prepareForReuse()
        return batch
    }

    internal fun release(batch: RuntimeCallBatch) {
        available.tryOffer(batch)
    }
}
