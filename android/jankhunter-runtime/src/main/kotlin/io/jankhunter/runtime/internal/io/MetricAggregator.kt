package io.jankhunter.runtime.internal.io

import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.locks.ReentrantReadWriteLock
import kotlin.math.min

/**
 * Concurrent metric accumulator. [maxKeys] is a hard memory-safety boundary in every admission
 * mode. EXACT admission preserves already accepted keys and rejects excess cardinality with an
 * explicit loss counter; BEST_EFFORT may evict a cold key to keep newer evidence.
 *
 * Existing keys are updated without a global exclusive monitor. A shared lifecycle read lock keeps
 * flush lossless, while the admission lock is only taken for a new key or an LRU eviction. Flush
 * swaps the complete active batch under the lifecycle write lock and emits it after producers have
 * resumed on the next batch.
 */
internal class MetricAggregator(
    maxKeys: Int,
    private val exactAdmission: Boolean = false,
) {
    private val capacity = maxKeys.coerceIn(0, MAX_KEYS_HARD_LIMIT)
    private val lruEvictionEnabled = !exactAdmission && capacity in 1..LRU_EVICTION_MAX_KEYS
    private val initialMapCapacity = min(capacity, DEFAULT_INITIAL_MAP_CAPACITY)
    private val lifecycleLocks = Array(producerStripeCount(capacity)) { ReentrantReadWriteLock() }
    private val producerLocks = Array(lifecycleLocks.size) { lifecycleLocks[it].readLock() }
    private val flushLocks = Array(lifecycleLocks.size) { lifecycleLocks[it].writeLock() }
    private val producerStripeMask = lifecycleLocks.size - 1

    @Volatile
    private var active = Batch(initialMapCapacity, if (lruEvictionEnabled) capacity else 0)

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

            val normalizedName = metricName(name)
            if (normalizedName == null) {
                saturatedAddAndGet(batch.dropped, 1L)
                return
            }
            while (true) {
                val existing = batch.counters[normalizedName]
                if (existing != null && existing.add(value)) return

                synchronized(batch.admissionLock) {
                    val raced = batch.counters[normalizedName]
                    if (raced != null) {
                        if (raced.add(value)) return
                    } else {
                        if (!admitLocked(batch)) return
                        val created = CounterValue(lruEvictionEnabled)
                        if (!created.add(value)) {
                            saturatedAddAndGet(batch.dropped, 1L)
                            return
                        }
                        batch.counters[normalizedName] = created
                        return
                    }
                }
            }
        } finally {
            producerLock.unlock()
        }
    }

    fun gauge(name: String?, value: Long, mode: MetricAggregationMode = MetricAggregationMode.AVERAGE) {
        gaugeInternal(name, value, mode)
    }

    fun gaugeClassified(name: String?, value: Long) {
        gaugeInternal(name, value, null)
    }

    private fun gaugeInternal(name: String?, value: Long, requestedMode: MetricAggregationMode?) {
        val producerLock = producerLock()
        producerLock.lock()
        try {
            val batch = active
            if (value < 0L) {
                saturatedAddAndGet(batch.invalidNegative, 1L)
                return
            }

            val normalizedName = metricName(name)
            if (normalizedName == null) {
                saturatedAddAndGet(batch.dropped, 1L)
                return
            }
            while (true) {
                val existing = batch.gauges[normalizedName]
                if (existing != null && existing.add(value, requestedMode)) return

                synchronized(batch.admissionLock) {
                    val raced = batch.gauges[normalizedName]
                    if (raced != null) {
                        if (raced.add(value, requestedMode)) return
                    } else {
                        if (!admitLocked(batch)) return
                        val mode = requestedMode ?: MetricSemantics.gaugeMode(normalizedName)
                        val created = GaugeValue(lruEvictionEnabled, mode)
                        if (!created.add(value, null)) {
                            saturatedAddAndGet(batch.dropped, 1L)
                            return
                        }
                        batch.gauges[normalizedName] = created
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

        for ((name, value) in drained.counters) {
            sink.counter(name, value.total())
        }
        for ((name, value) in drained.gauges) {
            emitGauge(sink, name, value.snapshot())
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
            active = Batch(initialMapCapacity, if (lruEvictionEnabled) capacity else 0)
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
            MetricAggregationMode.LAST -> {
                sink.gauge(name, gauge.last, gauge.count, gauge.last, gauge.last, gauge.mode)
            }
            MetricAggregationMode.STATE -> {
                sink.gauge(name, gauge.last, gauge.count, gauge.last, gauge.max, gauge.mode)
            }
            MetricAggregationMode.BOOLEAN_RATE -> {
                val truePct = ((metricSumAsDouble(gauge.total, gauge.totalHigh) * 100.0) / gauge.count.toDouble()).toLong()
                sink.gauge(name, truePct, gauge.count, gauge.total, gauge.max, gauge.mode, gauge.totalHigh)
            }
            MetricAggregationMode.UNKNOWN,
            MetricAggregationMode.AVERAGE -> {
                sink.gauge(name, roundedMetricAverage(gauge.total, gauge.totalHigh, gauge.count), gauge.count, gauge.total, gauge.max, gauge.mode, gauge.totalHigh)
            }
        }
    }

    private fun admitLocked(batch: Batch): Boolean {
        if (capacity <= 0) {
            saturatedAddAndGet(batch.dropped, 1L)
            return false
        }
        if (batch.size() < capacity) return true
        if (exactAdmission) {
            saturatedAddAndGet(batch.dropped, 1L)
            return false
        }

        // Small bounded sets retain the previous LRU behavior. For large sets, scanning thousands
        // of keys would cost more than dropping a new high-cardinality metric.
        if (capacity > LRU_EVICTION_MAX_KEYS) {
            saturatedAddAndGet(batch.dropped, 1L)
            return false
        }

        val candidates = batch.evictionScratch()
        candidates.prepare(batch.counters, batch.gauges)
        try {
            for (index in 0 until candidates.size) {
                val name = candidates.name(index)
                val value = candidates.value(index)
                val lostSamples = value.tryRetire() ?: continue
                val removed = if (candidates.isCounter(index)) {
                    removeRetired(batch.counters, name, value)
                } else {
                    removeRetired(batch.gauges, name, value)
                }
                if (removed) {
                    saturatedAddAndGet(batch.dropped, lostSamples.coerceAtLeast(1L))
                    return true
                }
            }
        } finally {
            candidates.clear()
        }
        // Every candidate is being updated. Dropping one new high-cardinality sample is safer than
        // blocking an arbitrary application thread or the maintenance flush behind that writer.
        saturatedAddAndGet(batch.dropped, 1L)
        return false
    }

    /** Called with [Batch.admissionLock] held; producers cannot replace a value concurrently. */
    private fun <T : MetricValue> removeRetired(
        metrics: ConcurrentHashMap<String, T>,
        name: String,
        value: MetricValue,
    ): Boolean {
        if (metrics[name] !== value) return false
        metrics.remove(name)
        return true
    }

    interface Sink {
        fun counter(name: String, value: Long)
        fun gauge(name: String, value: Long, count: Long, sum: Long, max: Long, mode: MetricAggregationMode, sumHigh: Long = 0L)
    }

    private class Batch(initialMapCapacity: Int, private val evictionCapacity: Int) {
        val counters = ConcurrentHashMap<String, CounterValue>(initialMapCapacity.coerceAtLeast(1))
        val gauges = ConcurrentHashMap<String, GaugeValue>(initialMapCapacity.coerceIn(1, INITIAL_GAUGE_CAPACITY))
        val dropped = AtomicLong()
        val invalidNegative = AtomicLong()
        val admissionLock = Any()
        private var reusableEvictionScratch: EvictionScratch? = null

        fun size(): Int = counters.size + gauges.size

        fun evictionScratch(): EvictionScratch {
            check(evictionCapacity > 0)
            return reusableEvictionScratch ?: EvictionScratch(evictionCapacity).also {
                reusableEvictionScratch = it
            }
        }
    }

    private class EvictionScratch(capacity: Int) {
        private val names = arrayOfNulls<String>(capacity)
        private val values = arrayOfNulls<MetricValue>(capacity)
        private val counterFlags = BooleanArray(capacity)
        private val lastAccess = LongArray(capacity)

        var size: Int = 0
            private set

        fun prepare(
            counters: ConcurrentHashMap<String, CounterValue>,
            gauges: ConcurrentHashMap<String, GaugeValue>,
        ) {
            check(size == 0)
            counters.forEach { (name, value) -> add(name, value, counter = true) }
            gauges.forEach { (name, value) -> add(name, value, counter = false) }
            sortByLastAccess()
        }

        fun name(index: Int): String = checkNotNull(names[index])

        fun value(index: Int): MetricValue = checkNotNull(values[index])

        fun isCounter(index: Int): Boolean = counterFlags[index]

        fun clear() {
            for (index in 0 until size) {
                names[index] = null
                values[index] = null
            }
            size = 0
        }

        private fun add(name: String, value: MetricValue, counter: Boolean) {
            check(size < names.size)
            names[size] = name
            values[size] = value
            counterFlags[size] = counter
            lastAccess[size] = value.lastAccess()
            size++
        }

        private fun sortByLastAccess() {
            for (root in size / 2 - 1 downTo 0) siftDown(root, size)
            for (end in size - 1 downTo 1) {
                swap(0, end)
                siftDown(0, end)
            }
        }

        private fun siftDown(start: Int, endExclusive: Int) {
            var root = start
            while (true) {
                val left = root * 2 + 1
                if (left >= endExclusive) return
                val right = left + 1
                val largest = if (right < endExclusive && lastAccess[right] > lastAccess[left]) right else left
                if (lastAccess[root] >= lastAccess[largest]) return
                swap(root, largest)
                root = largest
            }
        }

        private fun swap(first: Int, second: Int) {
            val name = names[first]
            names[first] = names[second]
            names[second] = name

            val value = values[first]
            values[first] = values[second]
            values[second] = value

            val counter = counterFlags[first]
            counterFlags[first] = counterFlags[second]
            counterFlags[second] = counter

            val access = lastAccess[first]
            lastAccess[first] = lastAccess[second]
            lastAccess[second] = access
        }
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

    private class GaugeValue(
        retirementEnabled: Boolean,
        initialMode: MetricAggregationMode,
    ) : MetricValue(retirementEnabled) {
        private val sampleLock = Any()
        private var count = 0L
        private var total = 0L
        private var totalHigh = 0L
        private var max = 0L
        private var last = 0L
        private var mode = initialMode

        fun add(value: Long, requestedMode: MetricAggregationMode?): Boolean = update {
            // One publication point keeps count, both sum words and last/mode coherent.
            // This replaces the prior three atomic additions plus the last-value monitor.
            synchronized(sampleLock) {
                if (count < Long.MAX_VALUE) count++
                val previous = total
                total += value
                // Samples are nonnegative signed longs: an unsigned carry crosses negative to positive.
                if (previous < 0L && total >= 0L) totalHigh++
                if (value > max) max = value
                last = value
                if (requestedMode != null) mode = requestedMode
            }
        }

        fun snapshot(): GaugeSnapshot = synchronized(sampleLock) {
            GaugeSnapshot(count, total, totalHigh, max, last, mode)
        }

        override fun sampleCount(): Long = synchronized(sampleLock) { count }
    }

    private data class GaugeSnapshot(
        val count: Long,
        val total: Long,
        val totalHigh: Long,
        val max: Long,
        val last: Long,
        val mode: MetricAggregationMode,
    )

    companion object {
        const val DROPPED_METRIC_NAME = "jankhunter.metric_aggregation.dropped.count"
        const val INVALID_METRIC_NAME = "jankhunter.metric.invalid_negative.count"
        private const val DEFAULT_INITIAL_MAP_CAPACITY = 16
        private const val INITIAL_GAUGE_CAPACITY = 4
        private const val LRU_EVICTION_MAX_KEYS = 64
        private const val MAX_KEYS_HARD_LIMIT = 65_536
        private const val MAX_METRIC_NAME_CHARS = 1_024
        private const val MAX_PRODUCER_STRIPES = 8
        private const val PRODUCER_HASH_SHIFT = 16

        private fun producerStripeCount(capacity: Int): Int {
            val target = min(capacity.coerceAtLeast(1), MAX_PRODUCER_STRIPES)
            var stripes = 1
            while (stripes < target) stripes = stripes shl 1
            return stripes
        }

        private fun metricName(name: String?): String? {
            val normalized = name?.trim()?.takeIf { it.isNotEmpty() } ?: "unknown"
            return normalized.takeIf { it.length <= MAX_METRIC_NAME_CHARS }
        }

        private fun saturatedAddAndGet(target: AtomicLong, delta: Long): Long {
            while (true) {
                val current = target.get()
                val updated = if (Long.MAX_VALUE - current < delta) Long.MAX_VALUE else current + delta
                if (target.compareAndSet(current, updated)) return updated
            }
        }

    }
}
