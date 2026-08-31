package io.jankhunter.runtime

import android.os.Looper
import android.os.SystemClock

internal class RuntimeSemanticTelemetry(
    private val access: RuntimeTelemetryAccess,
    private val runtimeCallGraph: RuntimeCallGraph,
) {
    fun enter(kind: Int): Long {
        return RuntimeHookGuard.value(0L) {
            if (!access.isActive() || !isEnabled(kind)) 0L else {
                SystemClock.elapsedRealtimeNanos().coerceAtLeast(1L)
            }
        }
    }

    fun exit(token: Long, kind: Int, methodId: Long, methodName: String, outcome: Int) {
        RuntimeHookGuard.run {
            if (token == 0L || !isEnabled(kind)) return@run
            recordBoundary(
                kind = kind,
                calleeId = methodId,
                calleeName = methodName,
                durationNanos = (SystemClock.elapsedRealtimeNanos() - token).coerceAtLeast(0L),
                outcome = workerOutcomeFromCode(outcome),
            )
        }
    }

    fun recordCompose(phase: JankHunterComposePhase, name: String, durationNanos: Long) {
        RuntimeHookGuard.run {
            val kind = phase.semanticKind()
            if (!isEnabled(kind)) return@run
            recordNamedBoundary(kind, name, durationNanos)
        }
    }

    fun <T> traceCompose(phase: JankHunterComposePhase, name: String, block: () -> T): T {
        if (!isEnabled(phase.semanticKind())) return block()
        val startedAt = SystemClock.elapsedRealtimeNanos()
        try {
            return block()
        } finally {
            recordCompose(phase, name, SystemClock.elapsedRealtimeNanos() - startedAt)
        }
    }

    fun isEnabled(kind: Int): Boolean {
        val active = access.config ?: return false
        return when (kind) {
            JankHunterSemanticWork.COMPOSE_COMPOSITION,
            JankHunterSemanticWork.COMPOSE_MEASURE,
            JankHunterSemanticWork.COMPOSE_LAYOUT,
            JankHunterSemanticWork.COMPOSE_DRAW,
            -> active.composeTracingEnabled()
            JankHunterSemanticWork.ROOM_DAO -> active.roomTracingEnabled()
            JankHunterSemanticWork.WORKER -> active.workerTracingEnabled()
            else -> false
        }
    }

    fun recordNamedBoundary(
        kind: Int,
        name: String,
        durationNanos: Long,
        outcome: JankHunterWorkerOutcome? = null,
    ) {
        val normalized = name.trim().takeIf(String::isNotEmpty) ?: UNKNOWN
        val targetId = JankHunterSemanticWork.stableId("jankhunter.semantic.target.v1\u0000$normalized")
        recordBoundary(kind, targetId, normalized, durationNanos, outcome)
    }

    fun recordBoundary(
        kind: Int,
        calleeId: Long,
        calleeName: String,
        durationNanos: Long,
        outcome: JankHunterWorkerOutcome?,
    ) {
        val mainLooper = Looper.getMainLooper()
        val mainThread = mainLooper != null && Looper.myLooper() === mainLooper
        val callerName = JankHunterSemanticWork.callerLabel(kind, mainThread, outcome) ?: return
        runtimeCallGraph.recordSemantic(
            callerId = JankHunterSemanticWork.stableId(callerName),
            callerName = callerName,
            calleeId = calleeId,
            calleeName = calleeName,
            durationMs = durationNanos.coerceAtLeast(0L) / NANOS_PER_MILLISECOND,
            enabled = access.isActive() && isEnabled(kind),
        )
    }

    private fun JankHunterComposePhase.semanticKind(): Int {
        return when (this) {
            JankHunterComposePhase.COMPOSITION -> JankHunterSemanticWork.COMPOSE_COMPOSITION
            JankHunterComposePhase.MEASURE -> JankHunterSemanticWork.COMPOSE_MEASURE
            JankHunterComposePhase.LAYOUT -> JankHunterSemanticWork.COMPOSE_LAYOUT
            JankHunterComposePhase.DRAW -> JankHunterSemanticWork.COMPOSE_DRAW
        }
    }

    private companion object {
        const val NANOS_PER_MILLISECOND = 1_000_000L
        const val UNKNOWN = "unknown"
    }
}

internal fun workerOutcomeFromCode(value: Int): JankHunterWorkerOutcome {
    return when (value) {
        JankHunterWorkerOutcome.SUCCESS.code -> JankHunterWorkerOutcome.SUCCESS
        JankHunterWorkerOutcome.FAILURE.code -> JankHunterWorkerOutcome.FAILURE
        JankHunterWorkerOutcome.RETRY.code -> JankHunterWorkerOutcome.RETRY
        JankHunterWorkerOutcome.CANCELLED.code -> JankHunterWorkerOutcome.CANCELLED
        else -> JankHunterWorkerOutcome.UNKNOWN
    }
}
