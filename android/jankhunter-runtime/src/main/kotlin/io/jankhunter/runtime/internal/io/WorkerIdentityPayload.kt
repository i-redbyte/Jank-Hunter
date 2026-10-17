package io.jankhunter.runtime.internal.io

/** Only primitive identity words are retained; formatting and application objects are unnecessary. */
internal class PendingWorkerIdentity(
    private val instanceId: Long,
    private val most: Long,
    private val least: Long,
) : PendingLogEvent(Jhlog.TYPE_WORKER_IDENTITY, null) {
    override fun writePayload(writer: BinaryLogWriter) {
        writer.emitSemantic(Jhlog.TYPE_WORKER_IDENTITY, 0,
            writer.payload().uvarint(1).uvarint(instanceId).uvarint(most).uvarint(least), null)
    }
}
