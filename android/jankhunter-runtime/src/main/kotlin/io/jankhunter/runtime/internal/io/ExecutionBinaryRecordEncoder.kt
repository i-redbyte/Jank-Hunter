package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterOperationAttributes

/** Validates and encodes IO, worker and structured-operation execution records. */
internal class ExecutionBinaryRecordEncoder(
    private val sink: BinaryEncodingSink,
) {
    fun io(
        operation: Long,
        durationUs: Long,
        bytes: Long,
        mainThread: Boolean,
        sourceId: Long,
        sourceName: String?,
        outcome: Long,
        bytesKnown: Boolean,
    ) {
        require(
            operation in Jhlog.IO_FILE_READ..Jhlog.IO_FILE_SYNC ||
                operation in Jhlog.IO_CONTENT_READ..Jhlog.IO_CONTENT_WRITE,
        )
        require(outcome in Jhlog.IO_OUTCOME_SUCCESS..Jhlog.IO_OUTCOME_FAILURE)
        require(bytesKnown || bytes == 0L)
        val sourceAlias = if (sourceId != 0L) sink.defineStableSymbol(sourceId, sourceName) else 0L
        val payload = sink.payload()
        if (sourceId != 0L) payload.stableSymbolAlias(sourceAlias) else payload.symbolRef(0L)
        payload
            .uvarint(operation)
            .uvarint(outcome)
            .uvarint(nonNegative(durationUs))
            .uvarint(nonNegative(bytes))
        var flags = if (mainThread) BinaryLogWriter.FLAG_THREAD_MAIN else 0L
        if (bytesKnown) flags = flags or Jhlog.FLAG_IO_BYTES_KNOWN
        sink.emitSemantic(Jhlog.TYPE_IO, flags, payload, sink.producerContext())
    }

    fun worker(
        workerId: Long,
        workerName: String?,
        instanceId: Long,
        stage: Long,
        outcome: Long,
        durationMs: Long,
        runAttempt: Long,
        generation: Long,
        stopReason: Long,
        flags: Long,
    ) {
        require(instanceId != 0L)
        require(stage in Jhlog.WORKER_STAGE_ENQUEUED..Jhlog.WORKER_STAGE_FINISHED)
        require(stage == Jhlog.WORKER_STAGE_ENQUEUED || workerId != 0L)
        require(outcome in Jhlog.WORKER_OUTCOME_UNKNOWN..Jhlog.WORKER_OUTCOME_CANCELLED)
        val finished = stage == Jhlog.WORKER_STAGE_FINISHED
        require(finished || outcome == Jhlog.WORKER_OUTCOME_UNKNOWN)
        require(finished || durationMs == 0L)
        require(!finished || outcome != Jhlog.WORKER_OUTCOME_UNKNOWN)
        require(runAttempt in 0L..MAX_UINT32)
        require(generation in 0L..MAX_UINT32)
        require(stopReason in 0L..MAX_UINT32)
        val stopReasonKnown = flags and Jhlog.FLAG_WORKER_STOP_REASON_KNOWN != 0L
        require(finished || !stopReasonKnown)
        require(stopReasonKnown || stopReason == 0L)
        val workerAlias = if (workerId != 0L) sink.defineStableSymbol(workerId, workerName) else 0L
        val payload = sink.payload()
        if (workerId != 0L) payload.stableSymbolAlias(workerAlias) else payload.symbolRef(0L)
        payload
            .uvarint(instanceId)
            .uvarint(stage)
            .uvarint(outcome)
            .uvarint(nonNegative(durationMs))
            .uvarint(runAttempt)
            .uvarint(generation)
            .uvarint(stopReason)
        sink.emitSemantic(Jhlog.TYPE_WORKER, knownFlags(flags), payload, sink.producerContext())
    }

    fun operation(
        name: String,
        operationId: Long,
        parentId: Long,
        phase: Long,
        kind: Long,
        outcome: Long,
        durationUs: Long,
        budgetUs: Long,
        attributes: JankHunterOperationAttributes,
    ) {
        require(operationId > 0L) { "Operation ID must be positive" }
        require(parentId >= 0L && parentId != operationId) { "Operation parent must be different" }
        require(kind in 1L..5L) { "Unsupported operation kind $kind" }
        when (phase) {
            Jhlog.OPERATION_PHASE_STARTED -> require(outcome == 0L && durationUs == 0L) {
                "Started operation cannot have outcome or duration"
            }
            Jhlog.OPERATION_PHASE_FINISHED -> require(outcome in 1L..4L) {
                "Finished operation requires a supported outcome"
            }
            else -> error("Unsupported operation phase $phase")
        }
        require(attributes.size <= Jhlog.OPERATION_MAX_ATTRIBUTES)
        val payload = sink.payload()
            .symbolRef(sink.symbolId(BinaryLogWriter.DICT_OPERATION, name))
            .uvarint(operationId)
            .uvarint(parentId)
            .uvarint(phase)
            .uvarint(kind)
            .uvarint(outcome)
            .uvarint(nonNegative(durationUs))
            .uvarint(nonNegative(budgetUs))
            .uvarint(attributes.size.toLong())
        for (index in 0 until attributes.size) {
            payload
                .symbolRef(sink.symbolId(BinaryLogWriter.DICT_ATTRIBUTE_KEY, attributes.key(index)))
                .symbolRef(sink.symbolId(BinaryLogWriter.DICT_ATTRIBUTE_VALUE, attributes.value(index)))
        }
        sink.emitSemantic(Jhlog.TYPE_OPERATION, 0L, payload, sink.producerContext())
    }

    private fun knownFlags(flags: Long): Long = flags and FLAG_KNOWN_MASK

    private fun nonNegative(value: Long): Long = value.coerceAtLeast(0L)

    private companion object {
        const val MAX_UINT32 = 0xffff_ffffL
        const val FLAG_KNOWN_MASK: Long =
            ((1L shl 14) - 1L) or BinaryLogWriter.FLAG_HTTP_SLOW or BinaryLogWriter.FLAG_UI_PROBLEM or
                BinaryLogWriter.FLAG_HTTP_CLASSIFIED or BinaryLogWriter.FLAG_UI_CLASSIFIED or
                Jhlog.FLAG_WORKER_PERIODIC or Jhlog.FLAG_WORKER_STOP_REASON_KNOWN or Jhlog.FLAG_IO_BYTES_KNOWN
    }
}
