package io.jankhunter.runtime.internal.io

/** Exception evidence from one attempt. No exception message or field values are recorded. */
internal data class WorkerFailureObservation(
    val instanceId: Long,
    val runAttempt: Int,
    val generation: Int,
    val observedNanos: Long,
    val exceptionClass: String,
    val stack: String,
    val stackTruncated: Boolean,
)

internal fun encodeWorkerFailure(payload: BinaryPayload, value: WorkerFailureObservation) {
    require(value.instanceId != 0L && value.runAttempt >= 0 && value.generation >= 0 &&
        value.observedNanos >= 0 && value.exceptionClass.isNotEmpty())
    require(value.exceptionClass.toByteArray(Charsets.UTF_8).size <= 128 &&
        value.stack.toByteArray(Charsets.UTF_8).size <= 1024)
    payload.uvarint(1).uvarint(value.instanceId).uvarint(value.runAttempt.toLong())
        .uvarint(value.generation.toLong()).uvarint(value.observedNanos)
        .uvarint(if (value.stackTruncated) 1 else 0)
        .boundedString(value.exceptionClass, 128).boundedString(value.stack, 1024)
}

internal class PendingWorkerFailure(context: LogEventContext?, private val value: WorkerFailureObservation) :
    PendingLogEvent(Jhlog.TYPE_WORKER_FAILURE, context) {
    override fun writePayload(writer: BinaryLogWriter) {
        val payload = writer.payload()
        encodeWorkerFailure(payload, value)
        writer.emitSemantic(Jhlog.TYPE_WORKER_FAILURE, 0, payload, writer.producerContext())
    }
}
