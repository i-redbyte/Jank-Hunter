package io.jankhunter.runtime

import android.os.Looper
import io.jankhunter.runtime.internal.io.SymbolOrigin
import java.util.concurrent.Callable
import java.util.concurrent.atomic.AtomicLong

internal class RuntimeContextTelemetry(
    private val state: RuntimeState,
    private val contexts: ContextTracker,
    private val access: RuntimeTelemetryAccess,
    private val callGraph: RuntimeCallGraph,
    private val elapsedRealtimeMs: RuntimeLongSource,
) {
    private val stallIds = AtomicLong()

    fun nextMainThreadStallId(): Long = stallIds.incrementAndGet().also { check(it > 0L) }

    fun bindMainThreadStallCallbacks(): MainThreadStallCallbacks {
        val writer = access.writer
        return object : MainThreadStallCallbacks {
            private var foreground = access.isUiVisible()

            override fun nextMainThreadStallId(): Long = this@RuntimeContextTelemetry.nextMainThreadStallId()

            override fun captureMainThreadStallContext(owner: String?): JankHunterContextSnapshot {
                if (writer !== access.writer) return JankHunterContextSnapshot(null, owner)
                foreground = access.isUiVisible()
                return this@RuntimeContextTelemetry.captureMainThreadStallContext(owner)
            }

            override fun recordMainThreadStall(
                context: JankHunterContextSnapshot, stackHint: String?, durationMs: Long,
                incidentId: Long, state: MainThreadStallState,
            ) {
                writer?.updateProducerContext(context.screen, context.owner, context.operationId)
                writer?.stall(context.screen, context.owner, stackHint, durationMs, foreground, incidentId, state.wireValue, SymbolOrigin.RUNTIME_STACK)
                // Make the first observation available in an open log, without main-thread recovery.
                writer?.flush()
            }
        }
    }

    fun withOwner(ownerName: String?, runnable: Runnable) {
        val start = elapsedRealtimeMs.getAsLong()
        try {
            access.callWithOwner(ownerName) { runnable.run() }
        } finally {
            val durationMs = elapsedRealtimeMs.getAsLong() - start
            if (shouldRecordOwnerStall(durationMs)) {
                recordStall(ownerName, "explicit_owner_block", durationMs, SymbolOrigin.SOURCE_LABEL)
            }
        }
    }

    fun <T> withOwner(ownerName: String?, callable: Callable<T>): T {
        val start = elapsedRealtimeMs.getAsLong()
        try {
            return access.callWithOwner(ownerName) { callable.call() }
        } finally {
            val durationMs = elapsedRealtimeMs.getAsLong() - start
            if (shouldRecordOwnerStall(durationMs)) {
                recordStall(ownerName, "explicit_owner_block", durationMs, SymbolOrigin.SOURCE_LABEL)
            }
        }
    }

    fun enterAnnotated(
        screenName: String?,
        ownerName: String?,
    ): Any? {
        if (!isActiveForCallbacks()) return null
        val token = RuntimeHookGuard.value<JankHunterAnnotationScope?>(null) {
            contexts.enterScopedContext(screenName, ownerName)
        }
        RuntimeHookGuard.run { access.ensureContextRecorded() }
        return token
    }

    fun exitAnnotated(token: Any?) {
        if (token !is JankHunterAnnotationScope) return
        RuntimeHookGuard.run { contexts.exitScopedContext(token) }
        RuntimeHookGuard.run { access.ensureContextRecorded() }
    }

    fun captureSnapshot(
        collectionEpochId: Long = access.collectionEpoch?.id ?: 0L,
        httpToken: Long = 0L,
    ): JankHunterContextSnapshot {
        val context = access.captureContext()
        return JankHunterContextSnapshot(
            context.screen,
            context.owner,
            callGraph.hasCurrentMethod(),
            callGraph.currentMethodId(),
            callGraph.currentMethodName(),
            operationId = context.operationId,
            collectionEpochId = collectionEpochId,
            httpToken = httpToken,
        )
    }

    fun setScreen(screenName: String?) {
        contexts.setScreen(screenName)
        access.ensureContextRecorded()
    }

    fun setUiVisible(visible: Boolean) {
        state.uiVisibility.set(
            if (visible) RuntimeUiVisibility.VISIBLE.wireValue else RuntimeUiVisibility.HIDDEN.wireValue,
        )
        state.memorySampler?.onUserRelevanceChanged()
        state.systemContextSampler?.onUserRelevanceChanged()
    }

    fun recordStall(owner: String?, stackHint: String?, durationMs: Long, stackOrigin: SymbolOrigin = SymbolOrigin.UNKNOWN) {
        val attributedOwner = firstContextValue(owner, contexts.ownerOrNull())
        val context = access.captureContext(ownerOverride = attributedOwner)
        access.ensureContextRecorded(screenOverride = context.screen, ownerOverride = context.owner)
        access.writer?.stall(
            context.screen,
            context.owner,
            stackHint,
            durationMs,
            foreground = access.isUiVisible(),
            stackOrigin = stackOrigin,
        )
    }

    fun captureMainThreadStallContext(owner: String?): JankHunterContextSnapshot {
        val context = state.mainThreadContext ?: access.captureContext(ownerOverride = owner)
        if (state.heapDumpInProgress.get() ||
            elapsedRealtimeMs.getAsLong() <= state.heapDumpAttributionUntilMs.get()
        ) {
            return JankHunterContextSnapshot(
                context.screen,
                "jankhunter.heap_dump",
                operationId = context.operationId,
            )
        }
        return JankHunterContextSnapshot(
            context.screen,
            firstContextValue(context.owner, owner),
            operationId = context.operationId,
        )
    }

    fun recordMainThreadStall(
        contextSnapshot: JankHunterContextSnapshot,
        stackHint: String?,
        durationMs: Long,
        incidentId: Long,
        stallState: MainThreadStallState,
    ) {
        val context = contextSnapshot.asRuntimeContext()
        val writer = access.writer
        writer?.updateProducerContext(context.screen, context.owner, context.operationId)
        writer?.stall(
            context.screen,
            context.owner,
            stackHint,
            durationMs,
            foreground = access.isUiVisible(),
            incidentId = incidentId,
            state = stallState.wireValue,
            stackOrigin = SymbolOrigin.RUNTIME_STACK,
        )
    }

    fun isActiveForCallbacks(): Boolean {
        return RuntimeHookGuard.value(false) { access.isActive() }
    }

    private fun shouldRecordOwnerStall(durationMs: Long): Boolean {
        val mainLooper = Looper.getMainLooper() ?: return false
        return isMainThreadOwnerBlock(
            durationMs = durationMs,
            thresholdMs = access.config?.ownerBlockThresholdMs() ?: DEFAULT_OWNER_BLOCK_THRESHOLD_MS,
            isMainThread = Looper.myLooper() === mainLooper,
            monitorActive = state.watchdog != null,
        )
    }

    private companion object {
        const val DEFAULT_OWNER_BLOCK_THRESHOLD_MS = 250L
    }
}
