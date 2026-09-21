package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterAgentEventBatch
import io.jankhunter.runtime.JankHunterAgentEventSink

/**
 * Bridges ART TI batches into the runtime writer.
 *
 * Agent event encoding for the current columnar JHLOG writer is still being ported from the
 * legacy v9 path; until then batches are accepted without blocking native producers.
 */
internal class CurrentAgentEventSink(
    private val writer: () -> AsyncLogWriter?,
) : JankHunterAgentEventSink {
    override fun tryPublish(batch: JankHunterAgentEventBatch): Boolean {
        if (batch.size == 0) return true
        return writer() != null
    }

    override fun tryPublishContext(
        contextToken: Long,
        screen: String?,
        owner: String?,
        flow: String?,
        step: String?,
    ): Boolean {
        if (contextToken == 0L) return false
        return writer() != null
    }

    override fun tryPublishMethodDefinition(methodId: Long, symbol: String): Boolean {
        if (methodId == 0L || symbol.isBlank()) return false
        return writer() != null
    }
}
