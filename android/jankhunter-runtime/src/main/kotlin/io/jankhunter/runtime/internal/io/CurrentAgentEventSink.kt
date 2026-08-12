package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterAgentEventBatch
import io.jankhunter.runtime.JankHunterAgentEventSink

/** Adapter from canonical semantic events to the current bounded v9 writer. */
internal class CurrentAgentEventSink(
    private val writer: () -> AsyncLogWriter?,
) : JankHunterAgentEventSink {
    override fun tryPublish(batch: JankHunterAgentEventBatch): Boolean {
        if (batch.size == 0) return true
        val target = writer() ?: return false
        return target.agentEvents(batch).also { accepted ->
            if (!accepted) target.recordQuality(QualityCounterId.AGENT_EVENT_WRITER_REJECTION_LOSS, batch.size.toLong())
        }
    }

    override fun tryPublishContext(
        contextToken: Long,
        screen: String?,
        owner: String?,
        flow: String?,
        step: String?,
    ): Boolean {
        if (contextToken == 0L) return false
        val target = writer() ?: return false
        return target.agentContext(contextToken, screen, owner, flow, step).also { accepted ->
            if (!accepted) target.recordQuality(QualityCounterId.AGENT_EVENT_WRITER_REJECTION_LOSS)
        }
    }

    override fun tryPublishMethodDefinition(methodId: Long, symbol: String): Boolean {
        if (methodId == 0L || symbol.isBlank()) return false
        val target = writer() ?: return false
        return target.agentMethodDefinition(methodId, symbol).also { accepted ->
            if (!accepted) target.recordQuality(QualityCounterId.AGENT_EVENT_WRITER_REJECTION_LOSS)
        }
    }
}
