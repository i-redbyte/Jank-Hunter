package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.MetricAggregator
import io.jankhunter.runtime.internal.io.MetricAggregationMode
import io.jankhunter.runtime.internal.io.MetricSemantics
import java.util.concurrent.atomic.AtomicLong

internal class RuntimeMetricsService(
    defaultMaxKeys: Int,
    private val nowMs: () -> Long,
    private val writer: () -> AsyncLogWriter?,
    private val config: () -> JankHunterConfig?,
    private val ensureContextRecorded: () -> Unit,
) {
    private val lastFlushAtMs = AtomicLong(0L)

    @Volatile
    private var aggregator = MetricAggregator(defaultMaxKeys)

    fun configure(maxKeys: Int) {
        aggregator = MetricAggregator(maxKeys)
        lastFlushAtMs.set(0L)
    }

    fun reset() {
        lastFlushAtMs.set(0L)
    }

    fun recordCounter(name: String?, value: Long) {
        val asyncWriter = writer() ?: return
        if (shouldAggregate()) {
            aggregator.counter(name, value)
            flush(force = false)
            return
        }
        ensureContextRecorded()
        asyncWriter.counter(name, value)
    }

    fun recordGauge(name: String?, value: Long) {
        val asyncWriter = writer() ?: return
        val mode = MetricSemantics.gaugeMode(name)
        if (shouldAggregate()) {
            aggregator.gauge(name, value, mode)
            flush(force = false)
            return
        }
        ensureContextRecorded()
        asyncWriter.gauge(name, value, count = 1L, sum = value, max = value, mode = mode)
    }

    fun flush(force: Boolean) {
        val asyncWriter = writer() ?: return
        val localConfig = config() ?: return
        if (!localConfig.metricAggregationEnabled()) return
        val now = nowMs()
        val last = lastFlushAtMs.get()
        val interval = localConfig.metricAggregationWindowMs()
        if (!force && (interval <= 0 || now - last < interval)) return
        if (!lastFlushAtMs.compareAndSet(last, now) && !force) return
        ensureContextRecorded()
        aggregator.flush(object : MetricAggregator.Sink {
            override fun counter(name: String, value: Long) {
                asyncWriter.counter(name, value)
            }

            override fun gauge(
                name: String,
                value: Long,
                count: Long,
                sum: Long,
                max: Long,
                mode: MetricAggregationMode,
            ) {
                asyncWriter.gauge(name, value, count, sum, max, mode)
            }
        })
    }

    private fun shouldAggregate(): Boolean {
        val localConfig = config() ?: return false
        return localConfig.metricAggregationEnabled() && localConfig.maxMetricAggregationKeys() > 0
    }
}
