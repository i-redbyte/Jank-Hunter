package io.jankhunter.runtime

import android.os.SystemClock
import android.os.Debug
import io.jankhunter.runtime.internal.io.CausalEventObservation
import io.jankhunter.runtime.internal.io.CausalKind
import io.jankhunter.runtime.internal.io.CausalPhase
import io.jankhunter.runtime.internal.io.QualityCounterId
import java.lang.ref.WeakReference
import java.util.UUID
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.locks.ReentrantLock
import kotlin.concurrent.withLock

/** Process-local service; all identity and queued data belong to the current collection epoch. */
internal class RuntimeConcurrencyTelemetry(private val access: RuntimeTelemetryAccess) {
    private val active = PrimitiveLongThreadLocal()
    private val rootRun = ThreadLocal<RootRunCell>()
    private val directRuns = ThreadLocal<DirectRunCell>()
    private val executors = RuntimeCausalExecutorTelemetry(access, this)
    private val handlers = RuntimeCausalHandlerTelemetry(access, this)
    private val submissionsLock = ReentrantLock()
    private var submissionsEpochId = 0L
    private val submissions = HashMap<UUID, WorkerSubmission>()
    private val workerImplementationSites = HashMap<Long, MutableSet<Long>>()
    private var workerImplementationSiteCount = 0

    fun isEnabled(): Boolean = activeEpoch() != null

    internal fun swapTrustedContext(kind: Int, id: Long): Long {
        val previous = active.get()
        active.set(pack(kind, id))
        return previous
    }

    internal fun restoreTrustedContext(previous: Long) {
        active.set(previous)
    }

    fun prepareExecutorRunnable(
        executor: Any?, original: Runnable?, prepared: Runnable?, siteId: Long, source: String, line: Int, origin: Int,
        delay: Long, period: Long, unit: Any?, cadence: Int,
    ): Runnable? = executors.prepareRunnable(executor, original, prepared, siteId, source, line, origin,
        delay, period, unit, cadence)

    fun <T> prepareExecutorCallable(
        executor: Any?, original: java.util.concurrent.Callable<T>?, prepared: java.util.concurrent.Callable<T>?, siteId: Long, source: String,
        line: Int, origin: Int, delay: Long, period: Long, unit: Any?, cadence: Int,
    ): java.util.concurrent.Callable<T>? = executors.prepareCallable(executor, original, prepared, siteId,
        source, line, origin, delay, period, unit, cadence)

    fun executorHandoffReturned(prepared: Any?, result: Any?) = executors.handoffReturned(prepared, result)
    fun executorHandoffThrew(prepared: Any?) = executors.handoffThrew(prepared)
    fun trackedRunnable(executor: Any, command: Runnable): Runnable = executors.trackedRunnable(executor, command)
    fun beginTrackedExecutorHandoff(executor: Any, command: Runnable): RuntimeCausalExecutorTelemetry.TrackedHandoff =
        executors.beginTrackedHandoff(executor, command)
    fun finishTrackedExecutorHandoff(handoff: RuntimeCausalExecutorTelemetry.TrackedHandoff, threw: Boolean) =
        executors.finishTrackedHandoff(handoff, threw)
    fun observeExecutorConstructed(executor: Any?, siteId: Long, source: String, line: Int, origin: Int) =
        executors.observeExecutorConstructed(executor, siteId, source, line, origin)
    fun observeExecutorFactoryReturned(executor: Any?, siteId: Long, source: String, line: Int, origin: Int) =
        executors.observeExecutorFactoryReturned(executor, siteId, source, line, origin)
    fun beginExecutorShutdown(executor: Any?, siteId: Long, source: String, line: Int, origin: Int): Long =
        executors.beginShutdown(executor, siteId, source, line, origin)
    fun executorShutdownReturned(executor: Any?, token: Long) = executors.shutdownReturned(executor, token)
    fun executorShutdownThrew(executor: Any?, token: Long) = executors.shutdownThrew(executor, token)
    fun observeExecutorTerminated(executor: Any?, terminated: Boolean) = executors.observeTerminated(executor, terminated)
    fun beginFutureCancel(future: Any?, siteId: Long, source: String, line: Int, origin: Int): Long =
        executors.beginFutureCancel(future, siteId, source, line, origin)
    fun futureCancelReturned(future: Any?, token: Long, cancelled: Boolean) =
        executors.futureCancelReturned(future, token, cancelled)
    fun futureCancelThrew(future: Any?, token: Long) = executors.futureCancelThrew(future, token)

    fun prepareThreadTarget(target: Runnable?, siteId: Long, source: String, line: Int, origin: Int): Runnable? {
        if (!access.usesTaskWrappers) return target
        if (target == null || activeEpoch() == null) return target
        return ControlledThreadTarget(this, target, siteId, source.take(512), line, origin)
    }

    fun bindThreadTarget(thread: Thread?, prepared: Runnable?) {
        if (thread == null || prepared !is ControlledThreadTarget || prepared.owner !== this) return
        val epoch = activeEpoch() ?: return
        observeThreadConstructed(thread, prepared.siteId, prepared.source, prepared.line, prepared.origin)
        val id = epoch.causalRegistry?.find(CausalKind.THREAD, thread) ?: 0L
        if (id > 0L) prepared.bind(epoch.id, thread, id)
    }

