package io.jankhunter.runtime

import android.os.SystemClock
import android.os.Debug
import android.os.Process
import io.jankhunter.runtime.internal.io.CausalEventObservation
import io.jankhunter.runtime.internal.io.CausalKind
import io.jankhunter.runtime.internal.io.CausalPhase
import io.jankhunter.runtime.internal.io.QualityCounterId
import java.util.ArrayDeque
import java.util.concurrent.Callable
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.locks.ReentrantLock

/** Passive executor boundaries: the command passed to the delegate is never changed here. */
internal class RuntimeCausalExecutorTelemetry(
    private val access: RuntimeTelemetryAccess,
    private val contexts: RuntimeConcurrencyTelemetry,
) {
    private val calls = ThreadLocal<CallFrames>()

    fun prepareRunnable(
        executor: Any?, original: Runnable?, prepared: Runnable?, siteId: Long, source: String,
        line: Int, origin: Int, delay: Long, period: Long, unit: Any?, cadence: Int,
    ): Runnable? {
        prepare(executor, original, prepared, siteId, source, line, origin, delay, period, unit, cadence)
        return prepared
    }

    fun <T> prepareCallable(
        executor: Any?, original: Callable<T>?, prepared: Callable<T>?, siteId: Long, source: String,
        line: Int, origin: Int, delay: Long, period: Long, unit: Any?, cadence: Int,
    ): Callable<T>? {
        prepare(executor, original, prepared, siteId, source, line, origin, delay, period, unit, cadence)
        return prepared
    }

    /** Explicit opt-in: the caller accepts command wrapping for a measured run boundary. */
    fun trackedRunnable(executor: Any, command: Runnable): Runnable = beginTrackedHandoff(executor, command).runnable

    fun beginTrackedHandoff(executor: Any, command: Runnable): TrackedHandoff {
        val epoch = epoch() ?: return TrackedHandoff(command)
        var frame = calls.get()?.takeIf { it.overflow == 0 }?.items?.peekLast()?.takeIf {
            it.epochId == epoch.id && it.executor === executor && it.prepared === command && !it.claimedByFacade
        }
        val ownsFrame = frame == null
        if (ownsFrame) {
            prepare(executor, command, command, 0L,
                "io.jankhunter.runtime.JankHunterTrackedExecutor#execute(Ljava/lang/Runnable;)V",
                0, 0, 0L, 0L, null, 0)
            val frames = calls.get()
            if (frames == null || frames.overflow != 0) {
                return TrackedHandoff(command, ownsFrame = true, overflow = frames != null)
            }
            frame = frames.items.peekLast()?.takeIf {
                it.epochId == epoch.id && it.executor === executor && it.prepared === command
            }
        }
        val claimed = frame ?: return TrackedHandoff(command)
        claimed.claimedByFacade = true
        val placement = claimed.placement
        if (placement == null) return TrackedHandoff(command, claimed, ownsFrame)
        if (!access.usesTaskWrappers) return TrackedHandoff(command, claimed, ownsFrame)
        val wrapper = TrackedRunnable(command, placement)
        if (epoch.causalRegistry?.bind(CausalKind.EXECUTOR_TASK, wrapper, placement.taskId) != true) {
            capacityLost(epoch)
            return TrackedHandoff(command, claimed, ownsFrame)
        }
        return TrackedHandoff(wrapper, claimed, ownsFrame, original = command)
    }

    fun finishTrackedHandoff(handoff: TrackedHandoff, threw: Boolean) {
        if (handoff.overflow) {
            popCall(handoff.original)
            return
        }
        val frame = handoff.frame as? CallFrame ?: return
        if (!frame.claimedByFacade) return
        frame.claimedByFacade = false
        if (!handoff.ownsFrame) return
        if (calls.get()?.items?.peekLast() !== frame) {
            epoch()?.writer?.recordQuality(QualityCounterId.CAUSAL_BINDING_MISS)
            return
        }
        if (threw) handoffThrew(handoff.original) else handoffReturned(handoff.original, null)
    }

    private fun prepare(
        executor: Any?, original: Any?, prepared: Any?, siteId: Long, source: String,
        line: Int, origin: Int, delay: Long, period: Long, unit: Any?, cadence: Int,
    ) {
        val epoch = epoch() ?: return
        val frames = calls.get() ?: CallFrames().also(calls::set)
        if (frames.items.size >= MAX_FRAMES) {
            frames.overflow++
            epoch.writer.recordQuality(QualityCounterId.CAUSAL_BINDING_MISS)
            return
        }
        val frame = CallFrame(epoch.id, executor, prepared)
        frames.items.addLast(frame)
        if (executor == null || original == null || prepared == null) return
        val registry = epoch.causalRegistry ?: return
        val scheduler = ensureExecutor(epoch, executor)
        if (scheduler == 0L) return
        val periodNanos = if (cadence >= 2 && period > 0L) (unit as? TimeUnit)?.toNanos(period) ?: -1L else -1L
        if (cadence >= 2 && periodNanos <= 0L) return
        val parent = contexts.captureCausalContext()
        val seriesId = if (cadence >= 2) registry.newId() else 0L
        val taskId = registry.newId()
        if (taskId == 0L || (cadence >= 2 && seriesId == 0L)) return
        val observed = now()
        val requestedDelay = if (cadence > 0 && delay >= 0L) (unit as? TimeUnit)?.toNanos(delay) ?: -1L else -1L
        val deadline = if (requestedDelay >= 0L && Long.MAX_VALUE - observed >= requestedDelay)
            observed + requestedDelay else -1L
        val placement = Placement(epoch.id, taskId, seriesId, scheduler, parent?.entityKind ?: 0,
            parent?.entityId ?: 0L, prepared)
        frame.placement = placement
        if (prepared is JankHunterRunnable || prepared is JankHunterCallable<*>) {
            bindControlled(epoch, placement, prepared)
        }
        if (seriesId > 0L) emit(epoch, CausalEventObservation(
            kind = CausalKind.SERIES, phase = CausalPhase.Series.DECLARED, id = seriesId,
            parentKind = placement.parentKind, parentId = placement.parentId,
            schedulerKind = CausalKind.EXECUTOR, schedulerId = scheduler,
            observedNanos = observed, sourceSiteId = siteId.coerceAtLeast(0L), sourceMethod = source,
            sourceLine = line.coerceAtLeast(0), sourceOrigin = origin.coerceIn(0, 2),
            requestedPeriodNanos = periodNanos, scheduleCadence = cadence,
        ))
        emit(epoch, CausalEventObservation(
            kind = CausalKind.EXECUTOR_TASK, phase = CausalPhase.ExecutorTask.SUBMITTED, id = taskId,
            parentKind = if (seriesId > 0L) CausalKind.SERIES else placement.parentKind,
            parentId = if (seriesId > 0L) seriesId else placement.parentId,
            schedulerKind = CausalKind.EXECUTOR, schedulerId = scheduler,
            actorKind = placement.parentKind, actorId = placement.parentId,
            observedNanos = observed, sourceSiteId = siteId.coerceAtLeast(0L), sourceMethod = source,
            sourceLine = line.coerceAtLeast(0), sourceOrigin = origin.coerceIn(0, 2),
            scheduledForNanos = deadline, occurrence = if (seriesId > 0L) 1L else -1L,
            requestedPeriodNanos = periodNanos, scheduleCadence = cadence,
        ))
    }

    private fun bindControlled(epoch: RuntimeCollectionEpoch, placement: Placement, prepared: Any) {
        val registry = epoch.causalRegistry ?: return
        val bound = registry.bind(CausalKind.EXECUTOR_TASK, prepared, placement.taskId)
        if (!bound) {
            if (registry.find(CausalKind.EXECUTOR_TASK, prepared) > 0L) {
                // The same wrapper was placed twice; either run could consume it first.
                when (prepared) {
                    is JankHunterRunnable -> prepared.bindCausal(CausalBinding(placement, prepared))
                    is JankHunterCallable<*> -> prepared.bindCausal(CausalBinding(placement, prepared))
                }
                registry.release(CausalKind.EXECUTOR_TASK, prepared)
                epoch.writer.recordQuality(QualityCounterId.CAUSAL_BINDING_MISS)
            } else capacityLost(epoch)
            return
        }
        val binding = CausalBinding(placement, prepared)
        val attached = when (prepared) {
            is JankHunterRunnable -> prepared.bindCausal(binding)
            is JankHunterCallable<*> -> prepared.bindCausal(binding)
            else -> false
        }
        if (!attached) {
            registry.release(CausalKind.EXECUTOR_TASK, prepared)
            epoch.writer.recordQuality(QualityCounterId.CAUSAL_BINDING_MISS)
        }
    }

    fun handoffReturned(prepared: Any?, result: Any?) {
        val placement = popCall(prepared) ?: return
        val epoch = matchingEpoch(placement.epochId) ?: return
        emit(epoch, CausalEventObservation(
            kind = CausalKind.EXECUTOR_TASK, phase = CausalPhase.ExecutorTask.HANDOFF_RETURNED,
            id = placement.taskId, observedNanos = now(), callResult = 1,
        ))
        if (result != null) bindReturnedFuture(epoch, placement, result)
    }

    private fun bindReturnedFuture(epoch: RuntimeCollectionEpoch, placement: Placement, result: Any) {
        val registry = epoch.causalRegistry ?: return
        if (placement.seriesId > 0L) {
            if (!registry.bind(CausalKind.SERIES, result, placement.seriesId)) capacityLost(epoch)
            return
        }
        tryDiagnosticLock(placement.lock, Unit) {
            // Inline execution can finish before submit returns; a finished Future needs no cancel bridge.
            if (placement.finished) return
            if (registry.bind(CausalKind.EXECUTOR_TASK, result, placement.taskId)) {
                placement.futureAlias = java.lang.ref.WeakReference(result)
            } else capacityLost(epoch)
        }
    }

    fun handoffThrew(prepared: Any?) {
        val placement = popCall(prepared) ?: return
        val epoch = matchingEpoch(placement.epochId) ?: return
        // A synchronous delegate may execute application work before throwing; this is not proof of rejection.
        emit(epoch, CausalEventObservation(
            kind = CausalKind.EXECUTOR_TASK, phase = CausalPhase.ExecutorTask.HANDOFF_THREW,
            id = placement.taskId, observedNanos = now(), callResult = 3,
        ))
    }

    private fun popCall(prepared: Any?): Placement? {
        val frames = calls.get() ?: return null
        if (frames.overflow > 0) {
            frames.overflow--
            if (frames.overflow == 0 && frames.items.isEmpty()) calls.remove()
            return null
        }
        val frame = frames.items.pollLast()
        if (frame?.prepared !== prepared) {
            frames.items.clear()
            calls.remove()
            epoch()?.writer?.recordQuality(QualityCounterId.CAUSAL_BINDING_MISS)
            return null
        }
        if (frames.items.isEmpty()) calls.remove()
        return frame?.placement
    }

    fun observeExecutorConstructed(executor: Any?, siteId: Long, source: String, line: Int, origin: Int) {
        val epoch = epoch() ?: return
        if (executor == null) return
        val registry = epoch.causalRegistry ?: return
        if (registry.find(CausalKind.EXECUTOR, executor) != 0L) return
        val id = registry.register(CausalKind.EXECUTOR, executor)
        if (id == 0L) return capacityLost(epoch)
        emitExecutorSite(epoch, id, CausalPhase.Executor.CONSTRUCTED, siteId, source, line, origin)
    }

    fun observeExecutorFactoryReturned(executor: Any?, siteId: Long, source: String, line: Int, origin: Int) {
        val epoch = epoch() ?: return
        if (executor == null) return
        val id = epoch.causalRegistry?.register(CausalKind.EXECUTOR, executor) ?: 0L
        if (id == 0L) return capacityLost(epoch)
        emitExecutorSite(epoch, id, CausalPhase.Executor.FACTORY_RETURNED, siteId, source, line, origin)
    }

    fun beginShutdown(executor: Any?, siteId: Long, source: String, line: Int, origin: Int): Long {
        val epoch = epoch() ?: return 0L
        if (executor == null) return 0L
        val id = ensureExecutor(epoch, executor)
        if (id == 0L) return 0L
        emitExecutorSite(epoch, id, CausalPhase.Executor.SHUTDOWN_REQUESTED, siteId, source, line, origin)
        return id
    }

    fun shutdownReturned(executor: Any?, token: Long) = finishShutdown(executor, token, CausalPhase.Executor.SHUTDOWN_RETURNED)

    fun shutdownThrew(executor: Any?, token: Long) = finishShutdown(executor, token, CausalPhase.Executor.SHUTDOWN_THREW)

    fun observeTerminated(executor: Any?, terminated: Boolean) {
        if (!terminated || executor == null) return
        val epoch = epoch() ?: return
        val id = ensureExecutor(epoch, executor)
        if (id == 0L) return
        emit(epoch, CausalEventObservation(
            kind = CausalKind.EXECUTOR, phase = CausalPhase.Executor.TERMINATED_OBSERVED,
            id = id, observedNanos = now(),
        ))
    }

    fun beginFutureCancel(future: Any?, siteId: Long, source: String, line: Int, origin: Int): Long {
        if (future == null) return 0L
        val epoch = epoch() ?: return 0L
        val target = futureTarget(epoch, future) ?: return 0L
        val phase = if (target.kind == CausalKind.SERIES) CausalPhase.Series.CANCEL_REQUESTED
            else CausalPhase.ExecutorTask.CANCEL_REQUESTED
        emit(epoch, CausalEventObservation(
            kind = target.kind, phase = phase, id = target.id, observedNanos = now(),
            sourceSiteId = siteId.coerceAtLeast(0L), sourceMethod = source, sourceLine = line.coerceAtLeast(0),
            sourceOrigin = origin.coerceIn(0, 2),
        ))
        return target.id
    }

    fun futureCancelReturned(future: Any?, token: Long, cancelled: Boolean) =
        finishFutureCancel(future, token, if (cancelled) 1 else 2)

    fun futureCancelThrew(future: Any?, token: Long) = finishFutureCancel(future, token, 3)

    private fun finishFutureCancel(future: Any?, token: Long, result: Int) {
        if (future == null || token <= 0L) return
        val epoch = epoch() ?: return
        val target = futureTarget(epoch, future) ?: return
        if (target.id != token) return
        val phase = if (target.kind == CausalKind.SERIES) CausalPhase.Series.CANCEL_RETURNED
            else CausalPhase.ExecutorTask.CANCEL_RETURNED
        emit(epoch, CausalEventObservation(
            kind = target.kind, phase = phase, id = token, observedNanos = now(), callResult = result,
        ))
    }

    private fun futureTarget(epoch: RuntimeCollectionEpoch, future: Any): Target? {
        val registry = epoch.causalRegistry ?: return null
        val series = registry.find(CausalKind.SERIES, future)
        if (series > 0L) return Target(CausalKind.SERIES, series)
        val task = registry.find(CausalKind.EXECUTOR_TASK, future)
        return if (task > 0L) Target(CausalKind.EXECUTOR_TASK, task) else null
    }

    private fun finishShutdown(executor: Any?, token: Long, phase: Int) {
        if (executor == null || token <= 0L) return
        val epoch = epoch() ?: return
        if (epoch.causalRegistry?.find(CausalKind.EXECUTOR, executor) != token) return
        emit(epoch, CausalEventObservation(
            kind = CausalKind.EXECUTOR, phase = phase, id = token, observedNanos = now(),
            callResult = if (phase == CausalPhase.Executor.SHUTDOWN_THREW) 3 else 1,
        ))
    }

    private fun ensureExecutor(epoch: RuntimeCollectionEpoch, executor: Any): Long {
        val registry = epoch.causalRegistry ?: return 0L
        val existing = registry.find(CausalKind.EXECUTOR, executor)
        if (existing > 0L) return existing
        val id = registry.register(CausalKind.EXECUTOR, executor)
        if (id == 0L) {
            capacityLost(epoch)
            return 0L
        }
        emit(epoch, CausalEventObservation(
            kind = CausalKind.EXECUTOR, phase = CausalPhase.Executor.FIRST_OBSERVED,
            id = id, observedNanos = now(),
        ))
        return id
    }

    private fun emitExecutorSite(
        epoch: RuntimeCollectionEpoch, id: Long, phase: Int,
        siteId: Long, source: String, line: Int, origin: Int,
    ) {
        val actor = contexts.captureCausalContext()
        val creating = phase == CausalPhase.Executor.CONSTRUCTED || phase == CausalPhase.Executor.FACTORY_RETURNED
        emit(epoch, CausalEventObservation(
            kind = CausalKind.EXECUTOR, phase = phase, id = id,
            parentKind = if (creating) actor?.entityKind ?: 0 else 0,
            parentId = if (creating) actor?.entityId ?: 0L else 0L,
            actorKind = actor?.entityKind ?: 0, actorId = actor?.entityId ?: 0L,
            observedNanos = now(), sourceSiteId = siteId.coerceAtLeast(0L), sourceMethod = source,
            sourceLine = line.coerceAtLeast(0), sourceOrigin = origin.coerceIn(0, 2),
        ))
    }

    private fun startRun(placement: Placement, identity: Any): RunToken? {
        val epoch = matchingEpoch(placement.epochId) ?: return null
        if (epoch.causalRegistry?.find(CausalKind.EXECUTOR_TASK, identity) != placement.taskId) {
            epoch.writer.recordQuality(QualityCounterId.CAUSAL_BINDING_MISS)
            return null
        }
        val ordinal = placement.occurrences.getAndIncrement()
        if (placement.seriesId == 0L && ordinal != 1L) return null
        val taskId = if (ordinal == 1L) placement.taskId else epoch.causalRegistry.newId()
        if (taskId == 0L) return null
        val started = now()
        val tid = Process.myTid().toLong().coerceAtLeast(0L)
        val cpu = Debug.threadCpuTimeNanos()
        val previous = contexts.swapTrustedContext(CausalKind.EXECUTOR_TASK, taskId)
        try {
            emit(epoch, CausalEventObservation(
                kind = CausalKind.EXECUTOR_TASK, phase = CausalPhase.ExecutorTask.RUN_STARTED,
                id = taskId, parentKind = if (placement.seriesId > 0L) CausalKind.SERIES else 0,
                parentId = placement.seriesId, schedulerKind = CausalKind.EXECUTOR,
                schedulerId = placement.schedulerId, observedNanos = started,
                targetJavaId = observedThreadId(Thread.currentThread()), targetTid = tid,
                occurrence = if (placement.seriesId > 0L) ordinal else -1L,
            ))
        } catch (throwable: Throwable) {
            contexts.restoreTrustedContext(previous)
            throw throwable
        }
        return RunToken(epoch.id, taskId, started, tid, cpu, previous, placement)
    }

    private fun endRun(value: Any?, failed: Boolean) {
        val token = value as? RunToken ?: return
        contexts.restoreTrustedContext(token.previous)
        val epoch = matchingEpoch(token.epochId) ?: return
        val ended = now()
        val tid = Process.myTid().toLong().coerceAtLeast(0L)
        val cpu = Debug.threadCpuTimeNanos()
        try {
            emit(epoch, CausalEventObservation(
                kind = CausalKind.EXECUTOR_TASK, phase = CausalPhase.ExecutorTask.RUN_ENDED,
                id = token.taskId, observedNanos = ended, targetTid = tid,
                durationNanos = (ended - token.started).coerceAtLeast(0L),
                cpuNanos = if (tid > 0L && tid == token.tid && token.cpu >= 0L && cpu >= token.cpu)
                    cpu - token.cpu else -1L,
                terminalOutcome = if (failed) 2 else 1,
            ))
        } finally {
            if (token.placement.seriesId == 0L) finishOneShot(epoch, token.placement)
        }
    }

    private fun finishOneShot(epoch: RuntimeCollectionEpoch, placement: Placement) {
        tryDiagnosticLock(placement.lock, Unit) {
            placement.finished = true
            val alias = placement.futureAlias?.get()
            placement.futureAlias = null
            val registry = epoch.causalRegistry ?: return
            if (alias != null && registry.find(CausalKind.EXECUTOR_TASK, alias) == placement.taskId) {
                registry.release(CausalKind.EXECUTOR_TASK, alias)
            }
        }
    }

    private fun releaseRunBinding(placement: Placement, identity: Any) {
        if (placement.seriesId > 0L) return
        val epoch = matchingEpoch(placement.epochId) ?: return
        val registry = epoch.causalRegistry ?: return
        if (registry.find(CausalKind.EXECUTOR_TASK, identity) == placement.taskId) {
            registry.release(CausalKind.EXECUTOR_TASK, identity)
        }
    }

    private fun epoch(): RuntimeCollectionEpoch? =
        access.collectionEpoch?.takeIf { it.config.concurrencyDiagnosticsEnabled() && it.writer.isAcceptingEvents() }

    private fun matchingEpoch(expected: Long): RuntimeCollectionEpoch? = epoch()?.takeIf { it.id == expected }

    private fun capacityLost(epoch: RuntimeCollectionEpoch) {
        epoch.writer.recordQuality(QualityCounterId.CAUSAL_REGISTRY_CAPACITY)
    }

    private fun emit(epoch: RuntimeCollectionEpoch, event: CausalEventObservation) {
        epoch.writer.causalEvent(event)
    }

    private fun now(): Long = SystemClock.elapsedRealtimeNanos().coerceAtLeast(0L)

    private class CallFrames(val items: ArrayDeque<CallFrame> = ArrayDeque(), var overflow: Int = 0)
    internal class TrackedHandoff(
        val runnable: Runnable,
        internal val frame: Any? = null,
        internal val ownsFrame: Boolean = false,
        internal val overflow: Boolean = false,
        internal val original: Runnable = runnable,
    )

    private class CallFrame(
        val epochId: Long, val executor: Any?, val prepared: Any?,
        var placement: Placement? = null, var claimedByFacade: Boolean = false,
    )
    private class Placement(
        val epochId: Long, val taskId: Long, val seriesId: Long, val schedulerId: Long,
        val parentKind: Int, val parentId: Long, val prepared: Any,
        val occurrences: AtomicLong = AtomicLong(1L),
    ) {
        val lock = ReentrantLock()
        var finished: Boolean = false
        var futureAlias: java.lang.ref.WeakReference<Any>? = null
    }
    private class Target(val kind: Int, val id: Long)
    private class RunToken(
        val epochId: Long, val taskId: Long, val started: Long, val tid: Long,
        val cpu: Long, val previous: Long, val placement: Placement,
    )

    private inner class CausalBinding(
        private val placement: Placement,
        private val identity: Any,
    ) : RuntimeCausalExecutionBinding {
        override val recurring: Boolean get() = placement.seriesId > 0L
        override fun enter(): Any? = startRun(placement, identity)
        override fun exit(token: Any?, failed: Boolean) {
            try {
                endRun(token, failed)
            } finally {
                releaseRunBinding(placement, identity)
            }
        }
    }

    private inner class TrackedRunnable(
        private val delegate: Runnable,
        private val placement: Placement,
    ) : Runnable {
        override fun run() {
            val token = RuntimeHookGuard.value<Any?>(null) { startRun(placement, this) }
            var failed = false
            try {
                delegate.run()
            } catch (throwable: Throwable) {
                failed = true
                throw throwable
            } finally {
                RuntimeHookGuard.run {
                    try {
                        endRun(token, failed)
                    } finally {
                        releaseRunBinding(placement, this)
                    }
                }
            }
        }
    }

    private companion object { const val MAX_FRAMES = 128 }
}
