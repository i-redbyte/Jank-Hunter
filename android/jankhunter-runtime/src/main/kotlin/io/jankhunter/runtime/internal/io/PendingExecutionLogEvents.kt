package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterOperationAttributes

internal class PendingIoEvent(
    producerContext: LogEventContext?,
    private val operation: Long,
    private val durationUs: Long,
    private val bytes: Long,
    private val mainThread: Boolean,
    private val sourceId: Long,
    private val sourceName: String?,
    private val outcome: Long,
    private val bytesKnown: Boolean,
) : PendingLogEvent(Jhlog.TYPE_IO, producerContext) {
    override fun writePayload(writer: BinaryLogWriter) {
        writer.io(operation, durationUs, bytes, mainThread, sourceId, sourceName, outcome, bytesKnown)
    }
}

internal class PendingWorkerEvent(
    producerContext: LogEventContext?,
    private val workerId: Long,
    private val workerName: String?,
    private val instanceId: Long,
    private val stage: Long,
    private val outcome: Long,
    private val durationMs: Long,
    private val runAttempt: Long,
    private val generation: Long,
    private val stopReason: Long,
    private val flags: Long,
) : PendingLogEvent(Jhlog.TYPE_WORKER, producerContext) {
    override fun writePayload(writer: BinaryLogWriter) {
        writer.worker(
            workerId,
            workerName,
            instanceId,
            stage,
            outcome,
            durationMs,
            runAttempt,
            generation,
            stopReason,
            flags,
        )
    }
}

internal class PendingAndroidComponentEvent(
    producerContext: LogEventContext?,
    private val componentId: Long,
    private val componentName: String?,
    private val action: String?,
    private val instanceId: Long,
    private val flowId: Long,
    private val kind: Long,
    private val stage: Long,
    private val outcome: Long,
    private val durationUs: Long,
    private val componentFlags: Long,
) : PendingLogEvent(Jhlog.TYPE_ANDROID_COMPONENT, producerContext) {
    override fun writePayload(writer: BinaryLogWriter) {
        writer.androidComponent(
            componentId,
            componentName,
            action,
            instanceId,
            flowId,
            kind,
            stage,
            outcome,
            durationUs,
            componentFlags,
        )
    }
}

internal class PendingBinderTransactionEvent(
    producerContext: LogEventContext?,
    private val descriptor: String?,
    private val method: String?,
    private val callId: Long,
    private val direction: Long,
    private val transactionCode: Long,
    private val outcome: Long,
    private val failureKind: Long,
    private val durationUs: Long,
    private val binderFlags: Long,
    private val mainThread: Boolean,
) : PendingLogEvent(Jhlog.TYPE_BINDER_TRANSACTION, producerContext) {
    override fun writePayload(writer: BinaryLogWriter) {
        writer.binderTransaction(
            descriptor,
            method,
            callId,
            direction,
            transactionCode,
            outcome,
            failureKind,
            durationUs,
            binderFlags,
            mainThread,
        )
    }
}

internal class PendingOperationEvent(
    producerContext: LogEventContext?,
    private val name: String,
    private val operationId: Long,
    private val parentId: Long,
    private val phase: Long,
    private val kind: Long,
    private val outcome: Long,
    private val durationUs: Long,
    private val budgetUs: Long,
    private val attributes: JankHunterOperationAttributes,
) : PendingLogEvent(Jhlog.TYPE_OPERATION, producerContext) {
    override fun writePayload(writer: BinaryLogWriter) {
        writer.operation(
            name,
            operationId,
            parentId,
            phase,
            kind,
            outcome,
            durationUs,
            budgetUs,
            attributes,
        )
    }
}
