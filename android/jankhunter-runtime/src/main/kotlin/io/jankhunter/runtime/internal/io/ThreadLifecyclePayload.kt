package io.jankhunter.runtime.internal.io

internal enum class ThreadLifecycleStage(val wire: Long) {
    RUN_OBSERVED(1), START_RETURNED(2), FACTORY_RETURNED(3),
}

/** Contains no Thread, Runnable, factory or other application object. */
internal data class ThreadLifecycleObservation(
    val stage: ThreadLifecycleStage,
    val javaId: Long,
    val targetTid: Long,
    val observedNanos: Long,
    val daemon: Boolean,
    val name: String,
    val source: String,
    val factory: String,
)

internal fun encodeThreadLifecycle(payload: BinaryPayload, observation: ThreadLifecycleObservation) {
    val point = observation
    require(point.javaId >= 0 && point.observedNanos >= 0)
    require((point.stage == ThreadLifecycleStage.RUN_OBSERVED && point.targetTid > 0) ||
        (point.stage != ThreadLifecycleStage.RUN_OBSERVED && point.targetTid == 0L))
    payload.uvarint(1).uvarint(point.stage.wire).uvarint(point.javaId).uvarint(point.targetTid)
        .uvarint(point.observedNanos).uvarint(if (point.daemon) 1 else 0)
        .boundedString(point.name, 1024).boundedString(point.source, 1024).boundedString(point.factory, 1024)
}

internal class PendingThreadLifecycle(
    context: LogEventContext?,
    private val observation: ThreadLifecycleObservation,
) : PendingLogEvent(Jhlog.TYPE_THREAD_LIFECYCLE, context) {
    override fun writePayload(writer: BinaryLogWriter) {
        val context = writer.producerContext()
        val payload = writer.payload()
        encodeThreadLifecycle(payload, observation)
        writer.emitSemantic(Jhlog.TYPE_THREAD_LIFECYCLE, 0, payload, context)
    }
}
