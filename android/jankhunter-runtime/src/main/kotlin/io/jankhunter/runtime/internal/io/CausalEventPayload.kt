package io.jankhunter.runtime.internal.io

internal object CausalKind {
    const val NONE = 0
    const val THREAD = 1
    const val EXECUTOR_TASK = 2
    const val HANDLER_POST = 3
    const val COROUTINE = 4
    const val WORKER_ATTEMPT = 5
    const val RESOURCE = 6
    const val OPERATION = 7
    const val SERIES = 8
    const val EXECUTOR = 9
    const val DIRECT_EXECUTION = 10
}

internal object CausalPhase {
    object Thread {
        const val CONSTRUCTED = 1
        const val FACTORY_RETURNED = 2
        const val START_INVOKED = 3
        const val START_RETURNED = 4
        const val START_THREW = 5
        const val ROOT_RUN_STARTED = 6
        const val ROOT_RUN_ENDED = 7
        const val INTERRUPT_REQUESTED = 8
        const val INTERRUPT_RETURNED = 9
        const val INTERRUPT_THREW = 10
        const val END_OBSERVED = 11
    }

    object ExecutorTask {
        const val SUBMITTED = 1
        const val HANDOFF_RETURNED = 2
        const val HANDOFF_THREW = 3
        const val RUN_STARTED = 4
        const val RUN_ENDED = 5
        const val CANCEL_REQUESTED = 6
        const val CANCEL_RETURNED = 7
        const val TERMINAL_OBSERVED = 8
    }

    object HandlerPost {
        const val POST_INVOKED = 1
        const val POST_RETURNED = 2
        const val DISPATCH_STARTED = 3
        const val DISPATCH_ENDED = 4
        const val REMOVE_REQUESTED = 5
        const val REMOVE_RETURNED = 6
        const val POST_THREW = 7
        const val REMOVE_THREW = 8
    }

    object Coroutine {
        const val CREATED_OBSERVED = 1
        const val RESUME_STARTED = 2
        const val SUSPENDED = 3
        const val CANCEL_REQUESTED = 4
        const val TERMINAL_OBSERVED = 5
        const val STATE_MACHINE_OBSERVED = 6
    }

    object WorkerAttempt {
        const val BOUND = 1
        const val STARTED = 2
        const val RETRY_RETURNED = 3
        const val STOP_REQUESTED = 4
        const val TERMINAL_OBSERVED = 5
        const val IMPLEMENTATION_OBSERVED = 6
    }

    object Resource {
        const val ACQUIRE_ATTEMPT = 1
        const val ACQUIRED = 2
        const val ACQUIRE_FAILED = 3
        const val RELEASED = 4
        const val WAIT_OBSERVED = 5
        const val SCOPE_TRANSFERRED = 6
        const val USE_ENTERED = 7
    }

    object Executor {
        const val CONSTRUCTED = 1
        const val FACTORY_RETURNED = 2
        const val SHUTDOWN_REQUESTED = 3
        const val SHUTDOWN_RETURNED = 4
        const val TERMINATED_OBSERVED = 5
        const val FIRST_OBSERVED = 6
        const val SHUTDOWN_THREW = 7
    }

    object Series {
        const val DECLARED = 1
        const val TERMINAL_OBSERVED = 2
        const val CANCEL_REQUESTED = 3
        const val CANCEL_RETURNED = 4
    }

    object DirectExecution {
        const val RUN_STARTED = 1
        const val RUN_ENDED = 2
    }
}

/** One bounded, immutable semantic observation; application objects never enter the writer queue. */
internal data class CausalEventObservation(
    val kind: Int,
    val phase: Int,
    val id: Long,
    val parentKind: Int = 0,
    val parentId: Long = 0L,
    val schedulerKind: Int = 0,
    val schedulerId: Long = 0L,
    val actorKind: Int = 0,
    val actorId: Long = 0L,
    val observedNanos: Long,
    val sourceOrigin: Int = 0,
    val targetJavaId: Long = 0L,
    val targetTid: Long = 0L,
    val sourceSiteId: Long = 0L,
    val sourceMethod: String? = null,
    val sourceLine: Int = 0,
    val appCallerSiteId: Long = 0L,
    val appCallerMethod: String? = null,
    val appCallerLine: Int = 0,
    val cpuNanos: Long = -1L,
    val durationNanos: Long = -1L,
    val readBytes: Long = -1L,
    val writtenBytes: Long = -1L,
    val scheduledForNanos: Long = -1L,
    val queueEnteredNanos: Long = -1L,
    val occurrence: Long = -1L,
    val workerInstanceId: Long = 0L,
    val runAttempt: Int = -1,
    val generation: Int = -1,
    val resourceKind: Int = 0,
    val operationKind: Int = 0,
    val callResult: Int = 0,
    val terminalOutcome: Int = 0,
    val leaseId: Long = 0L,
    val coroutineToken: Long = 0L,
    val coroutineGeneration: Long = 0L,
    val leaseScopeKind: Int = 0,
    val leaseScopeId: Long = 0L,
    val boundToScope: Boolean = false,
    val leaseScopeKnown: Boolean = false,
    val requestedPeriodNanos: Long = -1L,
    val scheduleCadence: Int = 0,
    val requestedDelayNanos: Long = -1L,
)

