package io.jankhunter.runtime.internal.concurrent

import io.jankhunter.runtime.internal.concurrent.BoundedMpscQueue.OfferResult
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReference
import java.util.concurrent.atomic.AtomicReferenceArray

/**
 * Bounded queue for one consumer and externally serialized producers. Storage follows current
 * occupancy in fixed segments instead of retaining one slot pair for the configured maximum.
 */
internal class BoundedSegmentedQueue<T : Any>(
    val capacity: Int,
) {
    private val segmentCapacity = minOf(capacity, MAX_SEGMENT_CAPACITY)
    private val producerPosition = AtomicLong()
    private val consumerPosition = AtomicLong()
    private val recycled = AtomicReference<Segment<T>?>()
    private var producerSegment = Segment<T>(0L, segmentCapacity)
    private var consumerSegment = producerSegment

    init {
        require(capacity > 0) { "Segmented queue capacity must be positive" }
    }

    fun tryOffer(element: T): OfferResult {
        val producer = producerPosition.get()
        if (producer - consumerPosition.get() >= capacity.toLong()) return OfferResult.FULL
        val segment = producerSegment(producer)
        val offset = (producer - segment.startPosition).toInt()
        if (segment.elements.get(offset) != null) return OfferResult.CONTENDED
        segment.elements.set(offset, element)
        producerPosition.lazySet(producer + 1L)
        return OfferResult.OFFERED
    }

    fun peek(): T? {
        val consumer = consumerPosition.get()
        if (consumer >= producerPosition.get()) return null
        val segment = consumerSegment(consumer)
        return segment.elements.get((consumer - segment.startPosition).toInt())
    }

    fun poll(): T? {
        val consumer = consumerPosition.get()
        if (consumer >= producerPosition.get()) return null
        val segment = consumerSegment(consumer)
        val offset = (consumer - segment.startPosition).toInt()
        val element = segment.elements.getAndSet(offset, null) ?: return null
        consumerPosition.lazySet(consumer + 1L)
        return element
    }

    fun isEmpty(): Boolean = consumerPosition.get() >= producerPosition.get()

    fun hasCapacity(): Boolean {
        return producerPosition.get() - consumerPosition.get() < capacity.toLong()
    }

    internal fun retainedSlotCapacityForTest(): Int {
        var slots = 0
        var segment: Segment<T>? = consumerSegment
        while (segment != null) {
            slots += segment.elements.length()
            segment = segment.next
        }
        return slots + (recycled.get()?.elements?.length() ?: 0)
    }

    private fun producerSegment(position: Long): Segment<T> {
        val current = producerSegment
        if (position < current.endPosition) return current
        val next = recycled.getAndSet(null)?.also { it.reset(position) }
            ?: Segment(position, segmentCapacity)
        current.next = next
        producerSegment = next
        return next
    }

    private fun consumerSegment(position: Long): Segment<T> {
        val current = consumerSegment
        if (position < current.endPosition) return current
        val next = checkNotNull(current.next)
        consumerSegment = next
        current.next = null
        recycled.compareAndSet(null, current)
        return next
    }

    private class Segment<T : Any>(
        startPosition: Long,
        capacity: Int,
    ) {
        val elements = AtomicReferenceArray<T>(capacity)

        @Volatile
        var next: Segment<T>? = null

        var startPosition = startPosition
            private set

        val endPosition: Long
            get() = startPosition + elements.length()

        fun reset(position: Long) {
            startPosition = position
            next = null
        }
    }

    private companion object {
        const val MAX_SEGMENT_CAPACITY = 256
    }
}