    fun beginHandlerPost(handler: Any?, callback: Runnable?, siteId: Long, source: String, line: Int,
                         origin: Int, requestedDelayMillis: Long = Long.MIN_VALUE): Long =
        handlers.beginPost(handler, callback, siteId, source, line, origin, requestedDelayMillis)
    fun handlerPostReturned(handler: Any?, callback: Runnable?, token: Long, posted: Boolean) =
        handlers.postReturned(handler, callback, token, posted)
    fun handlerPostThrew(handler: Any?, callback: Runnable?, token: Long) =
        handlers.postThrew(handler, callback, token)
    fun prepareTrackedHandlerPost(callback: Runnable, siteId: Long, source: String, line: Int,
                                  origin: Int, requestedDelayMillis: Long = Long.MIN_VALUE) =
        handlers.prepareTrackedPost(callback, siteId, source, line, origin, requestedDelayMillis)
    fun trackedHandlerPostReturned(prepared: RuntimeCausalHandlerTelemetry.PreparedPost, posted: Boolean) =
        handlers.trackedPostReturned(prepared, posted)
    fun trackedHandlerPostThrew(prepared: RuntimeCausalHandlerTelemetry.PreparedPost) =
        handlers.trackedPostThrew(prepared)
    fun handlerRemovalRequested(epochId: Long, id: Long): Long = handlers.removalRequested(epochId, id)
    fun handlerRemovalReturned(epochId: Long, token: Long, threw: Boolean) =
        handlers.removalReturned(epochId, token, threw)

    fun observeThreadConstructed(thread: Thread?, siteId: Long, source: String, line: Int, origin: Int) {
        if (thread == null) return
        val epoch = activeEpoch() ?: return
        val registry = epoch.causalRegistry ?: return
        if (registry.find(CausalKind.THREAD, thread) != 0L) return
        val id = registry.register(CausalKind.THREAD, thread)
        if (id == 0L) return capacityLost(epoch)
        val actor = captureCausalContext()
        emit(epoch, CausalEventObservation(
            kind = CausalKind.THREAD, phase = CausalPhase.Thread.CONSTRUCTED, id = id,
            parentKind = actor?.entityKind ?: 0, parentId = actor?.entityId ?: 0L,
            actorKind = actor?.entityKind ?: 0, actorId = actor?.entityId ?: 0L,
            observedNanos = now(), targetJavaId = observedThreadId(thread),
            sourceSiteId = siteId.coerceAtLeast(0L), sourceMethod = source,
            sourceLine = line.coerceAtLeast(0), sourceOrigin = origin.coerceIn(0, 2),
        ))
    }

    fun observeThreadFactoryReturned(thread: Thread?, siteId: Long, source: String, line: Int, origin: Int) {
        if (thread == null) return
        val epoch = activeEpoch() ?: return
        val id = epoch.causalRegistry?.register(CausalKind.THREAD, thread) ?: 0L
        if (id == 0L) return capacityLost(epoch)
        val actor = captureCausalContext()
        emit(epoch, CausalEventObservation(
            kind = CausalKind.THREAD, phase = CausalPhase.Thread.FACTORY_RETURNED, id = id,
            actorKind = actor?.entityKind ?: 0, actorId = actor?.entityId ?: 0L,
            observedNanos = now(), targetJavaId = observedThreadId(thread),
            sourceSiteId = siteId.coerceAtLeast(0L), sourceMethod = source,
            sourceLine = line.coerceAtLeast(0), sourceOrigin = origin.coerceIn(0, 2),
        ))
    }

    fun beginThreadStart(thread: Thread?, siteId: Long, source: String, line: Int, origin: Int): Long {
        if (thread == null) return 0L
        val epoch = activeEpoch() ?: return 0L
        val id = epoch.causalRegistry?.register(CausalKind.THREAD, thread) ?: 0L
        if (id == 0L) {
            capacityLost(epoch)
            return 0L
        }
        epoch.causalRegistry?.markThreadStart(thread)
        val actor = captureCausalContext()
        emit(epoch, CausalEventObservation(
            kind = CausalKind.THREAD, phase = CausalPhase.Thread.START_INVOKED, id = id,
            actorKind = actor?.entityKind ?: 0, actorId = actor?.entityId ?: 0L,
            observedNanos = now(), targetJavaId = observedThreadId(thread),
            sourceSiteId = siteId.coerceAtLeast(0L), sourceMethod = source,
            sourceLine = line.coerceAtLeast(0), sourceOrigin = origin.coerceIn(0, 2),
        ))
        return id
    }

    fun threadStartReturned(thread: Thread?, token: Long) = finishThreadCall(thread, token, CausalPhase.Thread.START_RETURNED)

    fun threadStartThrew(thread: Thread?, token: Long) = finishThreadCall(thread, token, CausalPhase.Thread.START_THREW)

    fun beginThreadInterrupt(thread: Thread?, siteId: Long, source: String, line: Int, origin: Int): Long {
        if (thread == null) return 0L
        val epoch = activeEpoch() ?: return 0L
        val id = epoch.causalRegistry?.register(CausalKind.THREAD, thread) ?: 0L
        if (id == 0L) {
            capacityLost(epoch)
            return 0L
        }
        val actor = captureCausalContext()
        emit(epoch, CausalEventObservation(
            kind = CausalKind.THREAD, phase = CausalPhase.Thread.INTERRUPT_REQUESTED, id = id,
            actorKind = actor?.entityKind ?: 0, actorId = actor?.entityId ?: 0L,
            observedNanos = now(), targetJavaId = observedThreadId(thread),
            sourceSiteId = siteId.coerceAtLeast(0L), sourceMethod = source,
            sourceLine = line.coerceAtLeast(0), sourceOrigin = origin.coerceIn(0, 2),
        ))
        return id
    }

