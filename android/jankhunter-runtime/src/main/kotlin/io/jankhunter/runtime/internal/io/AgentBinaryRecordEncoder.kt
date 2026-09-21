package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterAgentEventBatch
import io.jankhunter.runtime.JankHunterAgentEventFlag
import io.jankhunter.runtime.JankHunterAgentEventType

/** Encodes JVM TI / ART TI agent semantic records into TYPE_AGENT payloads. */
internal class AgentBinaryRecordEncoder(
    private val sink: BinaryEncodingSink,
) {
    fun fromBatchWords(words: LongArray, index: Int) {
        val offset = index * JankHunterAgentEventBatch.WORDS_PER_EVENT
        val header = words[offset + JankHunterAgentEventBatch.TYPE_SCHEMA_FLAGS]
        val type = (header and UINT16_MASK).toInt()
        val schemaVersion = ((header ushr 16) and UINT16_MASK).toInt()
        val flags = (header ushr 32).toInt()
        agent(
            semanticType = type,
            schemaVersion = schemaVersion,
            eventFlags = flags.toLong(),
            producerSequence = words[offset + JankHunterAgentEventBatch.PRODUCER_SEQUENCE],
            monotonicNs = words[offset + JankHunterAgentEventBatch.MONOTONIC_NS],
            producerId = words[offset + JankHunterAgentEventBatch.PRODUCER_ID],
            threadToken = words[offset + JankHunterAgentEventBatch.THREAD_TOKEN],
            contextToken = words[offset + JankHunterAgentEventBatch.CONTEXT_TOKEN],
            payload0 = words[offset + JankHunterAgentEventBatch.PAYLOAD_0],
            payload1 = words[offset + JankHunterAgentEventBatch.PAYLOAD_1],
            payload2 = words[offset + JankHunterAgentEventBatch.PAYLOAD_2],
            payload3 = words[offset + JankHunterAgentEventBatch.PAYLOAD_3],
            methodSymbol = null,
            recordContext = null,
        )
    }

    fun contextDefinition(
        contextToken: Long,
        screen: String?,
        owner: String?,
        flow: String?,
        step: String?,
    ) {
        val flowLabel = flowOperationLabel(flow, step)
        val operationId = sink.optionalSymbolId(BinaryLogWriter.DICT_GENERIC, flowLabel)
        val context = sink.context(screen, owner, operationId)
        agent(
            semanticType = JankHunterAgentEventType.CORRELATION_LINK,
            schemaVersion = 1,
            eventFlags = JankHunterAgentEventFlag.CONTEXT_DEFINITION.toLong(),
            producerSequence = 0L,
            monotonicNs = 0L,
            producerId = 0L,
            threadToken = 0L,
            contextToken = contextToken,
            payload0 = contextToken,
            payload1 = 0L,
            payload2 = 0L,
            payload3 = 0L,
            methodSymbol = null,
            recordContext = context,
        )
    }

    fun methodDefinition(methodId: Long, symbol: String) {
        agent(
            semanticType = JankHunterAgentEventType.METHOD_DEFINITION,
            schemaVersion = 1,
            eventFlags = 0L,
            producerSequence = 0L,
            monotonicNs = 0L,
            producerId = 0L,
            threadToken = 0L,
            contextToken = 0L,
            payload0 = methodId,
            payload1 = 0L,
            payload2 = 0L,
            payload3 = 0L,
            methodSymbol = symbol,
            recordContext = null,
        )
    }

    @Suppress("LongParameterList")
    private fun agent(
        semanticType: Int,
        schemaVersion: Int,
        eventFlags: Long,
        producerSequence: Long,
        monotonicNs: Long,
        producerId: Long,
        threadToken: Long,
        contextToken: Long,
        payload0: Long,
        payload1: Long,
        payload2: Long,
        payload3: Long,
        methodSymbol: String?,
        recordContext: BinaryRecordContext?,
    ) {
        val payload = sink.payload()
            .uvarint(semanticType.toLong())
            .uvarint(schemaVersion.toLong())
            .uvarint(producerSequence)
            .uvarint(producerId)
            .uvarint(threadToken)
            .uvarint(contextToken)
            .uvarint(eventFlags)
            .uvarint(payload0)
            .uvarint(payload1)
            .uvarint(payload2)
            .uvarint(payload3)
        if (semanticType == JankHunterAgentEventType.METHOD_DEFINITION && !methodSymbol.isNullOrBlank()) {
            payload.symbolRef(sink.symbolId(BinaryLogWriter.DICT_METHOD, methodSymbol, SymbolOrigin.RUNTIME_STACK))
        }
        sink.emitSemantic(Jhlog.TYPE_AGENT, 0L, payload, recordContext ?: sink.producerContext())
    }

    private fun flowOperationLabel(flow: String?, step: String?): String? {
        if (flow.isNullOrBlank() && step.isNullOrBlank()) return null
        if (step.isNullOrBlank()) return flow
        if (flow.isNullOrBlank()) return step
        return buildString(flow.length + step.length + 1) {
            append(flow)
            append(':')
            append(step)
        }
    }

    private companion object {
        private const val UINT16_MASK = 0xFFFFL
    }
}
