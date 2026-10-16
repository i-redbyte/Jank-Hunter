package io.jankhunter.runtime.internal.io

internal enum class WorkSubmissionPolicy(val wire: Long) {
    NONE(0), KEEP(1), REPLACE(2), APPEND(3), APPEND_OR_REPLACE(4), UPDATE(5), CANCEL_AND_REENQUEUE(6),
}

/** Primitive call evidence; never retains a request, Operation, Worker or Data. */
internal data class WorkSubmissionObservation(
    val most: Long,
    val least: Long,
    val observedNanos: Long,
    val periodic: Boolean,
    val uniqueName: String?,
    val policy: WorkSubmissionPolicy,
    val omittedRequests: Int,
    val nameTruncated: Boolean = false,
    val callerSource: String? = null,
    val sourceTruncated: Boolean = false,
    val backoffPolicy: Int = 0,
    val requestedBackoffMS: Long = 0,
    val quotaPolicy: Int = 0,
    val requestedPeriodMS: Long? = null,
    val requestedFlexMS: Long? = null,
    val requestedInitialDelayMS: Long? = null,
)

internal fun encodeWorkSubmission(payload: BinaryPayload, value: WorkSubmissionObservation) {
    require(value.observedNanos >= 0 && value.omittedRequests >= 0)
    require(!value.nameTruncated || value.uniqueName != null)
    require(!value.sourceTruncated || !value.callerSource.isNullOrEmpty())
    require(value.callerSource == null || value.callerSource.isNotEmpty())
    require(value.backoffPolicy in 0..2 && value.quotaPolicy in 0..2 && value.requestedBackoffMS >= 0 &&
        (value.backoffPolicy != 0 || value.requestedBackoffMS == 0L))
    val schedule = listOf(value.requestedPeriodMS, value.requestedFlexMS, value.requestedInitialDelayMS)
    require(schedule.all { it == null || it >= 0 })
    require(value.periodic || value.requestedPeriodMS == null && value.requestedFlexMS == null)
    require(value.requestedFlexMS == null || value.requestedPeriodMS != null)
    val hasSchedule = schedule.any { it != null }
    val hasHints = value.backoffPolicy != 0 || value.quotaPolicy != 0
    require(!(hasHints || hasSchedule) || value.callerSource != null)
    val policy = value.policy
    require(if (value.uniqueName == null) policy == WorkSubmissionPolicy.NONE else if (value.periodic) {
        policy == WorkSubmissionPolicy.KEEP || policy == WorkSubmissionPolicy.REPLACE ||
            policy == WorkSubmissionPolicy.UPDATE || policy == WorkSubmissionPolicy.CANCEL_AND_REENQUEUE
    } else policy == WorkSubmissionPolicy.KEEP || policy == WorkSubmissionPolicy.REPLACE ||
        policy == WorkSubmissionPolicy.APPEND || policy == WorkSubmissionPolicy.APPEND_OR_REPLACE)
    val flags = (if (value.uniqueName != null) SUBMISSION_UNIQUE else 0L) or
        (if (value.periodic) SUBMISSION_PERIODIC else 0L) or
        (if (value.nameTruncated) SUBMISSION_NAME_TRUNCATED else 0L) or
        (if (value.sourceTruncated) SUBMISSION_SOURCE_TRUNCATED else 0L)
    payload.uvarint(if (hasSchedule) 4 else if (hasHints) 3 else if (value.callerSource == null) 1 else 2)
        .uvarint(value.most).uvarint(value.least).uvarint(value.observedNanos)
        .uvarint(flags).uvarint(value.omittedRequests.toLong()).uvarint(policy.wire)
    value.uniqueName?.let { payload.boundedString(it, 512) }
    value.callerSource?.let { payload.boundedString(it, 512) }
    if (hasHints || hasSchedule) payload.uvarint(value.backoffPolicy.toLong()).uvarint(value.requestedBackoffMS)
        .uvarint(value.quotaPolicy.toLong())
    if (hasSchedule) {
        var mask = 0L
        schedule.forEachIndexed { index, field -> if (field != null) mask = mask or (1L shl index) }
        payload.uvarint(mask)
        schedule.forEach { if (it != null) payload.uvarint(it) }
    }
}

internal class PendingWorkSubmission(context: LogEventContext?, private val value: WorkSubmissionObservation) :
    PendingLogEvent(Jhlog.TYPE_WORK_SUBMISSION, context) {
    override fun writePayload(writer: BinaryLogWriter) {
        val context = writer.producerContext()
        val payload = writer.payload()
        encodeWorkSubmission(payload, value)
        writer.emitSemantic(Jhlog.TYPE_WORK_SUBMISSION, 0, payload, context)
    }
}

/** Same metadata fields as submission, but graph construction is a distinct event. */
internal class PendingWorkChainRoot(context: LogEventContext?, private val value: WorkSubmissionObservation) :
    PendingLogEvent(Jhlog.TYPE_WORK_CHAIN_ROOT, context) {
    override fun writePayload(writer: BinaryLogWriter) {
        val payload = writer.payload()
        encodeWorkSubmission(payload, value)
        writer.emitSemantic(Jhlog.TYPE_WORK_CHAIN_ROOT, 0, payload, writer.producerContext())
    }
}

private const val SUBMISSION_UNIQUE = 1L
private const val SUBMISSION_PERIODIC = 2L

private const val SUBMISSION_NAME_TRUNCATED = 4L

private const val SUBMISSION_SOURCE_TRUNCATED = 8L
