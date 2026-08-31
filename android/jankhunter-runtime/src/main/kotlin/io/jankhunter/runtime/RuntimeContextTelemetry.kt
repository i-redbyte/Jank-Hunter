package io.jankhunter.runtime

import android.os.Looper
import java.util.concurrent.Callable

internal class RuntimeContextTelemetry(
    private val state: RuntimeState,
    private val contexts: ContextTracker,
    private val access: RuntimeTelemetryAccess,
    private val callGraph: RuntimeCallGraph,
    private val elapsedRealtimeMs: RuntimeLongSource,
) {
    fun withOwner(ownerName: String?, runnable: Runnable) {
        val start = elapsedRealtimeMs.getAsLong()
        try {
            access.callWithOwner(ownerName) { runnable.run() }
        } finally {
            val durationMs = elapsedRealtimeMs.getAsLong() - start
            if (shouldRecordOwnerStall(durationMs)) {
                recordStall(ownerName, "explicit_owner_block", durationMs)
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
                recordStall(ownerName, "explicit_owner_block", durationMs)
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

    fun captureSnapshot(): JankHunterContextSnapshot {
        val context = access.captureContext()
        return JankHunterContextSnapshot(
            context.screen,
            context.owner,
            callGraph.hasCurrentMethod(),
            callGraph.currentMethodId(),
            callGraph.currentMethodName(),
            operationId = context.operationId,
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

    fun recordStall(owner: String?, stackHint: String?, durationMs: Long) {
        val attributedOwner = firstContextValue(owner, contexts.ownerOrNull())
        val context = access.captureContext(ownerOverride = attributedOwner)
        access.ensureContextRecorded(screenOverride = context.screen, ownerOverride = context.owner)
        access.writer?.stall(
            context.screen,
            context.owner,
            stackHint,
            durationMs,
            foreground = access.isUiVisible(),
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
    ) {
        val context = contextSnapshot.asRuntimeContext()
        access.writer?.stall(
            context.screen,
            context.owner,
            stackHint,
            durationMs,
            foreground = access.isUiVisible(),
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