internal fun encodeCausalEvent(payload: BinaryPayload, event: CausalEventObservation) {
    require(event.kind in 1..CausalKind.DIRECT_EXECUTION && event.phase in 1..maxPhase(event.kind))
    require(event.id > 0L && event.observedNanos >= 0L)
    require(validRef(event.parentKind, event.parentId) && validRef(event.schedulerKind, event.schedulerId))
    require(validRef(event.actorKind, event.actorId))
    require(event.kind != CausalKind.DIRECT_EXECUTION || event.schedulerId == 0L)
    require(event.sourceOrigin in 0..2 && (event.sourceMethod != null || event.sourceOrigin == 0))
    require(event.sourceSiteId >= 0L && event.appCallerSiteId >= 0L)
    require(event.sourceLine >= 0 && event.appCallerLine >= 0)
    require(event.targetJavaId >= 0L && event.targetTid >= 0L)
    require(event.cpuNanos >= -1L && event.durationNanos >= -1L && event.readBytes >= -1L)
    require(event.cpuNanos < 0L || event.targetTid > 0L)
    require(event.writtenBytes >= -1L && event.scheduledForNanos >= -1L && event.queueEnteredNanos >= -1L)
    require(event.occurrence == -1L || event.occurrence > 0L)
    require(event.requestedPeriodNanos == -1L || event.requestedPeriodNanos > 0L)
    require(event.requestedDelayNanos >= -1L)
    require(event.requestedDelayNanos < 0L || event.kind == CausalKind.HANDLER_POST &&
        event.phase == CausalPhase.HandlerPost.POST_INVOKED)
    require(event.scheduleCadence in 0..3)
    require(event.scheduleCadence < 2 || event.requestedPeriodNanos > 0L)
    require(event.callResult in 0..3 && event.terminalOutcome in 0..5)
    require(event.resourceKind in 0..4 && event.operationKind in 0..10)
    require(event.resourceKind == 0 || event.kind == CausalKind.RESOURCE)
    require(event.operationKind == 0 || event.kind == CausalKind.OPERATION)
    require(event.leaseId == 0L || event.kind == CausalKind.OPERATION)
    require(event.coroutineToken == 0L || event.kind == CausalKind.COROUTINE)
    require(event.kind != CausalKind.RESOURCE || event.resourceKind > 0)
    require(event.kind != CausalKind.OPERATION || event.operationKind > 0 && event.durationNanos >= 0L)
    require(event.kind != CausalKind.WORKER_ATTEMPT || event.phase != CausalPhase.WorkerAttempt.BOUND ||
        event.workerInstanceId != 0L)
    require(event.scheduleCadence == 0 || event.kind == CausalKind.SERIES || event.kind == CausalKind.EXECUTOR_TASK)
    require(event.requestedPeriodNanos < 0L || event.kind == CausalKind.SERIES || event.kind == CausalKind.EXECUTOR_TASK)
    require(event.leaseId >= 0L && event.coroutineToken >= 0L && event.coroutineGeneration >= 0L)
    require(event.workerInstanceId == 0L || event.runAttempt >= 0 && event.generation >= 0)
    require(validRef(event.leaseScopeKind, event.leaseScopeId))
    require(!event.boundToScope || event.leaseScopeId > 0L)
    require(!event.leaseScopeKnown || event.leaseScopeId > 0L ||
        event.kind == CausalKind.RESOURCE && event.phase == CausalPhase.Resource.SCOPE_TRANSFERRED && !event.boundToScope)
    require(event.sourceMethod == null || event.sourceMethod.isNotBlank())
    require(event.appCallerMethod == null || event.appCallerMethod.isNotBlank())

    var presence = 0L
    if (event.targetJavaId > 0L) presence = presence or bit(0)
    if (event.targetTid > 0L) presence = presence or bit(1)
    if (event.sourceMethod != null) presence = presence or bit(2)
    if (event.appCallerMethod != null) presence = presence or bit(3)
    if (event.cpuNanos >= 0L) presence = presence or bit(4)
    if (event.durationNanos >= 0L) presence = presence or bit(5)
    if (event.readBytes >= 0L) presence = presence or bit(6)
    if (event.writtenBytes >= 0L) presence = presence or bit(7)
    if (event.scheduledForNanos >= 0L) presence = presence or bit(8)
    if (event.queueEnteredNanos >= 0L) presence = presence or bit(9)
    if (event.occurrence > 0L) presence = presence or bit(10)
    if (event.workerInstanceId != 0L) presence = presence or bit(11)
    if (event.resourceKind > 0) presence = presence or bit(12)
    if (event.operationKind > 0) presence = presence or bit(13)
    if (event.callResult > 0) presence = presence or bit(14)
    if (event.terminalOutcome > 0) presence = presence or bit(15)
    if (event.leaseId > 0L) presence = presence or bit(16)
    if (event.coroutineToken > 0L) presence = presence or bit(17)
    if (event.leaseScopeKnown || event.leaseScopeKind > 0) presence = presence or bit(18)
    if (event.requestedPeriodNanos > 0L) presence = presence or bit(19)
    if (event.scheduleCadence > 0) presence = presence or bit(20)
    if (event.requestedDelayNanos >= 0L) presence = presence or bit(21)

    payload.uvarint(1).uvarint(event.kind.toLong()).uvarint(event.phase.toLong()).uvarint(event.id)
        .uvarint(event.parentKind.toLong()).uvarint(event.parentId)
        .uvarint(event.schedulerKind.toLong()).uvarint(event.schedulerId)
        .uvarint(event.actorKind.toLong()).uvarint(event.actorId)
        .uvarint(event.observedNanos).uvarint(event.sourceOrigin.toLong()).uvarint(presence)
    if (presence has 0) payload.uvarint(event.targetJavaId)
    if (presence has 1) payload.uvarint(event.targetTid)
    if (presence has 2) payload.uvarint(event.sourceSiteId)
        .boundedString(checkNotNull(event.sourceMethod), MAX_SOURCE_BYTES).uvarint(event.sourceLine.toLong())
    if (presence has 3) payload.uvarint(event.appCallerSiteId)
        .boundedString(checkNotNull(event.appCallerMethod), MAX_SOURCE_BYTES).uvarint(event.appCallerLine.toLong())
    if (presence has 4) payload.uvarint(event.cpuNanos)
    if (presence has 5) payload.uvarint(event.durationNanos)
    if (presence has 6) payload.uvarint(event.readBytes)
    if (presence has 7) payload.uvarint(event.writtenBytes)
    if (presence has 8) payload.uvarint(event.scheduledForNanos)
    if (presence has 9) payload.uvarint(event.queueEnteredNanos)
    if (presence has 10) payload.uvarint(event.occurrence)
    if (presence has 11) payload.uvarint(event.workerInstanceId)
        .uvarint(event.runAttempt.toLong()).uvarint(event.generation.toLong())
    if (presence has 12) payload.uvarint(event.resourceKind.toLong())
    if (presence has 13) payload.uvarint(event.operationKind.toLong())
    if (presence has 14) payload.uvarint(event.callResult.toLong())
    if (presence has 15) payload.uvarint(event.terminalOutcome.toLong())
    if (presence has 16) payload.uvarint(event.leaseId)
    if (presence has 17) payload.uvarint(event.coroutineToken).uvarint(event.coroutineGeneration)
    if (presence has 18) payload.uvarint(event.leaseScopeKind.toLong()).uvarint(event.leaseScopeId)
        .uvarint(if (event.boundToScope) 1L else 0L)
    if (presence has 19) payload.uvarint(event.requestedPeriodNanos)
    if (presence has 20) payload.uvarint(event.scheduleCadence.toLong())
    if (presence has 21) payload.uvarint(event.requestedDelayNanos)
}

