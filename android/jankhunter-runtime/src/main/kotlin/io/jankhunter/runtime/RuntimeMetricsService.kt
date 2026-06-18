package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.MetricAggregator
import java.util.concurrent.atomic.AtomicLong

internal class RuntimeMetricsService(
    defaultMaxKeys: Int,
    private val nowMs: () -> Long,
    private val writer: () -> AsyncLogWriter?,
    private val config: () -> JankHunterConfig?,
    private val ensureContextRecorded: () -> Unit,
    private val events: RuntimeEventBus,
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
        if (shouldAggregate()) {
            aggregator.gauge(name, value)
            flush(force = false)
            return
        }
        ensureContextRecorded()
        asyncWriter.gauge(name, value)
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

            override fun gauge(name: String, value: Long) {
                asyncWriter.gauge(name, value)
            }
        })
        events.emit(RuntimeEvent.MetricsFlushed(force))
    }

    private fun shouldAggregate(): Boolean {
        val localConfig = config() ?: return false
        return localConfig.metricAggregationEnabled() && localConfig.maxMetricAggregationKeys() > 0
    }
}
