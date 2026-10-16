package io.jankhunter.runtime.internal.io

/** A declared dependency from a public then() call, without WorkManager object retention. */
internal data class WorkChainEdgeObservation(
    val parentMost: Long,
    val parentLeast: Long,
    val childMost: Long,
    val childLeast: Long,
    val observedNanos: Long,
    val callerSource: String,
    val sourceTruncated: Boolean = false,
)

internal fun encodeWorkChainEdge(payload: BinaryPayload, value: WorkChainEdgeObservation) {
    require(value.observedNanos >= 0 && value.callerSource.isNotEmpty())
    payload.uvarint(1).uvarint(value.parentMost).uvarint(value.parentLeast)
        .uvarint(value.childMost).uvarint(value.childLeast).uvarint(value.observedNanos)
        .uvarint(if (value.sourceTruncated) 1 else 0)
        .boundedString(value.callerSource, 512)
}

internal class PendingWorkChainEdge(context: LogEventContext?, private val value: WorkChainEdgeObservation) :
    PendingLogEvent(Jhlog.TYPE_WORK_CHAIN_EDGE, context) {
    override fun writePayload(writer: BinaryLogWriter) {
        val payload = writer.payload()
        encodeWorkChainEdge(payload, value)
        writer.emitSemantic(Jhlog.TYPE_WORK_CHAIN_EDGE, 0, payload, writer.producerContext())
    }
}