internal class PendingCausalEvent(
    context: LogEventContext?,
    private val observation: CausalEventObservation,
) : PendingLogEvent(Jhlog.TYPE_CAUSAL_EVENT, context) {
    override fun writePayload(writer: BinaryLogWriter) {
        val payload = writer.payload()
        encodeCausalEvent(payload, observation)
        writer.emitSemantic(Jhlog.TYPE_CAUSAL_EVENT, 0, payload, writer.producerContext())
    }
}

private fun validRef(kind: Int, id: Long): Boolean =
    (kind == 0 && id == 0L) || (kind in 1..CausalKind.DIRECT_EXECUTION && id > 0L)

private fun maxPhase(kind: Int): Int = when (kind) {
    CausalKind.THREAD -> 11
    CausalKind.EXECUTOR_TASK -> 8
    CausalKind.HANDLER_POST -> 8
    CausalKind.COROUTINE -> 6
    CausalKind.WORKER_ATTEMPT -> 6
    CausalKind.RESOURCE -> 7
    CausalKind.OPERATION -> 1
    CausalKind.SERIES -> 4
    CausalKind.EXECUTOR -> 7
    CausalKind.DIRECT_EXECUTION -> 2
    else -> 0
}

private fun bit(index: Int): Long = 1L shl index

private infix fun Long.has(index: Int): Boolean = this and bit(index) != 0L

private const val MAX_SOURCE_BYTES = 512