    fun threadInterruptReturned(thread: Thread?, token: Long) =
        finishThreadCall(thread, token, CausalPhase.Thread.INTERRUPT_RETURNED)

    fun threadInterruptThrew(thread: Thread?, token: Long) =
        finishThreadCall(thread, token, CausalPhase.Thread.INTERRUPT_THREW)

    fun observeThreadEnd(thread: Thread?) {
        if (thread == null) return
        val epoch = activeEpoch() ?: return
        if (observedThreadState(thread) != Thread.State.TERMINATED) return
        val registry = epoch.causalRegistry ?: return
        if (registry.find(CausalKind.THREAD, thread) == 0L &&
            registry.register(CausalKind.THREAD, thread) == 0L) return capacityLost(epoch)
        val id = registry.markThreadEndObserved(thread)
        if (id == 0L) return
        emit(epoch, CausalEventObservation(
            kind = CausalKind.THREAD, phase = CausalPhase.Thread.END_OBSERVED, id = id,
            observedNanos = now(), targetJavaId = observedThreadId(thread),
        ))
    }

    fun enterRunnableRun(receiver: Any?, siteId: Long, source: String, line: Int, origin: Int): Long {
        val thread = receiver as? Thread ?: return beginDirectExecution(receiver, siteId, source, line, origin)
        if (thread !== Thread.currentThread()) return beginDirectExecution(receiver, siteId, source, line, origin)
        val epoch = activeEpoch() ?: return 0L
        if (epoch.causalRegistry?.hasThreadStart(thread) != true) {
            return beginDirectExecution(receiver, siteId, source, line, origin)
        }
        if (rootRun.get()?.id != 0L && rootRun.get()?.epochId == epoch.id) {
            return beginDirectExecution(receiver, siteId, source, line, origin)
        }
        return beginRootRun(epoch, thread, siteId, source, line, origin)
    }

    private fun enterBoundThreadTarget(target: ControlledThreadTarget): Long {
        val thread = target.boundThread()?.takeIf { it === Thread.currentThread() } ?: return 0L
        val epoch = activeEpoch() ?: return 0L
        if (target.epochId != epoch.id || rootRun.get()?.epochId == epoch.id) return 0L
        if (epoch.causalRegistry?.find(CausalKind.THREAD, thread) != target.threadId) return 0L
        if (!target.claim()) return 0L
        return beginRootRun(epoch, thread, target.siteId, target.source, target.line, target.origin)
    }

    private fun beginRootRun(
        epoch: RuntimeCollectionEpoch, thread: Thread, siteId: Long, source: String, line: Int, origin: Int,
    ): Long {
        val id = epoch.causalRegistry?.register(CausalKind.THREAD, thread) ?: 0L
        if (id == 0L) {
            capacityLost(epoch)
            return 0L
        }
        val started = now()
        val tid = android.os.Process.myTid().toLong().coerceAtLeast(0L)
        val cpu = Debug.threadCpuTimeNanos()
        val previous = active.get()
        rootRun.set(RootRunCell(epoch.id, id, started, tid, cpu, previous))
        active.set(pack(CausalKind.THREAD, id))
        emit(epoch, CausalEventObservation(
            kind = CausalKind.THREAD, phase = CausalPhase.Thread.ROOT_RUN_STARTED, id = id,
            observedNanos = started, targetJavaId = observedThreadId(thread), targetTid = tid,
            sourceSiteId = siteId.coerceAtLeast(0L), sourceMethod = source,
            sourceLine = line.coerceAtLeast(0), sourceOrigin = origin.coerceIn(0, 2),
        ))
        return id
    }

    fun exitRunnableRun(token: Long, outcome: Int) {
        if (token == 0L) return
        val cell = rootRun.get()
        if (cell?.id != token) {
            endDirectExecution(token, outcome)
            return
        }
        rootRun.remove()
        active.set(cell.previous)
        val epoch = activeEpoch() ?: return
        if (cell.epochId != epoch.id) {
            epoch.writer.recordQuality(QualityCounterId.CAUSAL_STALE_EPOCH)
            return
        }
        val tid = android.os.Process.myTid().toLong().coerceAtLeast(0L)
        val ended = now()
        val endCpu = Debug.threadCpuTimeNanos()
        val cpuDelta = if (tid > 0L && tid == cell.tid && cell.cpuNanos >= 0L && endCpu >= cell.cpuNanos) {
            endCpu - cell.cpuNanos
        } else -1L
        emit(epoch, CausalEventObservation(
            kind = CausalKind.THREAD, phase = CausalPhase.Thread.ROOT_RUN_ENDED, id = token,
            observedNanos = ended, targetJavaId = observedThreadId(Thread.currentThread()), targetTid = tid,
            durationNanos = (ended - cell.startedNanos).coerceAtLeast(0L), cpuNanos = cpuDelta,
            terminalOutcome = if (outcome == 0) 1 else 2,
        ))
    }

