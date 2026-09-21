package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterAgentEventBatch
import io.jankhunter.runtime.JankHunterAgentEventSink

/** Bridges ART TI semantic batches into the columnar JHLOG writer. */
internal class CurrentAgentEventSink(
    private val writer: () -> AsyncLogWriter?,
) : JankHunterAgentEventSink {
    override fun tryPublish(batch: JankHunterAgentEventBatch): Boolean {
        if (batch.size == 0) return true
        val asyncWriter = writer() ?: return false
        return asyncWriter.agentBatch(batch)
    }

    override fun tryPublishContext(
        contextToken: Long,
        screen: String?,
        owner: String?,
        flow: String?,
        step: String?,
    ): Boolean {
        if (contextToken == 0L) return false
        val asyncWriter = writer() ?: return false
        return asyncWriter.agentContextDefinition(contextToken, screen, owner, flow, step)
    }

    override fun tryPublishMethodDefinition(methodId: Long, symbol: String): Boolean {
        if (methodId == 0L || symbol.isBlank()) return false
        val asyncWriter = writer() ?: return false
        return asyncWriter.agentMethodDefinition(methodId, symbol)
    }
}
