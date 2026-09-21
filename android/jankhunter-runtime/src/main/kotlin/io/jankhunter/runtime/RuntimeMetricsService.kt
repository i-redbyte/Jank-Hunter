package io.jankhunter.runtime

import io.jankhunter.runtime.internal.monotonicDeadlineAfterMillis
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.MetricAggregationMode
import io.jankhunter.runtime.internal.io.MetricAggregator
import io.jankhunter.runtime.internal.io.MetricSemantics
import io.jankhunter.runtime.internal.io.QualityCounterId
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.locks.ReentrantLock
import kotlin.concurrent.withLock

internal class RuntimeMetricsService(
    defaultMaxKeys: Int,
    private val nowMs: RuntimeLongSource,
    private val writer: () -> AsyncLogWriter?,
    private val config: () -> JankHunterConfig?,
    private val ensureContextRecorded: () -> Unit,
    private val executeMaintenance: RuntimeTaskExecutor,
    private val executeDelayedMaintenance: RuntimeDelayedTaskExecutor,
    private val executeBlockingDrain: RuntimeTaskExecutor = RuntimeMetricDrainExecutor(),
) {
    private val lastFlushAtMs = AtomicLong(0L)
    private val metricGeneration = AtomicLong(0L)
    private val windowFlushQueued = AtomicBoolean(false)
    private val immediateFlushQueued = AtomicBoolean(false)
    private val flushLock = ReentrantLock()

    @Volatile
    private var aggregator = MetricAggregator(defaultMaxKeys)

    fun configure(maxKeys: Int, exactAdmission: Boolean) {
        replaceAggregator(MetricAggregator(maxKeys, exactAdmission), nowMs.getAsLong())
    }

    fun reset() {
        // The old drain owns its captured accumulator and writer. Reset never waits for its I/O.
        replaceAggregator(MetricAggregator(0), 0L)
    }

    private fun replaceAggregator(replacement: MetricAggregator, startedAtMs: Long) {
        aggregator = replacement
        lastFlushAtMs.set(startedAtMs)
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
        if (AsyncLogWriter.isCriticalMetricName(name)) {
            ensureContextRecorded()
            val mode = MetricSemantics.gaugeMode(name)
            asyncWriter.gauge(name, value, count = 1L, sum = value, max = value, mode = mode)
            return
        }
        if (shouldAggregate()) {
            aggregator.gaugeClassified(name, value)
            metricGeneration.incrementAndGet()
            scheduleWindowFlush()
            return
        }
        ensureContextRecorded()
        val mode = MetricSemantics.gaugeMode(name)
        asyncWriter.gauge(name, value, count = 1L, sum = value, max = value, mode = mode)
    }

    fun recordExecutorQueueChanged(
        keys: ExecutorMetricKeys,
        queueDepth: Int,
        activeCount: Int,
        poolSize: Int,
        completedTaskCount: Long,
    ) {
        recordMetricBatch { asyncWriter, aggregate ->
            recordBatchGauge(asyncWriter, aggregate, keys.queueDepth, queueDepth.toLong())
            recordExecutorPoolSnapshot(
                asyncWriter,
                aggregate,
                keys,
                activeCount,
                poolSize,
                completedTaskCount,
            )
        }
    }

    fun recordExecutorStarted(
        keys: ExecutorMetricKeys,
        waitMs: Long,
        queueDepth: Int,
        activeCount: Int,
        poolSize: Int,
        completedTaskCount: Long,
        scheduled: Boolean = false,
    ) {
        recordMetricBatch { asyncWriter, aggregate ->
            if (scheduled) {
                if (waitMs >= 0L) recordBatchGauge(asyncWriter, aggregate, checkNotNull(keys.scheduledLateness), waitMs)
            } else if (waitMs > 0L) recordBatchGauge(asyncWriter, aggregate, keys.wait, waitMs)
            recordBatchCounter(asyncWriter, aggregate, keys.started, 1L)
            keys.ownerStarted?.let { recordBatchCounter(asyncWriter, aggregate, it, 1L) }
            recordBatchGauge(asyncWriter, aggregate, keys.queueDepth, queueDepth.toLong())
            recordExecutorPoolSnapshot(
                asyncWriter,
                aggregate,
                keys,
                activeCount,
                poolSize,
                completedTaskCount,
            )
        }
    }

    fun recordExecutorFinished(
        keys: ExecutorMetricKeys,
        durationMs: Long,
        failed: Boolean,
        recordOwnerDuration: Boolean,
    ) {
        recordMetricBatch { asyncWriter, aggregate ->
            if (durationMs >= 0L) recordBatchGauge(asyncWriter, aggregate, keys.service, durationMs)
            if (failed) {
                recordBatchCounter(asyncWriter, aggregate, keys.failure, 1L)
                recordBatchCounter(asyncWriter, aggregate, keys.ownerFailure, 1L)
            }
            if (recordOwnerDuration) {
                recordBatchGauge(asyncWriter, aggregate, keys.ownerDuration, durationMs)
            }
        }
    }

    /** Only the caller's wait is blocking, including when called by a maintenance task. */
    fun flushBlocking(timeoutMs: Long): Boolean = flushBlocking(timeoutMs, writer())

    /** The caller never drains inline; late work can only use the captured writer. */
    fun flushBlocking(timeoutMs: Long, expectedWriter: AsyncLogWriter?): Boolean {
        if (expectedWriter == null) return true
        val localConfig = config() ?: return true
        if (!localConfig.metricAggregationEnabled() || localConfig.maxMetricAggregationKeys() <= 0) return true
        val deadlineNs = monotonicDeadlineAfterMillis(timeoutMs.coerceAtLeast(0L))
        val expectedAggregator = aggregator
        val completed = CountDownLatch(1)
        val succeeded = AtomicBoolean()
        val accepted = executeBlockingDrain.execute {
            try {
                val acquired = flushLock.tryLock((deadlineNs - System.nanoTime()).coerceAtLeast(0L), TimeUnit.NANOSECONDS)
                if (!acquired) return@execute
                try {
                    if (aggregator === expectedAggregator && writer() === expectedWriter) {
                        flushLocked(expectedWriter, expectedAggregator)
                        succeeded.set(true)
                    }
                } finally {
                    flushLock.unlock()
                }
            } finally {
                completed.countDown()
            }
        }
        if (!accepted) return false
        return try {
            val remainingNs = (deadlineNs - System.nanoTime()).coerceAtLeast(0L)
            completed.await(remainingNs, TimeUnit.NANOSECONDS) && succeeded.get()
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
            false
        }
    }

    /** Requests an immediate asynchronous drain and writer flush. */
    fun requestFlush(): Boolean {
        if (!shouldAggregate()) return false
        val activeWriter = writer() ?: return false
        val activeAggregator = aggregator
        if (!immediateFlushQueued.compareAndSet(false, true)) return true

        val accepted = executeMaintenance.execute {
            val generationBeforeFlush = metricGeneration.get()
            try {
                flushNow(activeWriter, activeAggregator)?.flush()
            } finally {
                if (aggregator === activeAggregator) {
                    immediateFlushQueued.set(false)
                    if (metricGeneration.get() != generationBeforeFlush) requestFlush()
                }
            }
        }
        if (!accepted && aggregator === activeAggregator) immediateFlushQueued.set(false)
        return accepted
    }

    private fun scheduleWindowFlush() {
        if (!windowFlushQueued.compareAndSet(false, true)) return
        val localConfig = config()
        if (localConfig == null) {
            windowFlushQueued.set(false)
            return
        }
        if (!localConfig.metricAggregationEnabled()) {
            windowFlushQueued.set(false)
            return
        }
        val activeWriter = writer()
        val activeAggregator = aggregator
        val now = nowMs.getAsLong()
        val last = lastFlushAtMs.get()
        val elapsed = (now - last).coerceAtLeast(0L)
        val delayMs = (localConfig.metricAggregationWindowMs() - elapsed).coerceAtLeast(0L)

        val accepted = executeDelayedMaintenance.execute(delayMs) {
            val generationBeforeFlush = metricGeneration.get()
            try {
                flushNow(activeWriter, activeAggregator)
            } finally {
                if (aggregator === activeAggregator) {
                    windowFlushQueued.set(false)
                    if (metricGeneration.get() != generationBeforeFlush) scheduleWindowFlush()
                }
            }
        }
        if (!accepted && aggregator === activeAggregator) windowFlushQueued.set(false)
    }

    private fun flushNow(expectedWriter: AsyncLogWriter?, activeAggregator: MetricAggregator): AsyncLogWriter? {
        val targetWriter = expectedWriter ?: return null
        flushLock.withLock {
            if (writer() !== targetWriter || aggregator !== activeAggregator) return null
            flushLocked(targetWriter, activeAggregator)
            return targetWriter
        }
    }

    /** Called only while holding flushLock and using the captured writer/accumulator pair. */
    private fun flushLocked(asyncWriter: AsyncLogWriter, activeAggregator: MetricAggregator) {
        activeAggregator.flush(object : MetricAggregator.Sink {
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
                sumHigh: Long,
            ) {
                asyncWriter.gaugeWide(name, value, count, sum, max, mode, sumHigh)
            }
        })
        if (aggregator === activeAggregator) lastFlushAtMs.set(nowMs.getAsLong())
    }

    private inline fun recordMetricBatch(record: (AsyncLogWriter, Boolean) -> Unit) {
        val asyncWriter = writer() ?: return
        val aggregate = shouldAggregate()
        if (!aggregate) ensureContextRecorded()
        record(asyncWriter, aggregate)
        if (aggregate) {
            metricGeneration.incrementAndGet()
            scheduleWindowFlush()
        }
    }

    private fun recordExecutorPoolSnapshot(
        asyncWriter: AsyncLogWriter,
        aggregate: Boolean,
        keys: ExecutorMetricKeys,
        activeCount: Int,
        poolSize: Int,
        completedTaskCount: Long,
    ) {
        if (activeCount < 0) return
        recordBatchGauge(asyncWriter, aggregate, keys.activeCount, activeCount.toLong())
        recordBatchGauge(asyncWriter, aggregate, keys.poolSize, poolSize.toLong())
        recordBatchGauge(asyncWriter, aggregate, keys.completedTaskCount, completedTaskCount)
    }

    private fun recordBatchCounter(
        asyncWriter: AsyncLogWriter,
        aggregate: Boolean,
        name: String,
        value: Long,
    ) {
        if (aggregate) aggregator.counter(name, value) else asyncWriter.counter(name, value)
    }

    private fun recordBatchGauge(
        asyncWriter: AsyncLogWriter,
        aggregate: Boolean,
        name: String,
        value: Long,
    ) {
        if (aggregate) {
            aggregator.gaugeClassified(name, value)
        } else {
            val mode = MetricSemantics.gaugeMode(name)
            asyncWriter.gauge(name, value, count = 1L, sum = value, max = value, mode = mode)
        }
    }

    private fun shouldAggregate(): Boolean {
        val localConfig = config() ?: return false
        return localConfig.metricAggregationEnabled() && localConfig.maxMetricAggregationKeys() > 0
    }
}