    fun enterCallableCall(receiver: Any?, siteId: Long, source: String, line: Int, origin: Int): Long =
        beginDirectExecution(receiver, siteId, source, line, origin)

    fun exitCallableCall(token: Long, outcome: Int) = endDirectExecution(token, outcome)

    private fun beginDirectExecution(receiver: Any?, siteId: Long, source: String, line: Int, origin: Int): Long {
        if (receiver == null) return 0L
        val epoch = activeEpoch() ?: return 0L
        val parent = captureCausalContext()
        if (origin != 1 && parent == null) return 0L
        val registry = epoch.causalRegistry ?: return 0L
        val id = registry.newId()
        if (id == 0L) return 0L
        val started = now()
        val tid = android.os.Process.myTid().toLong().coerceAtLeast(0L)
        val cell = DirectRunCell(epoch.id, id, started, tid, Debug.threadCpuTimeNanos(),
            active.get(), directRuns.get())
        if (!registry.bind(CausalKind.DIRECT_EXECUTION, cell, id)) {
            capacityLost(epoch)
            return 0L
        }
        try {
            emit(epoch, CausalEventObservation(
                kind = CausalKind.DIRECT_EXECUTION, phase = CausalPhase.DirectExecution.RUN_STARTED, id = id,
                parentKind = parent?.entityKind ?: 0, parentId = parent?.entityId ?: 0L,
                actorKind = parent?.entityKind ?: 0, actorId = parent?.entityId ?: 0L,
                observedNanos = started, targetJavaId = observedThreadId(Thread.currentThread()),
                targetTid = tid, sourceSiteId = siteId.coerceAtLeast(0L), sourceMethod = source,
                sourceLine = line.coerceAtLeast(0), sourceOrigin = origin.coerceIn(0, 2),
            ))
        } catch (throwable: Throwable) {
            registry.release(CausalKind.DIRECT_EXECUTION, cell)
            throw throwable
        }
        directRuns.set(cell)
        active.set(pack(CausalKind.DIRECT_EXECUTION, id))
        return id
    }

    private fun endDirectExecution(token: Long, outcome: Int) {
        if (token <= 0L) return
        val cell = directRuns.get() ?: return
        if (cell.id != token) return
        if (cell.previousCell == null) directRuns.remove() else directRuns.set(cell.previousCell)
        active.set(cell.previousContext)
        val epoch = activeEpoch() ?: return
        if (epoch.id != cell.epochId) {
            epoch.writer.recordQuality(QualityCounterId.CAUSAL_STALE_EPOCH)
            return
        }
        epoch.causalRegistry?.release(CausalKind.DIRECT_EXECUTION, cell)
        val ended = now()
        val tid = android.os.Process.myTid().toLong().coerceAtLeast(0L)
        val cpu = Debug.threadCpuTimeNanos()
        emit(epoch, CausalEventObservation(
            kind = CausalKind.DIRECT_EXECUTION, phase = CausalPhase.DirectExecution.RUN_ENDED, id = token,
            observedNanos = ended, targetTid = tid,
            durationNanos = (ended - cell.startedNanos).coerceAtLeast(0L),
            cpuNanos = if (tid > 0L && tid == cell.tid && cell.cpuNanos >= 0L && cpu >= cell.cpuNanos)
                cpu - cell.cpuNanos else -1L,
            terminalOutcome = if (outcome == 0) 1 else 2,
        ))
    }

    private fun finishThreadCall(thread: Thread?, token: Long, phase: Int) {
        if (thread == null || token <= 0L) return
        val epoch = activeEpoch() ?: return
        if (epoch.causalRegistry?.find(CausalKind.THREAD, thread) != token) {
            epoch.writer.recordQuality(QualityCounterId.CAUSAL_BINDING_MISS)
            return
        }
        if (phase == CausalPhase.Thread.START_RETURNED || phase == CausalPhase.Thread.START_THREW) {
            epoch.causalRegistry.markThreadStartResult(thread, phase == CausalPhase.Thread.START_RETURNED)
        }
        emit(epoch, CausalEventObservation(
            kind = CausalKind.THREAD, phase = phase, id = token,
            observedNanos = now(), targetJavaId = observedThreadId(thread),
            callResult = if (phase == CausalPhase.Thread.START_THREW ||
                phase == CausalPhase.Thread.INTERRUPT_THREW) 3 else 1,
        ))
    }

    private fun capacityLost(epoch: RuntimeCollectionEpoch) {
        epoch.writer.recordQuality(QualityCounterId.CAUSAL_REGISTRY_CAPACITY)
    }

    private class RootRunCell(
        val epochId: Long, val id: Long, val startedNanos: Long, val tid: Long,
        val cpuNanos: Long, val previous: Long,
    )

    private class ControlledThreadTarget(
        val owner: RuntimeConcurrencyTelemetry, private val delegate: Runnable,
        val siteId: Long, val source: String, val line: Int, val origin: Int,
    ) : Runnable {
        private val started = AtomicBoolean()
        @Volatile
        private var thread = WeakReference<Thread>(null)
        @Volatile var epochId: Long = 0L
            private set
        @Volatile var threadId: Long = 0L
            private set

        fun bind(epoch: Long, target: Thread, id: Long) {
            thread = WeakReference(target)
            threadId = id
            epochId = epoch
        }

        fun boundThread(): Thread? = thread.get()
        fun claim(): Boolean = started.compareAndSet(false, true)

        override fun run() {
            val token = RuntimeHookGuard.value(0L) { owner.enterBoundThreadTarget(this) }
            var failed = false
            try {
                delegate.run()
            } catch (throwable: Throwable) {
                failed = true
                throw throwable
            } finally {
                if (token > 0L) RuntimeHookGuard.run { owner.exitRunnableRun(token, if (failed) 1 else 0) }
            }
        }
    }

