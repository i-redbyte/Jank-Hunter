package io.jankhunter.runtime.internal.io

import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicLong

class MetricAggregator(
    private val maxKeys: Int,
) {
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
        val counter = counters[key] ?: synchronized(keyAdmissionLock) {
            counters[key] ?: run {
                if (counters.size + gauges.size >= maxKeys) {
                    dropped.incrementAndGet()
                    return
                }
                counters.computeIfAbsent(key) { AtomicLong() }
            }
        }
        counter.addAndGet(value)
    }

    fun gauge(name: String?, value: Long, mode: MetricAggregationMode = MetricAggregationMode.AVERAGE) {
        if (value < 0) {
            invalidNegative.incrementAndGet()
            return
        }
        val key = metricKey(name)
        val gauge = gauges[key] ?: synchronized(keyAdmissionLock) {
            gauges[key] ?: run {
                if (counters.size + gauges.size >= maxKeys) {
                    dropped.incrementAndGet()
                    return
                }
                gauges.computeIfAbsent(key) { GaugeStats() }
            }
        }
        gauge.add(value, mode)
    }

    fun flush(sink: Sink) {
        counters.forEach { (name, counter) ->
            val value = counter.getAndSet(0)
            if (value > 0) {
                sink.counter(name, value)
            }
        }
        counters.forEach { (name, counter) ->
            if (counter.get() == 0L) {
                counters.remove(name, counter)
            }
        }
        gauges.forEach { (name, gauge) ->
            val snapshot = gauge.snapshotAndReset()
            if (snapshot.count > 0) {
                when (snapshot.mode) {
                    MetricAggregationMode.LAST,
                    MetricAggregationMode.STATE -> {
                        sink.gauge(name, snapshot.last, snapshot.count, snapshot.last, snapshot.last, snapshot.mode)
                    }
                    MetricAggregationMode.BOOLEAN_RATE -> {
                        val truePct = (snapshot.total * 100L) / snapshot.count
                        sink.gauge(name, truePct, snapshot.count, snapshot.total, snapshot.max, snapshot.mode)
                    }
                    MetricAggregationMode.UNKNOWN,
                    MetricAggregationMode.AVERAGE -> {
                        sink.gauge(name, snapshot.average, snapshot.count, snapshot.total, snapshot.max, snapshot.mode)
                    }
                }
            }
        }
        gauges.forEach { (name, gauge) ->
            if (gauge.isEmpty()) {
                gauges.remove(name, gauge)
            }
        }
        val droppedCount = dropped.getAndSet(0)
        if (droppedCount > 0) {
            sink.counter("jankhunter.metric_aggregation.dropped.count", droppedCount)
        }
        val invalidNegativeCount = invalidNegative.getAndSet(0)
        if (invalidNegativeCount > 0) {
            sink.counter("jankhunter.metric.invalid_negative.count", invalidNegativeCount)
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

    companion object {
        private fun metricKey(name: String?): String {
            return name?.trim()?.takeIf { it.isNotEmpty() } ?: "unknown"
        }
    }
}
