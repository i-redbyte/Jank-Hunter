package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterWorkDataField

/** A direct setProgress request; WorkInfo remains the source for persisted state. */
internal data class WorkerProgressObservation(
    val stage: Int,
    val instanceId: Long,
    val runAttempt: Int,
    val generation: Int,
    val observedNanos: Long,
    val omittedUpdates: Int,
    val fields: List<JankHunterWorkDataField>,
    val omittedKeys: Int,
    val truncatedFields: Int,
)

internal fun encodeWorkerProgress(payload: BinaryPayload, value: WorkerProgressObservation) {
    require(value.stage in 1..2 && value.instanceId != 0L && value.runAttempt >= 0 && value.generation >= 0 &&
        value.observedNanos >= 0 && value.omittedUpdates >= 0 && value.omittedKeys >= 0 && value.truncatedFields >= 0 &&
        value.fields.size <= 8)
    require(value.stage != 2 || (value.fields.isEmpty() && value.omittedKeys == 0 && value.truncatedFields == 0))
    require(value.stage != 1 || value.omittedUpdates == 0)
    payload.uvarint(1).uvarint(value.stage.toLong()).uvarint(value.instanceId)
        .uvarint(value.runAttempt.toLong()).uvarint(value.generation.toLong()).uvarint(value.observedNanos)
        .uvarint(value.omittedUpdates.toLong()).uvarint(value.omittedKeys.toLong())
        .uvarint(value.truncatedFields.toLong())
    payload.workData(value.fields)
}

internal class PendingWorkerProgress(context: LogEventContext?, private val value: WorkerProgressObservation) :
    PendingLogEvent(Jhlog.TYPE_WORKER_PROGRESS, context) {
    override fun writePayload(writer: BinaryLogWriter) {
        val payload = writer.payload()
        encodeWorkerProgress(payload, value)
        writer.emitSemantic(Jhlog.TYPE_WORKER_PROGRESS, 0, payload, writer.producerContext())
    }
}
