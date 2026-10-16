package io.jankhunter.runtime

import android.os.Debug
import android.os.SystemClock
import io.jankhunter.runtime.internal.io.CausalEventObservation
import io.jankhunter.runtime.internal.io.CausalKind
import io.jankhunter.runtime.internal.io.CausalPhase
import io.jankhunter.runtime.internal.io.QualityCounterId
import java.lang.ref.WeakReference
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.locks.ReentrantLock
import kotlin.coroutines.jvm.internal.CoroutineStackFrame

/** No strong application references survive a suspension. */
internal class RuntimeCausalCoroutineLaunch(
    val epochId: Long,
    val id: Long,
    val siteId: Long,
    val source: String,
    val line: Int,
    val origin: Int,
) {
    val lock = ReentrantLock()
    val invoked = AtomicBoolean()
    val terminal = AtomicBoolean()
    var job: WeakReference<Any>? = null
}

/** Connects the exact builder block invocation to its first instrumented state machine. */
internal class RuntimeCausalCoroutineTelemetry(
    private val access: RuntimeTelemetryAccess,
    private val concurrency: RuntimeConcurrencyTelemetry,
) {
    private val pending = ThreadLocal<Invocation>()
    private val running = ThreadLocal<Segment>()

    fun isEnabled(): Boolean = epoch() != null

    fun capacityRejected() {
        epoch()?.writer?.recordQuality(QualityCounterId.CAUSAL_REGISTRY_CAPACITY)
    }

    fun create(
        siteId: Long, source: String, line: Int, origin: Int,
        initialPhase: Int = CausalPhase.Coroutine.CREATED_OBSERVED,
    ): RuntimeCausalCoroutineLaunch? {
        val epoch = epoch() ?: return null
        val registry = epoch.causalRegistry ?: return null
        val id = registry.newId()
        if (id <= 0L) return null
        val launch = RuntimeCausalCoroutineLaunch(epoch.id, id, siteId.coerceAtLeast(0L), source,
            line.coerceAtLeast(0), origin.coerceIn(0, 2))
        if (!registry.bind(CausalKind.COROUTINE, launch, id)) {
            epoch.writer.recordQuality(QualityCounterId.CAUSAL_REGISTRY_CAPACITY)
            return null
        }
        val parent = concurrency.captureCausalContext()
        epoch.writer.causalEvent(CausalEventObservation(
            kind = CausalKind.COROUTINE, phase = initialPhase, id = id,
            parentKind = parent?.entityKind ?: 0, parentId = parent?.entityId ?: 0L,
            actorKind = parent?.entityKind ?: 0, actorId = parent?.entityId ?: 0L,
            observedNanos = now(), sourceSiteId = launch.siteId, sourceMethod = launch.source,
            sourceLine = launch.line, sourceOrigin = launch.origin,
        ))
        return launch
    }

    fun <T> invoke(launch: RuntimeCausalCoroutineLaunch?, block: (RuntimeCausalCoroutineLaunch?) -> T): T {
        if (launch == null || epoch()?.id != launch.epochId) return block(null)
        val invocation = RuntimeHookGuard.value<RuntimeCausalCoroutineLaunch?>(null) {
            if (launch.invoked.compareAndSet(false, true)) launch
                else create(launch.siteId, launch.source, launch.line, launch.origin)
        }
        val previous = pending.get()
        if (invocation == null) pending.remove() else pending.set(Invocation(invocation))
        try {
            return block(invocation)
        } finally {
            if (previous == null) pending.remove() else pending.set(previous)
        }
    }

    /** The generated root must be the delegate's class and point to this exact completion. */
    fun expectRoot(launch: RuntimeCausalCoroutineLaunch?, delegate: Any, completion: Any?) {
        val invocation = pending.get()?.takeIf { it.launch === launch } ?: return
        invocation.rootClass = delegate.javaClass
        invocation.completion = completion as? CoroutineStackFrame
    }

    fun bindFirstSegment(
        continuation: Any, siteId: Long, source: String, line: Int, origin: Int,
    ): RuntimeCausalCoroutineLaunch? {
        val invocation = pending.get()
        val exactRoot = invocation != null && invocation.completion != null &&
            continuation.javaClass === invocation.rootClass &&
            (continuation as? CoroutineStackFrame)?.callerFrame === invocation.completion
        val launch = if (exactRoot) invocation?.launch else null
        if (exactRoot) pending.remove()
        // Without a builder bridge this is an observed state machine, not a proven creator.
        return launch?.takeIf { epoch()?.id == it.epochId && !it.terminal.get() }
            ?: create(siteId, source, line, origin, CausalPhase.Coroutine.STATE_MACHINE_OBSERVED)
    }

    fun enter(launch: RuntimeCausalCoroutineLaunch?, token: Long, generation: Long) {
        if (launch == null || token == 0L || launch.terminal.get()) return
        val epoch = epoch()?.takeIf { it.id == launch.epochId } ?: return
        val tid = android.os.Process.myTid().toLong().coerceAtLeast(0L)
        val started = now()
        val cpu = Debug.threadCpuTimeNanos()
        val previousSegment = running.get()
        val previous = concurrency.swapTrustedContext(CausalKind.COROUTINE, launch.id)
        try {
            running.set(Segment(launch, token, generation, started, tid, cpu, previous, previousSegment))
            epoch.writer.causalEvent(CausalEventObservation(
                kind = CausalKind.COROUTINE, phase = CausalPhase.Coroutine.RESUME_STARTED, id = launch.id,
                observedNanos = started, targetTid = tid, targetJavaId = observedThreadId(Thread.currentThread()),
                coroutineToken = token, coroutineGeneration = generation.coerceAtLeast(0L),
            ))
        } catch (failure: Throwable) {
            if (previousSegment == null) running.remove() else running.set(previousSegment)
            concurrency.restoreTrustedContext(previous)
            throw failure
        }
    }

    fun exit(token: Long, suspended: Boolean, outcome: CoroutineExecutionOutcome) {
        val segment = running.get()?.takeIf { it.token == token } ?: return
        if (segment.previousSegment == null) running.remove() else running.set(segment.previousSegment)
        concurrency.restoreTrustedContext(segment.previousContext)
        val epoch = epoch()?.takeIf { it.id == segment.launch.epochId } ?: return
        if (!suspended && !segment.launch.terminal.compareAndSet(false, true)) return
        val ended = now()
        val tid = android.os.Process.myTid().toLong().coerceAtLeast(0L)
        val cpu = Debug.threadCpuTimeNanos()
        epoch.writer.causalEvent(CausalEventObservation(
            kind = CausalKind.COROUTINE,
            phase = if (suspended) CausalPhase.Coroutine.SUSPENDED else CausalPhase.Coroutine.TERMINAL_OBSERVED,
            id = segment.launch.id, observedNanos = ended, targetTid = tid,
            durationNanos = if (ended >= segment.started) ended - segment.started else -1L,
            cpuNanos = if (tid > 0L && tid == segment.tid && segment.cpu >= 0L && cpu >= segment.cpu) cpu - segment.cpu else -1L,
            terminalOutcome = if (suspended) 0 else terminal(outcome),
            coroutineToken = token, coroutineGeneration = segment.generation.coerceAtLeast(0L),
        ))
        if (!suspended) releaseBindings(epoch, segment.launch)
    }

    /** Covers uninstrumented blocks too; a completed wrapper never implies an unobserved resume. */
    fun complete(launch: RuntimeCausalCoroutineLaunch?, outcome: CoroutineExecutionOutcome) {
        if (launch == null) return
        val epoch = epoch()?.takeIf { it.id == launch.epochId } ?: return
        if (!launch.terminal.compareAndSet(false, true)) return
        epoch.writer.causalEvent(CausalEventObservation(
            kind = CausalKind.COROUTINE, phase = CausalPhase.Coroutine.TERMINAL_OBSERVED,
            id = launch.id, observedNanos = now(), terminalOutcome = terminal(outcome),
        ))
        releaseBindings(epoch, launch)
    }

    fun builderReturned(prepared: Function2<*, *, *>?, job: Any?) {
        if (job == null) return
        val launch = (prepared as? JankHunterCoroutineFunction2)?.launchIdentity() ?: return
        val epoch = epoch()?.takeIf { it.id == launch.epochId } ?: return
        tryDiagnosticLock(launch.lock, Unit) {
            if (launch.terminal.get()) return
            if (epoch.causalRegistry?.bind(CausalKind.COROUTINE, job, launch.id) != true) {
                epoch.writer.recordQuality(QualityCounterId.CAUSAL_REGISTRY_CAPACITY)
            } else {
                launch.job = WeakReference(job)
            }
        }
    }

    private fun releaseBindings(epoch: RuntimeCollectionEpoch, launch: RuntimeCausalCoroutineLaunch) {
        tryDiagnosticLock(launch.lock, Unit) {
            epoch.causalRegistry?.release(CausalKind.COROUTINE, launch)
            launch.job?.get()?.let { epoch.causalRegistry?.release(CausalKind.COROUTINE, it) }
            launch.job = null
        }
    }

    fun builderThrew(prepared: Function2<*, *, *>?) {
        complete((prepared as? JankHunterCoroutineFunction2)?.launchIdentity(), CoroutineExecutionOutcome.FAILURE)
    }

    fun beginCancel(job: Any?, siteId: Long, source: String, line: Int, origin: Int): Long {
        if (job == null) return 0L
        val epoch = epoch() ?: return 0L
        val id = epoch.causalRegistry?.find(CausalKind.COROUTINE, job)?.takeIf { it > 0L } ?: return 0L
        val actor = concurrency.captureCausalContext()
        epoch.writer.causalEvent(CausalEventObservation(
            kind = CausalKind.COROUTINE, phase = CausalPhase.Coroutine.CANCEL_REQUESTED, id = id,
            actorKind = actor?.entityKind ?: 0, actorId = actor?.entityId ?: 0L,
            observedNanos = now(), sourceSiteId = siteId.coerceAtLeast(0L), sourceMethod = source,
            sourceLine = line.coerceAtLeast(0), sourceOrigin = origin.coerceIn(0, 2),
        ))
        return id
    }

    fun cancelResult(job: Any?, token: Long, threw: Boolean) {
        if (job == null || token <= 0L) return
        val epoch = epoch() ?: return
        if (epoch.causalRegistry?.find(CausalKind.COROUTINE, job) != token) return
        epoch.writer.causalEvent(CausalEventObservation(
            kind = CausalKind.COROUTINE, phase = CausalPhase.Coroutine.CANCEL_REQUESTED, id = token,
            observedNanos = now(), callResult = if (threw) 3 else 1,
        ))
    }

    private fun epoch(): RuntimeCollectionEpoch? = access.collectionEpoch?.takeIf {
        it.config.concurrencyDiagnosticsEnabled() && it.writer.isAcceptingEvents()
    }

    private fun now(): Long = SystemClock.elapsedRealtimeNanos().coerceAtLeast(0L)

    private fun terminal(outcome: CoroutineExecutionOutcome): Int = when (outcome) {
        CoroutineExecutionOutcome.SUCCESS -> 1
        CoroutineExecutionOutcome.FAILURE -> 2
        CoroutineExecutionOutcome.CANCELLED -> 3
    }

    private class Segment(
        val launch: RuntimeCausalCoroutineLaunch, val token: Long, val generation: Long,
        val started: Long, val tid: Long, val cpu: Long, val previousContext: Long,
        val previousSegment: Segment?,
    )

    /** Lives only on the invocation stack; no delegate or completion survives suspension here. */
    private class Invocation(val launch: RuntimeCausalCoroutineLaunch) {
        var rootClass: Class<*>? = null
        var completion: CoroutineStackFrame? = null
    }
}