    private class DirectRunCell(
        val epochId: Long, val id: Long, val startedNanos: Long, val tid: Long,
        val cpuNanos: Long, val previousContext: Long, val previousCell: DirectRunCell?,
    )

    fun captureCausalContext(): JankHunterCausalContext? {
        val epoch = activeEpoch() ?: return null
        val packed = active.get()
        val id = packed ushr KIND_BITS
        if (id < epoch.causalIdFloor || id == 0L) return null
        val kind = (packed and KIND_MASK).toInt()
        if (kind !in 1..CausalKind.DIRECT_EXECUTION) return null
        return JankHunterCausalContext(epoch.id, kind, id)
    }

    fun observeCausalWorkSubmissionRequests(requests: Any?, siteId: Long, source: String, line: Int, origin: Int) {
        val epoch = activeEpoch() ?: return
        val limit = minOf(128, epoch.config.concurrencyMaxTrackedEntities())
        WorkChainRequestCapture.captureRequests(requests, limit,
            { omitted -> epoch.writer.recordQuality(QualityCounterId.CAUSAL_REGISTRY_CAPACITY, omitted.toLong()) },
            { epoch.writer.recordQuality(QualityCounterId.CAUSAL_BINDING_MISS) },
        ) { request: Any, _, _ ->
            val requestId = WorkChainRequestCapture.requestId(request)
            val captured = WorkerSubmission(captureCausalContext(), siteId.coerceAtLeast(0L),
                source.take(512), line.coerceAtLeast(0), origin.coerceIn(0, 2))
            val admission = tryDiagnosticLock(submissionsLock, 3) {
                resetSubmissions(epoch.id)
                val previous = submissions[requestId]
                when {
                    previous == null && submissions.size >= epoch.config.concurrencyMaxTrackedEntities() -> 2
                    previous == null -> {
                        submissions[requestId] = captured
                        0
                    }
                    previous.ambiguous || previous.sameEvidence(captured) -> 0
                    else -> {
                        // A returned enqueue call does not prove which submission WorkManager accepted.
                        submissions[requestId] = WorkerSubmission(null, 0L, "", 0, 0, ambiguous = true)
                        1
                    }
                }
            }
            if (admission == 1) epoch.writer.recordQuality(QualityCounterId.CAUSAL_BINDING_MISS)
            if (admission == 2) epoch.writer.recordQuality(QualityCounterId.CAUSAL_REGISTRY_CAPACITY)
        }
    }

    fun observeWorkerImplementationSite(worker: Any?, siteId: Long, source: String, line: Int, origin: Int) {
        if (worker == null) return
        val epoch = activeEpoch() ?: return
        val activeWorker = captureCausalContext()?.takeIf { it.entityKind == CausalKind.WORKER_ATTEMPT } ?: return
        if (epoch.causalRegistry?.find(CausalKind.WORKER_ATTEMPT, worker) != activeWorker.entityId) return
        val exactSite = siteId.coerceAtLeast(0L)
        val admission = tryDiagnosticLock(submissionsLock, -2) {
            resetSubmissions(epoch.id)
            val seen = workerImplementationSites[activeWorker.entityId]
            when {
                seen?.contains(exactSite) == true -> 0
                workerImplementationSiteCount >= epoch.config.concurrencyMaxTrackedEntities() -> -1
                else -> {
                    workerImplementationSites.getOrPut(activeWorker.entityId) { HashSet() }.add(exactSite)
                    workerImplementationSiteCount++
                    1
                }
            }
        }
        if (admission == -1) epoch.writer.recordQuality(QualityCounterId.CAUSAL_REGISTRY_CAPACITY)
        if (admission != 1) return
        emit(epoch, CausalEventObservation(
            kind = CausalKind.WORKER_ATTEMPT, phase = CausalPhase.WorkerAttempt.IMPLEMENTATION_OBSERVED,
            id = activeWorker.entityId, actorKind = CausalKind.WORKER_ATTEMPT, actorId = activeWorker.entityId,
            observedNanos = now(), sourceSiteId = exactSite, sourceMethod = source,
            sourceLine = line.coerceAtLeast(0), sourceOrigin = origin.coerceIn(0, 2),
        ))
    }

