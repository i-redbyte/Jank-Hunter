package io.jankhunter.runtime.internal.concurrent

import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicLongArray
import java.util.concurrent.atomic.AtomicReferenceArray

internal class BoundedMpscQueue<T : Any>(
    val capacity: Int,
) {
    private val mask: Int
    private val sequences: AtomicLongArray
    private val elements: AtomicReferenceArray<T>
    private val producerPosition = AtomicLong()
    private val consumerPosition = AtomicLong()

    init {
        require(capacity >= MIN_CAPACITY) { "MPSC capacity must be >= $MIN_CAPACITY" }
        mask = if (capacity and (capacity - 1) == 0) capacity - 1 else NO_MASK
        sequences = AtomicLongArray(capacity)
        elements = AtomicReferenceArray(capacity)
        repeat(capacity) { index -> sequences.set(index, index.toLong()) }
    }

    fun tryOffer(element: T): OfferResult {
        repeat(MAX_CAS_ATTEMPTS) {
            val producer = producerPosition.get()
            val consumer = consumerPosition.get()
            if (producer - consumer >= capacity.toLong()) return OfferResult.FULL
            if (producerPosition.compareAndSet(producer, producer + 1L)) {
                publish(producer, element)
                return OfferResult.OFFERED
            }
        }
        return OfferResult.CONTENDED
    }

    private fun publish(position: Long, element: T) {
        val index = index(position)
        elements.set(index, element)
        sequences.lazySet(index, position + 1L)
    }

    fun peek(): T? {
        val position = consumerPosition.get()
        val index = index(position)
        if (sequences.get(index) != position + 1L) return null
        return elements.get(index)
    }

    fun poll(): T? {
        val position = consumerPosition.get()
        val index = index(position)
        if (sequences.get(index) != position + 1L) return null
        val element = elements.getAndSet(index, null) ?: return null
        sequences.lazySet(index, position + capacity.toLong())
        consumerPosition.lazySet(position + 1L)
        return element
    }

    fun isEmpty(): Boolean = peek() == null

    fun hasCapacity(): Boolean {
        return producerPosition.get() - consumerPosition.get() < capacity.toLong()
    }

    private fun index(position: Long): Int {
        return if (mask == NO_MASK) Math.floorMod(position, capacity) else position.toInt() and mask
    }

    companion object {
        private const val NO_MASK = -1
        private const val MIN_CAPACITY = 1
        private const val MAX_CAS_ATTEMPTS = 8
    }

    enum class OfferResult {
        OFFERED,
        FULL,
        CONTENDED,
    }
}
