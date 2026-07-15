package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.MetricAggregationMode
import io.jankhunter.runtime.internal.io.MetricAggregator
import io.jankhunter.runtime.internal.io.MetricSemantics
import io.jankhunter.runtime.internal.io.QualityCounterId
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong

internal class RuntimeMetricsService(
    defaultMaxKeys: Int,
    private val nowMs: () -> Long,
    private val writer: () -> AsyncLogWriter?,
    private val config: () -> JankHunterConfig?,
    private val ensureContextRecorded: () -> Unit,
    private val executeMaintenance: (task: () -> Unit) -> Boolean,
    private val executeDelayedMaintenance: (delayMs: Long, task: () -> Unit) -> Boolean,
    private val executeMaintenanceAndWait: (timeoutMs: Long, task: () -> Unit) -> Boolean,
) {
    private val lastFlushAtMs = AtomicLong(0L)
    private val metricGeneration = AtomicLong(0L)
    private val windowFlushQueued = AtomicBoolean(false)
    private val immediateFlushQueued = AtomicBoolean(false)
    private val flushLock = Any()

    @Volatile
    private var aggregator = MetricAggregator(defaultMaxKeys)

    fun configure(maxKeys: Int) {
        synchronized(flushLock) {
            aggregator = MetricAggregator(maxKeys)
            lastFlushAtMs.set(nowMs())
            metricGeneration.set(0L)
            windowFlushQueued.set(false)
            immediateFlushQueued.set(false)
        }
    }

    fun reset() {
        lastFlushAtMs.set(0L)
        metricGeneration.set(0L)
        windowFlushQueued.set(false)
        immediateFlushQueued.set(false)
    }

    fun recordCounter(name: String?, value: Long) {
        val asyncWriter = writer() ?: return
        if (AsyncLogWriter.isCriticalMetricName(name)) {
            ensureContextRecorded()
            asyncWriter.counter(name, value)
            return
        }
        if (shouldAggregate()) {
            if (value == 0L) return
            aggregator.counter(name, value)
            metricGeneration.incrementAndGet()
            scheduleWindowFlush()
            return
        }
        ensureContextRecorded()
        asyncWriter.counter(name, value)
    }

    fun recordGauge(name: String?, value: Long) {
        val asyncWriter = writer() ?: return
        val mode = MetricSemantics.gaugeMode(name)
        if (AsyncLogWriter.isCriticalMetricName(name)) {
            ensureContextRecorded()
            asyncWriter.gauge(name, value, count = 1L, sum = value, max = value, mode = mode)
            return
        }
        if (shouldAggregate()) {
            aggregator.gauge(name, value, mode)
            metricGeneration.incrementAndGet()
            scheduleWindowFlush()
            return
        }
        ensureContextRecorded()
        asyncWriter.gauge(name, value, count = 1L, sum = value, max = value, mode = mode)
    }

    /** Drains on the maintenance thread and only bounds-waits on the caller. */
    fun flushBlocking(timeoutMs: Long): Boolean {
        val localConfig = config() ?: return true
        if (!localConfig.metricAggregationEnabled() || localConfig.maxMetricAggregationKeys() <= 0) return true
        return executeMaintenanceAndWait(timeoutMs.coerceAtLeast(1L)) {
            flushNow()
        }
    }

    /** Requests an immediate asynchronous drain and writer flush. */
    fun requestFlush(): Boolean {
        if (!shouldAggregate() || writer() == null) return false
        if (!immediateFlushQueued.compareAndSet(false, true)) return true

        val accepted = executeMaintenance {
            val generationBeforeFlush = metricGeneration.get()
            try {
                flushNow()?.flush()
            } finally {
                immediateFlushQueued.set(false)
                if (metricGeneration.get() != generationBeforeFlush) requestFlush()
            }
        }
        if (!accepted) immediateFlushQueued.set(false)
        return accepted
    }

    private fun scheduleWindowFlush() {
        val localConfig = config() ?: return
        if (!localConfig.metricAggregationEnabled()) return
        val now = nowMs()
        val last = lastFlushAtMs.get()
        val elapsed = (now - last).coerceAtLeast(0L)
        val delayMs = (localConfig.metricAggregationWindowMs() - elapsed).coerceAtLeast(0L)
        if (!windowFlushQueued.compareAndSet(false, true)) return

        val accepted = executeDelayedMaintenance(delayMs) {
            val generationBeforeFlush = metricGeneration.get()
            try {
                flushNow()
            } finally {
                windowFlushQueued.set(false)
                if (metricGeneration.get() != generationBeforeFlush) scheduleWindowFlush()
            }
        }
        if (!accepted) windowFlushQueued.set(false)
    }

    private fun flushNow(): AsyncLogWriter? {
        synchronized(flushLock) {
            val asyncWriter = writer() ?: return null
            aggregator.flush(object : MetricAggregator.Sink {
                override fun counter(name: String, value: Long) {
                    when (name) {
                        MetricAggregator.DROPPED_METRIC_NAME -> {
                            asyncWriter.recordQuality(QualityCounterId.METRIC_CARDINALITY_LOSS, value)
                        }
                        MetricAggregator.INVALID_METRIC_NAME -> {
                            asyncWriter.recordQuality(QualityCounterId.INVALID_METRIC, value)
                        }
                        else -> asyncWriter.counter(name, value)
                    }
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
            lastFlushAtMs.set(nowMs())
            return asyncWriter
        }
    }

    private fun shouldAggregate(): Boolean {
        val localConfig = config() ?: return false
        return localConfig.metricAggregationEnabled() && localConfig.maxMetricAggregationKeys() > 0
    }
}
