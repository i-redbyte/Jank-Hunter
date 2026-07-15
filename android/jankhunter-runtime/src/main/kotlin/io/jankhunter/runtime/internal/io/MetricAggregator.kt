package io.jankhunter.runtime.internal.io

import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.locks.ReentrantReadWriteLock
import kotlin.math.min

/**
 * Bounded metric accumulator.
 *
 * Existing keys are updated without a global exclusive monitor. A shared lifecycle read lock keeps
 * flush lossless, while the admission lock is only taken for a new key or an LRU eviction. Flush
 * swaps the complete active batch under the lifecycle write lock and emits it after producers have
 * resumed on the next batch.
 */
internal class MetricAggregator(
    maxKeys: Int,
) {
    private val capacity = maxKeys.coerceAtLeast(0)
    private val lruEvictionEnabled = capacity in 1..BitLruCache.MAX_CAPACITY
    private val initialMapCapacity = min(capacity, DEFAULT_INITIAL_MAP_CAPACITY)
    private val lifecycleLocks = Array(producerStripeCount(capacity)) { ReentrantReadWriteLock() }
    private val producerLocks = Array(lifecycleLocks.size) { lifecycleLocks[it].readLock() }
    private val flushLocks = Array(lifecycleLocks.size) { lifecycleLocks[it].writeLock() }
    private val producerStripeMask = lifecycleLocks.size - 1

    @Volatile
    private var active = Batch(initialMapCapacity)

    fun counter(name: String?, value: Long) {
        if (value == 0L) return

        val producerLock = producerLock()
        producerLock.lock()
        try {
            val batch = active
            if (value < 0L) {
                saturatedAddAndGet(batch.invalidNegative, 1L)
                return
            }

            val key = MetricKey(MetricKind.COUNTER, metricName(name))
            while (true) {
                val existing = batch.metrics[key] as? CounterValue
                if (existing != null && existing.add(value)) return

                synchronized(batch.admissionLock) {
                    val raced = batch.metrics[key] as? CounterValue
                    if (raced != null) {
                        if (raced.add(value)) return
                    } else {
                        if (!admitLocked(batch)) return
                        val created = CounterValue(lruEvictionEnabled)
                        if (!created.add(value)) {
                            saturatedAddAndGet(batch.dropped, 1L)
                            return
                        }
                        batch.metrics[key] = created
                        return
                    }
                }
            }
        } finally {
            producerLock.unlock()
        }
    }

    fun gauge(name: String?, value: Long, mode: MetricAggregationMode = MetricAggregationMode.AVERAGE) {
        val producerLock = producerLock()
        producerLock.lock()
        try {
            val batch = active
            if (value < 0L) {
                saturatedAddAndGet(batch.invalidNegative, 1L)
                return
            }

            val key = MetricKey(MetricKind.GAUGE, metricName(name))
            while (true) {
                val existing = batch.metrics[key] as? GaugeValue
                if (existing != null && existing.add(value, mode)) return

                synchronized(batch.admissionLock) {
                    val raced = batch.metrics[key] as? GaugeValue
                    if (raced != null) {
                        if (raced.add(value, mode)) return
                    } else {
                        if (!admitLocked(batch)) return
                        val created = GaugeValue(lruEvictionEnabled)
                        if (!created.add(value, mode)) {
                            saturatedAddAndGet(batch.dropped, 1L)
                            return
                        }
                        batch.metrics[key] = created
                        return
                    }
                }
            }
        } finally {
            producerLock.unlock()
        }
    }

    fun flush(sink: Sink) {
        val drained = swapActiveBatch()

        for ((key, value) in drained.metrics) {
            when (value) {
                is CounterValue -> sink.counter(key.name, value.total())
                is GaugeValue -> emitGauge(sink, key.name, value.snapshot())
            }
        }
        drained.dropped.get().takeIf { it > 0L }?.let {
            sink.counter(DROPPED_METRIC_NAME, it)
        }
        drained.invalidNegative.get().takeIf { it > 0L }?.let {
            sink.counter(INVALID_METRIC_NAME, it)
        }
    }

    private fun swapActiveBatch(): Batch {
        flushLocks.forEach { it.lock() }
        return try {
            val drained = active
            active = Batch(initialMapCapacity)
            drained
        } finally {
            for (index in flushLocks.indices.reversed()) {
                flushLocks[index].unlock()
            }
        }
    }

    private fun producerLock(): ReentrantReadWriteLock.ReadLock {
        val identity = System.identityHashCode(Thread.currentThread())
        val mixed = identity xor (identity ushr PRODUCER_HASH_SHIFT)
        return producerLocks[mixed and producerStripeMask]
    }

    private fun emitGauge(sink: Sink, name: String, gauge: GaugeSnapshot) {
        when (gauge.mode) {
            MetricAggregationMode.LAST,
            MetricAggregationMode.STATE -> {
                sink.gauge(name, gauge.last, gauge.count, gauge.last, gauge.last, gauge.mode)
            }
            MetricAggregationMode.BOOLEAN_RATE -> {
                val truePct = ((gauge.total.toDouble() * 100.0) / gauge.count.toDouble()).toLong()
                sink.gauge(name, truePct, gauge.count, gauge.total, gauge.max, gauge.mode)
            }
            MetricAggregationMode.UNKNOWN,
            MetricAggregationMode.AVERAGE -> {
                sink.gauge(name, gauge.total / gauge.count, gauge.count, gauge.total, gauge.max, gauge.mode)
            }
        }
    }

    /** Called with [Batch.admissionLock] held. */
    private fun admitLocked(batch: Batch): Boolean {
        if (capacity <= 0) {
            saturatedAddAndGet(batch.dropped, 1L)
            return false
        }
        if (batch.metrics.size < capacity) return true

        // Small bounded sets retain the previous LRU behavior. For large sets, scanning thousands
        // of keys would cost more than dropping a new high-cardinality metric.
        if (capacity > BitLruCache.MAX_CAPACITY) {
            saturatedAddAndGet(batch.dropped, 1L)
            return false
        }

        val candidates = batch.metrics.entries
            .map { EvictionCandidate(it.key, it.value, it.value.lastAccess()) }
            .sortedBy(EvictionCandidate::lastAccess)
        for (candidate in candidates) {
            val lostSamples = candidate.value.tryRetire() ?: continue
            if (batch.metrics.remove(candidate.key, candidate.value)) {
                saturatedAddAndGet(batch.dropped, lostSamples.coerceAtLeast(1L))
                return true
            }
        }
        // Every candidate is being updated. Dropping one new high-cardinality sample is safer than
        // blocking an arbitrary application thread or the maintenance flush behind that writer.
        saturatedAddAndGet(batch.dropped, 1L)
        return false
    }

    interface Sink {
        fun counter(name: String, value: Long)
        fun gauge(name: String, value: Long, count: Long, sum: Long, max: Long, mode: MetricAggregationMode)
    }

    private class Batch(initialMapCapacity: Int) {
        val metrics = ConcurrentHashMap<MetricKey, MetricValue>(initialMapCapacity.coerceAtLeast(1))
        val dropped = AtomicLong()
        val invalidNegative = AtomicLong()
        val admissionLock = Any()
    }

    private sealed class MetricValue(
        retirementEnabled: Boolean,
    ) {
        private val lifecycle = if (retirementEnabled) AtomicInteger() else null
        private val lastAccess = if (retirementEnabled) AtomicLong() else null

        protected inline fun update(block: () -> Unit): Boolean {
            val lifecycle = lifecycle
            if (lifecycle == null) {
                block()
                return true
            }
            while (true) {
                val writers = lifecycle.get()
                if (writers == RETIRED) return false
                if (lifecycle.compareAndSet(writers, writers + 1)) break
            }
            return try {
                block()
                lastAccess?.lazySet(System.nanoTime())
                true
            } finally {
                lifecycle.decrementAndGet()
            }
        }

        fun lastAccess(): Long = lastAccess?.get() ?: Long.MAX_VALUE

        fun tryRetire(): Long? {
            val lifecycle = lifecycle ?: return null
            return if (lifecycle.compareAndSet(0, RETIRED)) sampleCount() else null
        }

        protected abstract fun sampleCount(): Long

        private companion object {
            const val RETIRED = -1
        }
    }

    private class CounterValue(retirementEnabled: Boolean) : MetricValue(retirementEnabled) {
        private val total = AtomicLong()
        private val samples = if (retirementEnabled) AtomicLong() else null

        fun add(value: Long): Boolean = update {
            saturatedAddAndGet(total, value)
            samples?.let { saturatedAddAndGet(it, 1L) }
        }

        fun total(): Long = total.get()

        override fun sampleCount(): Long = samples?.get() ?: 0L
    }

    private class GaugeValue(retirementEnabled: Boolean) : MetricValue(retirementEnabled) {
        private val count = AtomicLong()
        private val total = AtomicLong()
        private val max = AtomicLong()
        private val lastLock = Any()
        private var last = 0L
        private var mode = MetricAggregationMode.AVERAGE

        fun add(value: Long, aggregationMode: MetricAggregationMode): Boolean = update {
            saturatedAddAndGet(count, 1L)
            saturatedAddAndGet(total, value)
            updateMax(max, value)
            // Keep LAST/STATE value and its aggregation mode from the same sample. This monitor is
            // per metric; unrelated metrics and all counters remain concurrent.
            synchronized(lastLock) {
                last = value
                mode = aggregationMode
            }
        }

        fun snapshot(): GaugeSnapshot {
            return GaugeSnapshot(
                count = count.get(),
                total = total.get(),
                max = max.get(),
                last = last,
                mode = mode,
            )
        }

        override fun sampleCount(): Long = count.get()
    }

    private data class GaugeSnapshot(
        val count: Long,
        val total: Long,
        val max: Long,
        val last: Long,
        val mode: MetricAggregationMode,
    )

    private data class EvictionCandidate(
        val key: MetricKey,
        val value: MetricValue,
        val lastAccess: Long,
    )

    private data class MetricKey(
        val kind: MetricKind,
        val name: String,
    )

    private enum class MetricKind {
        COUNTER,
        GAUGE,
    }

    companion object {
        const val DROPPED_METRIC_NAME = "jankhunter.metric_aggregation.dropped.count"
        const val INVALID_METRIC_NAME = "jankhunter.metric.invalid_negative.count"
        private const val DEFAULT_INITIAL_MAP_CAPACITY = 16
        private const val MAX_PRODUCER_STRIPES = 8
        private const val PRODUCER_HASH_SHIFT = 16

        private fun producerStripeCount(capacity: Int): Int {
            val target = min(capacity.coerceAtLeast(1), MAX_PRODUCER_STRIPES)
            var stripes = 1
            while (stripes < target) stripes = stripes shl 1
            return stripes
        }

        private fun metricName(name: String?): String {
            return name?.trim()?.takeIf { it.isNotEmpty() } ?: "unknown"
        }

        private fun saturatedAddAndGet(target: AtomicLong, delta: Long): Long {
            while (true) {
                val current = target.get()
                val updated = if (Long.MAX_VALUE - current < delta) Long.MAX_VALUE else current + delta
                if (target.compareAndSet(current, updated)) return updated
            }
        }

        private fun updateMax(target: AtomicLong, value: Long) {
            var current = target.get()
            while (value > current && !target.compareAndSet(current, value)) {
                current = target.get()
            }
        }
    }
}
