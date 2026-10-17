package io.jankhunter.runtime

import android.os.Debug
import android.os.Process
import android.os.SystemClock
import io.jankhunter.runtime.internal.io.CausalEventObservation
import io.jankhunter.runtime.internal.io.CausalKind
import io.jankhunter.runtime.internal.io.CausalPhase
import io.jankhunter.runtime.internal.io.QualityCounterId
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean

/** Passive platform Handler calls and exact dispatch only for the explicit tracked facade. */
internal class RuntimeCausalHandlerTelemetry(
    private val access: RuntimeTelemetryAccess,
    private val contexts: RuntimeConcurrencyTelemetry,
) {
    fun beginPost(handler: Any?, callback: Runnable?, siteId: Long, source: String, line: Int,
                  origin: Int, requestedDelayMillis: Long): Long {
        if (handler == null || callback == null) return 0L
        val epoch = activeEpoch() ?: return 0L
        val id = epoch.causalRegistry?.newId() ?: return 0L
        emitPostInvoked(epoch, id, siteId, source, line, origin, requestedDelayMillis)
        return id
    }

    fun postReturned(handler: Any?, callback: Runnable?, token: Long, posted: Boolean) {
        if (handler == null || callback == null) return
        finishPost(token, CausalPhase.HandlerPost.POST_RETURNED, if (posted) 1 else 2)
    }

    fun postThrew(handler: Any?, callback: Runnable?, token: Long) {
        if (handler == null || callback == null) return
        finishPost(token, CausalPhase.HandlerPost.POST_THREW, 3)
    }

    fun prepareTrackedPost(callback: Runnable, siteId: Long, source: String, line: Int,
                           origin: Int, requestedDelayMillis: Long): PreparedPost {
        val epoch = activeEpoch() ?: return PreparedPost(0L, 0L, callback, false)
        val registry = epoch.causalRegistry ?: return PreparedPost(0L, 0L, callback, false)
        val id = registry.newId()
        if (id == 0L) return PreparedPost(0L, 0L, callback, false)
        if (!access.usesTaskWrappers) {
            emitPostInvoked(epoch, id, siteId, source, line, origin, requestedDelayMillis)
            return PreparedPost(epoch.id, id, callback, false)
        }
        val wrapper = TrackedCallback(this, callback, epoch.id, id)
        val tracked = registry.bind(CausalKind.HANDLER_POST, wrapper, id)
        if (!tracked) epoch.writer.recordQuality(QualityCounterId.CAUSAL_REGISTRY_CAPACITY)
        val prepared = PreparedPost(epoch.id, id, if (tracked) wrapper else callback, tracked)
        emitPostInvoked(epoch, id, siteId, source, line, origin, requestedDelayMillis)
        return prepared
    }

    fun trackedPostReturned(prepared: PreparedPost, posted: Boolean) {
        val epoch = matchingEpoch(prepared.epochId, prepared.id) ?: return
        if (!posted && prepared.tracked) epoch.causalRegistry?.release(CausalKind.HANDLER_POST, prepared.callback)
        emit(epoch, CausalEventObservation(
            kind = CausalKind.HANDLER_POST, phase = CausalPhase.HandlerPost.POST_RETURNED,
            id = prepared.id, observedNanos = now(), callResult = if (posted) 1 else 2,
        ))
    }

    fun trackedPostThrew(prepared: PreparedPost) {
        val epoch = matchingEpoch(prepared.epochId, prepared.id) ?: return
        if (prepared.tracked) epoch.causalRegistry?.release(CausalKind.HANDLER_POST, prepared.callback)
        emit(epoch, CausalEventObservation(
            kind = CausalKind.HANDLER_POST, phase = CausalPhase.HandlerPost.POST_THREW,
            id = prepared.id, observedNanos = now(), callResult = 3,
        ))
    }

    fun removalRequested(epochId: Long, id: Long): Long {
        val epoch = matchingEpoch(epochId, id) ?: return 0L
        val actor = contexts.captureCausalContext()
        emit(epoch, CausalEventObservation(
            kind = CausalKind.HANDLER_POST, phase = CausalPhase.HandlerPost.REMOVE_REQUESTED,
            id = id, observedNanos = now(), actorKind = actor?.entityKind ?: 0,
            actorId = actor?.entityId ?: 0L,
        ))
        return id
    }

    fun removalReturned(epochId: Long, token: Long, threw: Boolean) {
        val epoch = matchingEpoch(epochId, token) ?: return
        emit(epoch, CausalEventObservation(
            kind = CausalKind.HANDLER_POST,
            phase = if (threw) CausalPhase.HandlerPost.REMOVE_THREW else CausalPhase.HandlerPost.REMOVE_RETURNED,
            id = token, observedNanos = now(), callResult = if (threw) 3 else 1,
        ))
    }

    private fun beginDispatch(callback: TrackedCallback): Dispatch? {
        if (!callback.claim()) return null
        val epoch = matchingEpoch(callback.epochId, callback.id) ?: return null
        if (epoch.causalRegistry?.find(CausalKind.HANDLER_POST, callback) != callback.id) {
            epoch.writer.recordQuality(QualityCounterId.CAUSAL_BINDING_MISS)
            return null
        }
        val started = now()
        val tid = Process.myTid().toLong().coerceAtLeast(0L)
        val cpu = Debug.threadCpuTimeNanos()
        emit(epoch, CausalEventObservation(
            kind = CausalKind.HANDLER_POST, phase = CausalPhase.HandlerPost.DISPATCH_STARTED,
            id = callback.id, observedNanos = started,
            targetJavaId = observedThreadId(Thread.currentThread()), targetTid = tid,
        ))
        val previous = contexts.swapTrustedContext(CausalKind.HANDLER_POST, callback.id)
        return Dispatch(epoch.id, callback.id, callback, previous, started, tid, cpu)
    }

    private fun endDispatch(dispatch: Dispatch, failed: Boolean) {
        contexts.restoreTrustedContext(dispatch.previous)
        val epoch = matchingEpoch(dispatch.epochId, dispatch.id) ?: return
        epoch.causalRegistry?.release(CausalKind.HANDLER_POST, dispatch.callback)
        val ended = now()
        val tid = Process.myTid().toLong().coerceAtLeast(0L)
        val cpu = Debug.threadCpuTimeNanos()
        emit(epoch, CausalEventObservation(
            kind = CausalKind.HANDLER_POST, phase = CausalPhase.HandlerPost.DISPATCH_ENDED,
            id = dispatch.id, observedNanos = ended, targetTid = tid,
            durationNanos = (ended - dispatch.started).coerceAtLeast(0L),
            cpuNanos = if (tid > 0L && tid == dispatch.tid && dispatch.cpu >= 0L && cpu >= dispatch.cpu)
                cpu - dispatch.cpu else -1L,
            terminalOutcome = if (failed) 2 else 1,
        ))
    }

    private fun emitPostInvoked(epoch: RuntimeCollectionEpoch, id: Long, siteId: Long,
                                source: String, line: Int, origin: Int, requestedDelayMillis: Long) {
        val parent = contexts.captureCausalContext()
        val method = source.takeIf(String::isNotBlank)?.take(512)
        emit(epoch, CausalEventObservation(
            kind = CausalKind.HANDLER_POST, phase = CausalPhase.HandlerPost.POST_INVOKED, id = id,
            parentKind = parent?.entityKind ?: 0, parentId = parent?.entityId ?: 0L,
            actorKind = parent?.entityKind ?: 0, actorId = parent?.entityId ?: 0L,
            observedNanos = now(), sourceSiteId = siteId.coerceAtLeast(0L), sourceMethod = method,
            sourceLine = line.coerceAtLeast(0), sourceOrigin = if (method == null) 0 else origin.coerceIn(0, 2),
            requestedDelayNanos = if (requestedDelayMillis == Long.MIN_VALUE) -1L
                else TimeUnit.MILLISECONDS.toNanos(requestedDelayMillis.coerceAtLeast(0L)),
        ))
    }

    private fun finishPost(id: Long, phase: Int, result: Int) {
        if (id == 0L) return
        val epoch = activeEpoch() ?: return
        if (id < epoch.causalIdFloor) {
            epoch.writer.recordQuality(QualityCounterId.CAUSAL_STALE_EPOCH)
            return
        }
        emit(epoch, CausalEventObservation(
            kind = CausalKind.HANDLER_POST, phase = phase, id = id,
            observedNanos = now(), callResult = result,
        ))
    }

    private fun matchingEpoch(epochId: Long, id: Long): RuntimeCollectionEpoch? {
        if (id == 0L) return null
        val epoch = activeEpoch() ?: return null
        if (epoch.id != epochId || id < epoch.causalIdFloor) {
            epoch.writer.recordQuality(QualityCounterId.CAUSAL_STALE_EPOCH)
            return null
        }
        return epoch
    }

    private fun activeEpoch(): RuntimeCollectionEpoch? =
        access.collectionEpoch?.takeIf { it.config.concurrencyDiagnosticsEnabled() && it.writer.isAcceptingEvents() }

    private fun emit(epoch: RuntimeCollectionEpoch, event: CausalEventObservation) {
        epoch.writer.causalEvent(event)
    }

    private fun now(): Long = SystemClock.elapsedRealtimeNanos().coerceAtLeast(0L)

    internal class PreparedPost(val epochId: Long, val id: Long, val callback: Runnable, val tracked: Boolean)

    private class TrackedCallback(
        private val owner: RuntimeCausalHandlerTelemetry,
        private val delegate: Runnable,
        val epochId: Long,
        val id: Long,
    ) : Runnable {
        private val started = AtomicBoolean()

        fun claim(): Boolean = started.compareAndSet(false, true)

        override fun run() {
            val dispatch = RuntimeHookGuard.value<Dispatch?>(null) { owner.beginDispatch(this) }
            var failed = false
            try {
                delegate.run()
            } catch (throwable: Throwable) {
                failed = true
                throw throwable
            } finally {
                if (dispatch != null) RuntimeHookGuard.run { owner.endDispatch(dispatch, failed) }
            }
        }
    }

    private class Dispatch(
        val epochId: Long, val id: Long, val callback: TrackedCallback, val previous: Long,
        val started: Long, val tid: Long, val cpu: Long,
    )
}