    fun beginConcurrencyWorker(
        worker: Any?, instanceId: Long, runAttempt: Int, generation: Int, uuidMost: Long, uuidLeast: Long,
    ): JankHunterCausalContext? {
        val epoch = activeEpoch() ?: return null
        if (instanceId == 0L || runAttempt < 0 || generation < 0) return null
        // The existing WorkerIdentity record carries the exact UUID words; this event joins by tuple.
        val registry = epoch.causalRegistry ?: return null
        // WorkerIdentity and the causal binding must carry the same unsigned 64-bit ID bits.
        // The legacy WORKERS feature may be disabled while causal diagnostics are active.
        epoch.writer.causalWorkerIdentity(instanceId, uuidMost, uuidLeast)
        val id = registry.newId()
        if (id == 0L) return null
        val context = JankHunterCausalContext(epoch.id, CausalKind.WORKER_ATTEMPT, id)
        if (!registry.bind(CausalKind.WORKER_ATTEMPT, context, id)) {
            epoch.writer.recordQuality(QualityCounterId.CAUSAL_REGISTRY_CAPACITY)
            return null
        }
        if (worker != null && !registry.bind(CausalKind.WORKER_ATTEMPT, worker, id)) {
            epoch.writer.recordQuality(QualityCounterId.CAUSAL_REGISTRY_CAPACITY)
        }
        val submission = tryDiagnosticLock(submissionsLock, null) {
            resetSubmissions(epoch.id)
            submissions[UUID(uuidMost, uuidLeast)]?.takeUnless { it.ambiguous }
        }
        val parent = submission?.parent?.takeIf { it.epochId == epoch.id }
        emit(epoch, CausalEventObservation(
            kind = CausalKind.WORKER_ATTEMPT, phase = CausalPhase.WorkerAttempt.BOUND, id = id,
            parentKind = parent?.entityKind ?: 0, parentId = parent?.entityId ?: 0L,
            actorKind = parent?.entityKind ?: 0, actorId = parent?.entityId ?: 0L,
            observedNanos = now(), workerInstanceId = instanceId, runAttempt = runAttempt, generation = generation,
            appCallerSiteId = if (submission?.origin == 1) submission.siteId else 0L,
            appCallerMethod = if (submission?.origin == 1) submission.source else null,
            appCallerLine = if (submission?.origin == 1) submission.line else 0,
        ))
        emit(epoch, CausalEventObservation(
            kind = CausalKind.WORKER_ATTEMPT, phase = CausalPhase.WorkerAttempt.STARTED, id = id,
            observedNanos = now(), workerInstanceId = instanceId, runAttempt = runAttempt, generation = generation,
            targetJavaId = observedThreadId(Thread.currentThread()), targetTid = android.os.Process.myTid().toLong().coerceAtLeast(0L),
        ))
        return context
    }

    fun installConcurrencyContext(context: JankHunterCausalContext): Long {
        val previous = active.get()
        val epoch = activeEpoch() ?: return previous
        if (context.epochId != epoch.id) {
            epoch.writer.recordQuality(QualityCounterId.CAUSAL_STALE_EPOCH)
            return previous
        }
        if (epoch.causalRegistry?.find(context.entityKind, context) != context.entityId) {
            epoch.writer.recordQuality(QualityCounterId.CAUSAL_BINDING_MISS)
            return previous
        }
        active.set(pack(context.entityKind, context.entityId))
        return previous
    }

    fun restoreConcurrencyContext(previous: Long) {
        active.set(previous)
    }

    fun endConcurrencyWorker(worker: Any?, context: JankHunterCausalContext, outcome: JankHunterWorkerOutcome) {
        val epoch = activeEpoch() ?: return
        if (context.epochId != epoch.id) {
            epoch.writer.recordQuality(QualityCounterId.CAUSAL_STALE_EPOCH)
            return
        }
        if (epoch.causalRegistry?.release(CausalKind.WORKER_ATTEMPT, context) != true) {
            epoch.writer.recordQuality(QualityCounterId.CAUSAL_BINDING_MISS)
            return
        }
        if (worker != null && epoch.causalRegistry.find(CausalKind.WORKER_ATTEMPT, worker) == context.entityId) {
            epoch.causalRegistry.release(CausalKind.WORKER_ATTEMPT, worker)
        }
        tryDiagnosticLock(submissionsLock, Unit) {
            resetSubmissions(epoch.id)
            workerImplementationSiteCount -= workerImplementationSites.remove(context.entityId)?.size ?: 0
        }
        val retry = outcome == JankHunterWorkerOutcome.RETRY
        emit(epoch, CausalEventObservation(
            kind = CausalKind.WORKER_ATTEMPT,
            phase = if (retry) CausalPhase.WorkerAttempt.RETRY_RETURNED else CausalPhase.WorkerAttempt.TERMINAL_OBSERVED,
            id = context.entityId, observedNanos = now(),
            terminalOutcome = if (retry) 0 else workerTerminalOutcome(outcome),
        ))
    }

    /** `parent` must be captured at initiation, never inferred from the completion thread. */
    fun recordCost(
        category: Int, durationNanos: Long, readBytes: Long, writeBytes: Long,
        sourceSiteId: Long, sourceMethod: String?, sourceLine: Int, sourceOrigin: Int,
        parent: JankHunterCausalContext?,
        executionTid: Long = 0L,
    ) {
        val epoch = activeEpoch() ?: return
        if (category !in 1..10) return
        if (durationNanos < 0L) return
        val registry = epoch.causalRegistry ?: return
        val id = registry.newId()
        if (id == 0L) return
        val exactParent = parent.takeIf { it?.epochId == epoch.id }
        if (parent != null && exactParent == null) epoch.writer.recordQuality(QualityCounterId.CAUSAL_STALE_EPOCH)
        // A legacy label without a captured task or a verified project call site is not causal evidence.
        if (exactParent == null && (sourceOrigin != 1 || sourceMethod.isNullOrBlank())) return
        emit(epoch, CausalEventObservation(
            kind = CausalKind.OPERATION, phase = 1, id = id,
            parentKind = exactParent?.entityKind ?: 0, parentId = exactParent?.entityId ?: 0L,
            actorKind = exactParent?.entityKind ?: 0, actorId = exactParent?.entityId ?: 0L,
            observedNanos = now(),
            targetTid = executionTid.coerceAtLeast(0L),
            sourceSiteId = sourceSiteId.coerceAtLeast(0L), sourceMethod = sourceMethod?.takeIf(String::isNotBlank),
            sourceLine = sourceLine.coerceAtLeast(0), sourceOrigin = if (sourceMethod.isNullOrBlank()) 0 else sourceOrigin.coerceIn(0, 2),
            durationNanos = durationNanos.coerceAtLeast(-1L), readBytes = readBytes.coerceAtLeast(-1L),
            writtenBytes = writeBytes.coerceAtLeast(-1L), operationKind = category,
        ))
    }

