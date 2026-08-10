package io.jankhunter.runtime.internal.concurrent

import java.util.concurrent.atomic.AtomicLongArray

/**
 * Bounded single-producer/single-consumer slot ownership protocol.
 *
 * Callers store slot payload between [tryClaimProducer] and [publish], and read it between
 * [tryClaimConsumer] and [release]. The sequence array is the release/acquire publication fence;
 * payload fields must not be read or overwritten outside the claimed interval.
 */
internal class SpscSlotSequencer(
    val capacity: Int,
) {
    private val mask: Int
    private val sequences: AtomicLongArray
    private var producerPosition = 0L
    private var consumerPosition = 0L

    init {
        require(capacity >= MIN_CAPACITY && capacity and (capacity - 1) == 0) {
            "SPSC capacity must be a power of two >= $MIN_CAPACITY"
        }
        mask = capacity - 1
        sequences = AtomicLongArray(capacity)
        repeat(capacity) { index -> sequences.set(index, index.toLong()) }
    }

    fun tryClaimProducer(): Long {
        val position = producerPosition
        return if (sequences.get(index(position)) == position) position else NO_POSITION
    }

    fun publish(position: Long) {
        check(position == producerPosition) { "Producer position is not owned" }
        sequences.lazySet(index(position), position + 1L)
        producerPosition = position + 1L
    }

    fun tryClaimConsumer(): Long {
        val position = consumerPosition
        return if (sequences.get(index(position)) == position + 1L) position else NO_POSITION
    }

    fun release(position: Long) {
        check(position == consumerPosition) { "Consumer position is not owned" }
        sequences.lazySet(index(position), position + capacity.toLong())
        consumerPosition = position + 1L
    }

    fun slotIndex(position: Long): Int = index(position)

    fun isEmpty(): Boolean = tryClaimConsumer() == NO_POSITION

    fun pendingCount(): Long {
        val start = consumerPosition
        var count = 0
        while (count < capacity) {
            val position = start + count.toLong()
            if (sequences.get(index(position)) != position + 1L) break
            count++
        }
        return count.toLong()
    }

    private fun index(position: Long): Int = position.toInt() and mask

    internal companion object {
        const val NO_POSITION = -1L
        private const val MIN_CAPACITY = 2
    }
}
