package io.jankhunter.runtime.internal.io

/** One observed resume segment; suspension releases the executing thread. */
internal data class CoroutineSegmentObservation(
    val token: Long,
    val generation: Long,
    val entryJavaThreadId: Long,
    val exitJavaThreadId: Long,
    val entryTid: Long,
    val exitTid: Long,
    val startedElapsedMs: Long,
    val endedElapsedMs: Long,
    val state: Int,
    val owner: String,
)

internal fun encodeCoroutineSegment(payload: BinaryPayload, value: CoroutineSegmentObservation) {
    val runningObservation = value.state == 4
    require(value.token > 0 && value.generation >= 0 && value.entryJavaThreadId > 0 &&
        value.entryTid > 0 && value.startedElapsedMs >= 0 && value.endedElapsedMs >= value.startedElapsedMs &&
        value.state in 0..4 &&
        (if (runningObservation) value.exitJavaThreadId == 0L && value.exitTid == 0L
        else value.exitJavaThreadId > 0 && value.exitTid > 0) &&
        value.owner.isNotEmpty() && value.owner.toByteArray(Charsets.UTF_8).size <= 512)
    payload.uvarint(if (runningObservation) 3 else 2).uvarint(value.token).uvarint(value.generation)
        .uvarint(value.entryJavaThreadId).uvarint(value.exitJavaThreadId)
        .uvarint(value.startedElapsedMs).uvarint(value.endedElapsedMs)
        .uvarint(value.state.toLong()).uvarint(value.entryTid).uvarint(value.exitTid)
        .boundedString(value.owner, 512)
}

internal class PendingCoroutineSegment(context: LogEventContext?, private val value: CoroutineSegmentObservation) :
    PendingLogEvent(Jhlog.TYPE_COROUTINE_SEGMENT, context) {
    override fun writePayload(writer: BinaryLogWriter) {
        val payload = writer.payload()
        encodeCoroutineSegment(payload, value)
        writer.emitSemantic(Jhlog.TYPE_COROUTINE_SEGMENT, 0, payload, writer.producerContext())
    }
}