    fun beginResourceAcquire(
        kind: Int, sourceSiteId: Long, sourceMethod: String?, sourceLine: Int, sourceOrigin: Int,
        parent: JankHunterCausalContext?, boundToScope: Boolean = false,
    ): JankHunterResourceLease? = beginResource(
        kind, sourceSiteId, sourceMethod, sourceLine, sourceOrigin, parent, boundToScope,
        CausalPhase.Resource.ACQUIRE_ATTEMPT,
    )

    /** An existing AutoCloseable enters a tracked use scope; its acquisition and prior state are unknown. */
    fun beginResourceUse(
        sourceSiteId: Long, sourceMethod: String?, sourceLine: Int, sourceOrigin: Int,
        parent: JankHunterCausalContext?,
    ): JankHunterResourceLease? = beginResource(
        JankHunterResourceKind.AUTO_CLOSEABLE.wire, sourceSiteId, sourceMethod, sourceLine,
        sourceOrigin, parent, true, CausalPhase.Resource.USE_ENTERED,
    )

    private fun beginResource(
        kind: Int, sourceSiteId: Long, sourceMethod: String?, sourceLine: Int, sourceOrigin: Int,
        parent: JankHunterCausalContext?, boundToScope: Boolean, phase: Int,
    ): JankHunterResourceLease? {
        val epoch = activeEpoch() ?: return null
        if (!epoch.config.concurrencyDetailedEnabled() || kind !in 1..4) return null
        val registry = epoch.causalRegistry ?: return null
        val id = registry.newId()
        if (id == 0L) return null
        val exactParent = parent.takeIf { it?.epochId == epoch.id }
        val parentKind = exactParent?.entityKind ?: 0
        val parentId = exactParent?.entityId ?: 0L
        val observedAcquire = phase == CausalPhase.Resource.ACQUIRE_ATTEMPT
        val lease = JankHunterResourceLease(epoch.id, id, kind,
            if (observedAcquire) parentKind else 0, if (observedAcquire) parentId else 0L,
            parentKind, parentId, boundToScope && exactParent != null)
        if (!registry.bind(CausalKind.RESOURCE, lease, id)) {
            epoch.writer.recordQuality(QualityCounterId.CAUSAL_REGISTRY_CAPACITY)
            return null
        }
        if (!observedAcquire) lease.state = LEASE_ACQUIRED
        emit(epoch, resourceEvent(lease, phase,
            sourceSiteId, sourceMethod, sourceLine, sourceOrigin))
        return lease
    }

    fun resourceAcquired(lease: JankHunterResourceLease) {
        tryDiagnosticLock(lease.lock, Unit) {
            val epoch = leaseEpoch(lease) ?: return
            if (lease.state != LEASE_ATTEMPTING) return
            lease.state = LEASE_ACQUIRED
            emit(epoch, resourceEvent(lease, CausalPhase.Resource.ACQUIRED))
        }
    }

    fun resourceAcquireFailed(lease: JankHunterResourceLease) {
        finishResource(lease, CausalPhase.Resource.ACQUIRE_FAILED)
    }

    fun resourceReleased(
        lease: JankHunterResourceLease, sourceSiteId: Long = 0L, sourceMethod: String? = null,
        sourceLine: Int = 0, sourceOrigin: Int = 0,
    ): Boolean = finishResource(lease, CausalPhase.Resource.RELEASED,
        sourceSiteId, sourceMethod, sourceLine, sourceOrigin)

    fun resourceTransferred(lease: JankHunterResourceLease, newScope: JankHunterCausalContext?): Boolean {
        return tryDiagnosticLock(lease.lock, false) {
            val epoch = leaseEpoch(lease) ?: return false
            if (lease.state != LEASE_ACQUIRED || (newScope != null && newScope.epochId != epoch.id)) return false
            lease.scopeKind = newScope?.entityKind ?: 0
            lease.scopeId = newScope?.entityId ?: 0L
            lease.boundToScope = newScope != null
            emit(epoch, resourceEvent(lease, CausalPhase.Resource.SCOPE_TRANSFERRED))
            return true
        }
    }

