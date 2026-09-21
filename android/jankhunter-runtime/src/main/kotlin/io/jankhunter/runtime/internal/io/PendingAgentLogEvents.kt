package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterAgentEventBatch

internal class PendingAgentBatchEvent internal constructor(
    private val pool: PendingAgentBatchEventPool,
) : PendingLogEvent(Jhlog.TYPE_AGENT, null, captureProducer = false) {
    private var words: LongArray? = null
    private var eventCount = 0
    private var written = 0

    override val logicalEventCount: Long
        get() = eventCount.toLong()

    override val remainingEventCount: Long
        get() = (eventCount - written).coerceAtLeast(0).toLong()

    internal fun initialize(
        producerContext: LogEventContext?,
        batch: JankHunterAgentEventBatch,
    ): PendingAgentBatchEvent {
        captureProducer(producerContext)
        words = batch.copyPackedWords()
        eventCount = batch.size
        written = 0
        return this
    }

    internal fun clearForRecycle() {
        clearProducer()
        words = null
        eventCount = 0
        written = 0
    }

    override fun writePayload(writer: BinaryLogWriter) {
        val packed = checkNotNull(words) { "Agent batch event has no words" }
        while (written < eventCount) {
            writer.agentEventFromBatchWords(packed, written)
            written++
        }
    }

    override fun recycle() = pool.release(this)

    override fun rejectBeforeAdmission() = pool.release(this)
}

internal class PendingAgentBatchEventPool(capacity: Int) {
    private val available = io.jankhunter.runtime.internal.concurrent.BoundedMpscQueue<PendingAgentBatchEvent>(capacity)

    fun acquire(
        producerContext: LogEventContext?,
        batch: JankHunterAgentEventBatch,
    ): PendingAgentBatchEvent {
        val event = available.poll() ?: PendingAgentBatchEvent(this)
        return event.initialize(producerContext, batch)
    }

    fun release(event: PendingAgentBatchEvent) {
        event.clearForRecycle()
        available.tryOffer(event)
    }
}

internal class PendingAgentContextEvent(
    producerContext: LogEventContext?,
    private val contextToken: Long,
    private val screen: String?,
    private val owner: String?,
    private val flow: String?,
    private val step: String?,
) : PendingLogEvent(Jhlog.TYPE_AGENT, producerContext) {
    override fun writePayload(writer: BinaryLogWriter) {
        writer.agentContextDefinition(contextToken, screen, owner, flow, step)
    }
}

internal class PendingAgentMethodDefinitionEvent(
    producerContext: LogEventContext?,
    private val methodId: Long,
    private val symbol: String,
) : PendingLogEvent(Jhlog.TYPE_AGENT, producerContext) {
    override fun writePayload(writer: BinaryLogWriter) {
        writer.agentMethodDefinition(methodId, symbol)
    }
}
