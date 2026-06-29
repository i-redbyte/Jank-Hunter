package io.jankhunter.runtime.internal.io

import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicLong

internal class MetricAggregator(
    maxKeys: Int,
) {
    private val capacity = maxKeys.coerceAtLeast(0)
    private val keyLru = if (capacity <= BitLruCache.MAX_CAPACITY) {
        BitLruCache<MetricKey>(capacity)
    } else {
        null
    }
    private val counters = ConcurrentHashMap<String, AtomicLong>()
    private val gauges = ConcurrentHashMap<String, GaugeStats>()
    private val dropped = AtomicLong()
    private val invalidNegative = AtomicLong()
    private val keyAdmissionLock = Any()

    fun counter(name: String?, value: Long) {
        if (value < 0) {
            invalidNegative.incrementAndGet()
            return
        }
        val key = metricKey(name)
        val metric = MetricKey(MetricKind.COUNTER, key)
        synchronized(keyAdmissionLock) {
            val counter = counters[key] ?: run {
                if (!admitLocked(metric)) return
                AtomicLong().also { counters[key] = it }
            }
            keyLru?.touch(metric)
            counter.addAndGet(value)
        }
    }

    fun gauge(name: String?, value: Long, mode: MetricAggregationMode = MetricAggregationMode.AVERAGE) {
        if (value < 0) {
            invalidNegative.incrementAndGet()
            return
        }
        val key = metricKey(name)
        val metric = MetricKey(MetricKind.GAUGE, key)
        synchronized(keyAdmissionLock) {
            val gauge = gauges[key] ?: run {
                if (!admitLocked(metric)) return
                GaugeStats().also { gauges[key] = it }
            }
            keyLru?.touch(metric)
            gauge.add(value, mode)
        }
    }

    fun flush(sink: Sink) {
        val counterEmissions = ArrayList<Pair<String, Long>>()
        val gaugeEmissions = ArrayList<GaugeEmission>()
        var droppedCount = 0L
        var invalidNegativeCount = 0L

        synchronized(keyAdmissionLock) {
            counters.forEach { (name, counter) ->
                val value = counter.getAndSet(0)
                if (value > 0) {
                    counterEmissions += name to value
                }
            }
            counters.forEach { (name, counter) ->
                if (counter.get() == 0L) {
                    counters.remove(name, counter)
                    keyLru?.remove(MetricKey(MetricKind.COUNTER, name))
                }
            }
            gauges.forEach { (name, gauge) ->
                val snapshot = gauge.snapshotAndReset()
                if (snapshot.count > 0) {
                    gaugeEmissions += GaugeEmission(name, snapshot)
                }
            }
            gauges.forEach { (name, gauge) ->
                if (gauge.isEmpty()) {
                    gauges.remove(name, gauge)
                    keyLru?.remove(MetricKey(MetricKind.GAUGE, name))
                }
            }
            droppedCount = dropped.getAndSet(0)
            invalidNegativeCount = invalidNegative.getAndSet(0)
        }

        counterEmissions.forEach { (name, value) ->
            sink.counter(name, value)
        }
        gaugeEmissions.forEach { emission ->
            val snapshot = emission.snapshot
            when (snapshot.mode) {
                MetricAggregationMode.LAST,
                MetricAggregationMode.STATE -> {
                    sink.gauge(emission.name, snapshot.last, snapshot.count, snapshot.last, snapshot.last, snapshot.mode)
                }
                MetricAggregationMode.BOOLEAN_RATE -> {
                    val truePct = (snapshot.total * 100L) / snapshot.count
                    sink.gauge(emission.name, truePct, snapshot.count, snapshot.total, snapshot.max, snapshot.mode)
                }
                MetricAggregationMode.UNKNOWN,
                MetricAggregationMode.AVERAGE -> {
                    sink.gauge(emission.name, snapshot.average, snapshot.count, snapshot.total, snapshot.max, snapshot.mode)
                }
            }
        }
        if (droppedCount > 0) {
            sink.counter("jankhunter.metric_aggregation.dropped.count", droppedCount)
        }
        if (invalidNegativeCount > 0) {
            sink.counter("jankhunter.metric.invalid_negative.count", invalidNegativeCount)
        }
    }

    private fun admitLocked(key: MetricKey): Boolean {
        if (capacity <= 0) {
            dropped.incrementAndGet()
            return false
        }

        val lru = keyLru
        if (lru == null) {
            if (counters.size + gauges.size >= capacity) {
                dropped.incrementAndGet()
                return false
            }
            return true
        }

        val admission = lru.admit(key)
        if (!admission.admitted) {
            dropped.incrementAndGet()
            return false
        }
        admission.evicted?.let { evicted ->
            removeMetricLocked(evicted)
            dropped.incrementAndGet()
        }
        return true
    }

    private fun removeMetricLocked(key: MetricKey) {
        when (key.kind) {
            MetricKind.COUNTER -> counters.remove(key.name)
            MetricKind.GAUGE -> gauges.remove(key.name)
        }
    }

    interface Sink {
        fun counter(name: String, value: Long)
        fun gauge(name: String, value: Long, count: Long, sum: Long, max: Long, mode: MetricAggregationMode)
    }

    private class GaugeStats {
        private var count = 0L
        private var total = 0L
        private var max = Long.MIN_VALUE
        private var last = 0L
        private var mode = MetricAggregationMode.AVERAGE

        @Synchronized
        fun add(value: Long, aggregationMode: MetricAggregationMode) {
            count++
            total += value
            last = value
            mode = aggregationMode
            if (value > max) {
                max = value
            }
        }

        @Synchronized
        fun snapshotAndReset(): GaugeSnapshot {
            val snapshot = GaugeSnapshot(
                count = count,
                average = if (count > 0) total / count else 0L,
                max = if (max == Long.MIN_VALUE) 0L else max,
                last = last,
                total = total,
                mode = mode,
            )
            count = 0L
            total = 0L
            max = Long.MIN_VALUE
            last = 0L
            return snapshot
        }

        @Synchronized
        fun isEmpty(): Boolean = count == 0L
    }

    private data class GaugeSnapshot(
        val count: Long,
        val average: Long,
        val max: Long,
        val last: Long,
        val total: Long,
        val mode: MetricAggregationMode,
    )

    private data class GaugeEmission(
        val name: String,
        val snapshot: GaugeSnapshot,
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
        private fun metricKey(name: String?): String {
            return name?.trim()?.takeIf { it.isNotEmpty() } ?: "unknown"
        }
    }
}