    private fun finishResource(
        lease: JankHunterResourceLease, phase: Int,
        sourceSiteId: Long = 0L, sourceMethod: String? = null, sourceLine: Int = 0, sourceOrigin: Int = 0,
    ): Boolean {
        return tryDiagnosticLock(lease.lock, false) {
            val epoch = leaseEpoch(lease) ?: return false
            val required = if (phase == CausalPhase.Resource.ACQUIRE_FAILED) LEASE_ATTEMPTING else LEASE_ACQUIRED
            if (lease.state != required) return false
            lease.state = LEASE_TERMINAL
            if (epoch.causalRegistry?.release(CausalKind.RESOURCE, lease) != true) {
                epoch.writer.recordQuality(QualityCounterId.CAUSAL_BINDING_MISS)
                return false
            }
            emit(epoch, resourceEvent(lease, phase, sourceSiteId, sourceMethod, sourceLine, sourceOrigin))
            return true
        }
    }

    private fun resourceEvent(
        lease: JankHunterResourceLease, phase: Int,
        sourceSiteId: Long = 0L, sourceMethod: String? = null, sourceLine: Int = 0, sourceOrigin: Int = 0,
    ): CausalEventObservation {
        val actor = captureCausalContext()
        return CausalEventObservation(
        kind = CausalKind.RESOURCE, phase = phase, id = lease.entityId,
        parentKind = if (phase == CausalPhase.Resource.ACQUIRE_ATTEMPT) lease.originKind else 0,
        parentId = if (phase == CausalPhase.Resource.ACQUIRE_ATTEMPT) lease.originId else 0L,
        actorKind = actor?.entityKind ?: 0, actorId = actor?.entityId ?: 0L,
        observedNanos = now(), resourceKind = lease.resourceKind,
        sourceSiteId = sourceSiteId.coerceAtLeast(0L), sourceMethod = sourceMethod?.takeIf(String::isNotBlank),
        sourceLine = sourceLine.coerceAtLeast(0), sourceOrigin = if (sourceMethod.isNullOrBlank()) 0 else sourceOrigin.coerceIn(0, 2),
        leaseScopeKind = lease.scopeKind, leaseScopeId = lease.scopeId, boundToScope = lease.boundToScope,
        leaseScopeKnown = lease.scopeId > 0L || phase == CausalPhase.Resource.SCOPE_TRANSFERRED,
    )
    }

    private fun leaseEpoch(lease: JankHunterResourceLease): RuntimeCollectionEpoch? {
        val epoch = activeEpoch() ?: return null
        if (lease.epochId != epoch.id) {
            epoch.writer.recordQuality(QualityCounterId.CAUSAL_STALE_EPOCH)
            return null
        }
        if (epoch.causalRegistry?.find(CausalKind.RESOURCE, lease) != lease.entityId) {
            epoch.writer.recordQuality(QualityCounterId.CAUSAL_BINDING_MISS)
            return null
        }
        return epoch
    }

    private fun activeEpoch(): RuntimeCollectionEpoch? =
        access.collectionEpoch?.takeIf { it.config.concurrencyDiagnosticsEnabled() && it.writer.isAcceptingEvents() }

    private fun emit(epoch: RuntimeCollectionEpoch, event: CausalEventObservation) {
        epoch.writer.causalEvent(event)
    }

    private fun workerTerminalOutcome(outcome: JankHunterWorkerOutcome): Int = when (outcome) {
        JankHunterWorkerOutcome.SUCCESS -> 1
        JankHunterWorkerOutcome.FAILURE -> 2
        JankHunterWorkerOutcome.CANCELLED -> 3
        JankHunterWorkerOutcome.UNKNOWN -> 0
        JankHunterWorkerOutcome.RETRY -> 0
        JankHunterWorkerOutcome.STOPPED -> 4
    }

    private fun pack(kind: Int, id: Long): Long =
        if (id <= 0L || id > Long.MAX_VALUE ushr KIND_BITS) 0L else id shl KIND_BITS or kind.toLong()

    private fun now(): Long = SystemClock.elapsedRealtimeNanos().coerceAtLeast(0L)

    private fun resetSubmissions(epochId: Long) {
        if (submissionsEpochId != epochId) {
            submissions.clear()
            workerImplementationSites.clear()
            workerImplementationSiteCount = 0
            submissionsEpochId = epochId
        }
    }

    internal fun workerImplementationSiteCountForTest(): Int =
        submissionsLock.withLock { workerImplementationSiteCount }

    private class WorkerSubmission(
        val parent: JankHunterCausalContext?, val siteId: Long, val source: String,
        val line: Int, val origin: Int, val ambiguous: Boolean = false,
    ) {
        fun sameEvidence(other: WorkerSubmission): Boolean {
            if (siteId != other.siteId || source != other.source || line != other.line || origin != other.origin) return false
            val first = parent
            val second = other.parent
            return if (first == null || second == null) first == null && second == null else
                first.epochId == second.epochId && first.entityKind == second.entityKind && first.entityId == second.entityId
        }
    }

    private companion object {
        const val KIND_BITS = 4
        const val KIND_MASK = 15L
        const val LEASE_ATTEMPTING = 0
        const val LEASE_ACQUIRED = 1
        const val LEASE_TERMINAL = 2
    }
}

internal object RuntimeCausalOperationKind {
    const val FILE_READ = 1
    const val FILE_WRITE = 2
    const val FILE_SYNC = 3
    const val DATABASE = 4
    const val HTTP = 5
    const val LOCK_WAIT = 6
    const val MONITOR_WAIT = 7
    const val WAKE_LOCK = 8
    const val CONTENT_READ = 9
    const val CONTENT_WRITE = 10
}
