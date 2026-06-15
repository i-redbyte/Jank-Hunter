package io.jankhunter.runtime.internal.io

import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicLong

class MetricAggregator(
    private val maxKeys: Int,
) {
    private val counters = ConcurrentHashMap<String, AtomicLong>()
    private val gauges = ConcurrentHashMap<String, GaugeStats>()
    private val dropped = AtomicLong()

    fun counter(name: String?, value: Long) {
        val key = metricKey(name)
        val counter = counters[key] ?: run {
            if (counters.size + gauges.size >= maxKeys) {
                dropped.incrementAndGet()
                return
            }
            counters.computeIfAbsent(key) { AtomicLong() }
        }
        counter.addAndGet(value)
    }

    fun gauge(name: String?, value: Long) {
        val key = metricKey(name)
        val gauge = gauges[key] ?: run {
            if (counters.size + gauges.size >= maxKeys) {
                dropped.incrementAndGet()
                return
            }
            gauges.computeIfAbsent(key) { GaugeStats() }
        }
        gauge.add(value)
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
                sink.gauge(name, snapshot.average)
                if (snapshot.max != snapshot.average) {
                    sink.gauge("$name.max", snapshot.max)
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
    }

    interface Sink {
        fun counter(name: String, value: Long)
        fun gauge(name: String, value: Long)
    }

    private class GaugeStats {
        private var count = 0L
        private var total = 0L
        private var max = Long.MIN_VALUE

        @Synchronized
        fun add(value: Long) {
            count++
            total += value
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
            )
            count = 0L
            total = 0L
            max = Long.MIN_VALUE
            return snapshot
        }

        @Synchronized
        fun isEmpty(): Boolean = count == 0L
    }

    private data class GaugeSnapshot(
        val count: Long,
        val average: Long,
        val max: Long,
    )

    companion object {
        private fun metricKey(name: String?): String {
            return name?.trim()?.takeIf { it.isNotEmpty() } ?: "unknown"
        }
    }
}
