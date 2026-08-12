package io.jankhunter.artti.internal

import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterAgentEventBatch
import io.jankhunter.runtime.JankHunterAgentEventSink
import io.jankhunter.runtime.JankHunterAgentEventType
import java.nio.ByteBuffer

internal enum class ArtTiProcessingIssue {
    DRAIN_FAILED,
    DECODE_FAILED,
    CONTEXT_TABLE_DROP,
    CANONICAL_BATCH_DROP,
    EVENT_SINK_DROP,
    METHOD_RESOLUTION_DROP,
    FINAL_DRAIN_FAILED,
    FINAL_DRAIN_TRUNCATED,
}

internal enum class ArtTiDrainOutcome {
    SUCCESS,
    NATIVE_ERROR,
    DECODE_ERROR,
}

/** Owns reusable drain buffers and translates native records into storage-neutral events. */
internal class ArtTiEventProcessor(
    private val runtimeConfig: ArtTiRuntimeConfig,
    private val eventSink: JankHunterAgentEventSink,
    private val onIssue: (ArtTiProcessingIssue) -> Unit,
    private val onCapabilityDegraded: () -> Unit,
    private val onLongContention: (threadToken: Long, contextToken: Long, sequence: Long) -> Unit,
) {
    private val output = ArtTiNativeProtocol.allocateDrainBuffer(runtimeConfig.native.drainBatchSize)
    private val decoder = ArtTiNativeProtocolDecoder()
    private val contexts = ArtTiThreadContextTable(runtimeConfig.native.maxTrackedThreads)
    private val semanticBatch = JankHunterAgentEventBatch(runtimeConfig.native.drainBatchSize)
    private val methodBuffer = ArtTiMethodDefinition.allocateBuffer()
    private var resolveFinalDrainMethods = false

    private val regularVisitor = ArtTiNativeRecordVisitor(::onNativeRecord)
    private val finalDrainVisitor = ArtTiNativeRecordVisitor { record ->
        if (resolveFinalDrainMethods && record.type == JankHunterAgentEventType.STACK_DEFINITION) {
            resolveMethodDefinition(record.payload1)
        }
        appendCanonical(record)
    }

    fun drainOnce(): ArtTiDrainOutcome {
        semanticBatch.clear()
        val bytesWritten = ArtTiNativeBridge.nativeDrain(output, runtimeConfig.native.drainBatchSize)
        if (bytesWritten < 0) {
            onIssue(ArtTiProcessingIssue.DRAIN_FAILED)
            return ArtTiDrainOutcome.NATIVE_ERROR
        }

        val result = decoder.decode(output, bytesWritten, regularVisitor)
        if (result.status != ArtTiNativeStatus.OK) {
            onIssue(ArtTiProcessingIssue.DECODE_FAILED)
            return ArtTiDrainOutcome.DECODE_ERROR
        }

        publishBatch()
        return ArtTiDrainOutcome.SUCCESS
    }

    fun drainRemaining(resolveMethods: Boolean) {
        resolveFinalDrainMethods = resolveMethods
        repeat(MAX_FINAL_DRAIN_BATCHES) {
            semanticBatch.clear()
            val bytesWritten = ArtTiNativeBridge.nativeDrain(output, runtimeConfig.native.drainBatchSize)
            if (bytesWritten < 0) {
                onIssue(ArtTiProcessingIssue.FINAL_DRAIN_FAILED)
                return
            }
            val result = decoder.decode(output, bytesWritten, finalDrainVisitor)
            if (result.status != ArtTiNativeStatus.OK) {
                onIssue(ArtTiProcessingIssue.FINAL_DRAIN_FAILED)
                return
            }
            publishBatch()
            if (result.recordsSeen == 0) return
        }
        onIssue(ArtTiProcessingIssue.FINAL_DRAIN_TRUNCATED)
    }

    private fun onNativeRecord(record: ArtTiNativeRecordView) {
        when (record.type) {
            JankHunterAgentEventType.AGENT_STATUS -> {
                JankHunter.recordGauge("jankhunter.artti.agent_status", record.payload0)
            }
            JankHunterAgentEventType.AGENT_CAPABILITY -> onCapabilityRecord(record)
            JankHunterAgentEventType.AGENT_QUALITY_SNAPSHOT -> {
                JankHunter.recordGauge("jankhunter.artti.queue.high_watermark", record.payload0)
                JankHunter.recordGauge("jankhunter.artti.queue.full_total", record.payload1)
                JankHunter.recordGauge("jankhunter.artti.queue.contention_total", record.payload2)
                JankHunter.recordGauge("jankhunter.artti.native_other_loss_total", record.payload3)
            }
            JankHunterAgentEventType.THREAD_END -> contexts.remove(record.threadToken)
            JankHunterAgentEventType.MONITOR_CONTENTION_INTERVAL -> onLongContention(
                record.threadToken,
                contexts.get(record.threadToken),
                record.producerSequence,
            )
            JankHunterAgentEventType.STACK_DEFINITION -> resolveMethodDefinition(record.payload1)
            JankHunterAgentEventType.CORRELATION_LINK -> {
                if (!contexts.put(record.threadToken, record.contextToken)) {
                    onIssue(ArtTiProcessingIssue.CONTEXT_TABLE_DROP)
                }
            }
        }
        appendCanonical(record)
    }

    private fun onCapabilityRecord(record: ArtTiNativeRecordView) {
        val requested = record.payload0
        val granted = record.payload3
        JankHunter.recordGauge("jankhunter.artti.capabilities.requested", requested)
        JankHunter.recordGauge("jankhunter.artti.capabilities.potential", record.payload1)
        JankHunter.recordGauge("jankhunter.artti.capabilities.granted", record.payload2)
        JankHunter.recordGauge("jankhunter.artti.capabilities.active", granted)
        if ((granted and requested) != requested) onCapabilityDegraded()
    }

    private fun appendCanonical(record: ArtTiNativeRecordView) {
        val appended = semanticBatch.tryAppend(
            type = record.type,
            schemaVersion = record.schemaVersion,
            flags = record.flags,
            producerSequence = record.producerSequence,
            monotonicNs = record.monotonicNs,
            producerId = record.producerId,
            threadToken = record.threadToken,
            contextToken = record.contextToken,
            payload0 = record.payload0,
            payload1 = record.payload1,
            payload2 = record.payload2,
            payload3 = record.payload3,
        )
        if (!appended) onIssue(ArtTiProcessingIssue.CANONICAL_BATCH_DROP)
    }

    private fun resolveMethodDefinition(methodId: Long) {
        if (methodId == 0L) return
        val bytesWritten = ArtTiNativeBridge.nativeResolveMethod(methodId, methodBuffer)
        if (bytesWritten == 0) return
        if (bytesWritten < 0) {
            onIssue(ArtTiProcessingIssue.METHOD_RESOLUTION_DROP)
            return
        }
        val definition = ArtTiMethodDefinition.decode(methodBuffer, bytesWritten).getOrElse {
            onIssue(ArtTiProcessingIssue.METHOD_RESOLUTION_DROP)
            return
        }
        if (!eventSink.tryPublishMethodDefinition(definition.methodId, definition.symbol())) {
            onIssue(ArtTiProcessingIssue.EVENT_SINK_DROP)
        }
    }

    private fun publishBatch() {
        if (semanticBatch.size > 0 && !eventSink.tryPublish(semanticBatch)) {
            onIssue(ArtTiProcessingIssue.EVENT_SINK_DROP)
        }
    }

    private fun ArtTiMethodDefinition.symbol(): String = buildString {
        append(classSignature)
        append("->")
        append(methodName)
        append(methodSignature)
    }.take(MAX_METHOD_SYMBOL_LENGTH)

    private companion object {
        const val MAX_METHOD_SYMBOL_LENGTH = 2_048
        const val MAX_FINAL_DRAIN_BATCHES = 64
    }
}
