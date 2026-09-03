package io.jankhunter.runtime.internal.io

internal class PendingCounterEvent(
    producerContext: LogEventContext?,
    private val name: String?,
    private val value: Long,
) : PendingLogEvent(Jhlog.TYPE_COUNTER, producerContext) {
    override fun writePayload(writer: BinaryLogWriter) = writer.counter(name, value)
}

internal class PendingStableCountersEvent internal constructor(
    private val pool: PendingStableCountersEventPool,
) : PendingLogEvent(Jhlog.TYPE_COUNTER, null, captureProducer = false) {
    private var batch: StableCounterBatch? = null
    private var written = 0

    override val logicalEventCount: Long
        get() = batch?.size?.toLong() ?: 0L

    override val remainingEventCount: Long
        get() = ((batch?.size ?: 0) - written).coerceAtLeast(0).toLong()

    internal fun initialize(
        producerContext: LogEventContext?,
        batch: StableCounterBatch,
    ): PendingStableCountersEvent {
        captureProducer(producerContext)
        this.batch = batch
        written = 0
        return this
    }

    internal fun clearForRecycle(): StableCounterBatch? {
        clearProducer()
        val current = batch
        batch = null
        written = 0
        return current
    }

    override fun writePayload(writer: BinaryLogWriter) {
        val activeBatch = checkNotNull(batch) { "Stable counter event has no batch" }
        while (written < activeBatch.size) {
            val index = written
            writer.stableCounter(activeBatch.id(index), activeBatch.name(index), activeBatch.value(index))
            written++
        }
    }

    override fun recycle() = pool.release(this, recycleBatch = true)

    override fun rejectBeforeAdmission() = pool.release(this, recycleBatch = false)
}

internal class PendingGaugeEvent(
    producerContext: LogEventContext?,
    private val name: String?,
    private val value: Long,
    private val count: Long,
    private val sum: Long,
    private val max: Long,
    private val mode: MetricAggregationMode,
) : PendingLogEvent(Jhlog.TYPE_GAUGE, producerContext) {
    override fun writePayload(writer: BinaryLogWriter) = writer.gauge(name, value, count, sum, max, mode)
}

internal class PendingLogSpamEvent(
    producerContext: LogEventContext?,
    private val screen: String?,
    private val owner: String?,
    private val operationId: Long,
    private val source: String?,
    private val level: Int,
    private val count: Long,
) : PendingLogEvent(Jhlog.TYPE_LOG_SPAM, producerContext) {
    override fun writePayload(writer: BinaryLogWriter) {
        writer.logSpam(screen, owner, operationId, source, level, count)
    }
}

internal class PendingProblemEvent(
    producerContext: LogEventContext?,
    private val screen: String?,
    private val owner: String?,
    private val kind: String?,
    private val windowMs: Long,
    private val count: Long,
    private val maxMs: Long,
    private val foreground: Boolean,
) : PendingLogEvent(Jhlog.TYPE_PROBLEM, producerContext) {
    override fun writePayload(writer: BinaryLogWriter) {
        writer.problemWindow(screen, owner, kind, windowMs, count, maxMs, foreground)
    }
}

internal class PendingRuntimeCallsEvent internal constructor(
    private val pool: PendingRuntimeCallsEventPool,
) : PendingLogEvent(Jhlog.TYPE_RUNTIME_CALL, null, captureProducer = false) {
    private var batch: RuntimeCallBatch? = null
    private var written = 0

    override val logicalEventCount: Long
        get() = batch?.size?.toLong() ?: 0L

    override val remainingEventCount: Long
        get() = ((batch?.size ?: 0) - written).coerceAtLeast(0).toLong()

    internal fun initialize(
        producerContext: LogEventContext?,
        batch: RuntimeCallBatch,
    ): PendingRuntimeCallsEvent {
        captureProducer(producerContext)
        this.batch = batch
        written = 0
        return this
    }

    internal fun clearForRecycle(): RuntimeCallBatch? {
        clearProducer()
        val current = batch
        batch = null
        written = 0
        return current
    }

    override fun writePayload(writer: BinaryLogWriter) {
        val activeBatch = checkNotNull(batch) { "Runtime call event has no batch" }
        if (written >= activeBatch.size) return
        writer.runtimeCalls(activeBatch)
        written = activeBatch.size
    }

    override fun recycle() = pool.release(this, recycleBatch = true)

    // The graph still owns the batch until this event has entered the queue. A failed queue CAS
    // may discard the wrapper, but the same batch must remain valid for the admission retry.
    override fun rejectBeforeAdmission() = pool.release(this, recycleBatch = false)
}
