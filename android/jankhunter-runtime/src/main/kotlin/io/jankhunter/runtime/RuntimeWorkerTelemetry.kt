package io.jankhunter.runtime

import android.os.Looper
import android.os.SystemClock
import io.jankhunter.runtime.internal.io.BinaryLogWriter
import io.jankhunter.runtime.internal.io.Jhlog

internal class RuntimeWorkerTelemetry(
    private val access: RuntimeTelemetryAccess,
    private val semantic: RuntimeSemanticTelemetry,
) {
    private val instanceSalt = mixInstanceId(System.nanoTime(), System.identityHashCode(this).toLong())

    fun classifyOutcome(result: Any?): Int = inferOutcome(result).code

    fun classifyResult(result: Any?): JankHunterWorkerOutcome = inferOutcome(result)

    fun instanceId(mostSignificantBits: Long, leastSignificantBits: Long): Long {
        val mixed = mixInstanceId(
            mostSignificantBits xor instanceSalt,
            java.lang.Long.rotateLeft(leastSignificantBits, ID_ROTATION),
        )
        return mixed.nonZero()
    }

    fun enqueued(instanceId: Long, periodic: Boolean) {
        RuntimeHookGuard.run {
            if (!isEnabled() || instanceId == 0L) return@run
            recordEvent(
                workerId = 0L,
                workerName = null,
                instanceId = instanceId,
                stage = Jhlog.WORKER_STAGE_ENQUEUED,
                outcome = Jhlog.WORKER_OUTCOME_UNKNOWN,
                periodic = periodic,
            )
        }
    }

    fun isEnabled(): Boolean = access.isActive() && access.config?.workerTracingEnabled() == true

    fun started(
        instanceId: Long,
        workerName: String,
        runAttempt: Int,
        generation: Int,
    ): Long {
        if (!isEnabled() || instanceId == 0L) return 0L
        val normalized = normalizedName(workerName)
        return started(instanceId, stableId(normalized), normalized, runAttempt, generation)
    }

    fun started(
        instanceId: Long,
        workerId: Long,
        workerName: String,
        runAttempt: Int,
        generation: Int,
    ): Long = RuntimeHookGuard.value(0L) {
        if (!isEnabled() || instanceId == 0L || workerId == 0L) return@value 0L
        val token = SystemClock.elapsedRealtimeNanos().coerceAtLeast(1L)
        recordEvent(
            workerId = workerId,
            workerName = workerName,
            instanceId = instanceId,
            stage = Jhlog.WORKER_STAGE_STARTED,
            outcome = Jhlog.WORKER_OUTCOME_UNKNOWN,
            runAttempt = runAttempt,
            generation = generation,
        )
        token
    }

    fun finished(
        token: Long,
        instanceId: Long,
        workerName: String,
        outcome: JankHunterWorkerOutcome,
        runAttempt: Int,
        generation: Int,
        stopReason: Int,
        stopReasonKnown: Boolean,
    ) {
        if (token == 0L) return
        val normalized = normalizedName(workerName)
        finished(
            token,
            instanceId,
            stableId(normalized),
            normalized,
            outcome,
            runAttempt,
            generation,
            stopReason,
            stopReasonKnown,
        )
    }

    fun finished(
        token: Long,
        instanceId: Long,
        workerId: Long,
        workerName: String,
        outcome: JankHunterWorkerOutcome,
        runAttempt: Int,
        generation: Int,
        stopReason: Int,
        stopReasonKnown: Boolean,
    ) {
        RuntimeHookGuard.run {
            if (token == 0L || !isEnabled() || instanceId == 0L || workerId == 0L) return@run
            val durationNanos = (SystemClock.elapsedRealtimeNanos() - token).coerceAtLeast(0L)
            recordEvent(
                workerId = workerId,
                workerName = workerName,
                instanceId = instanceId,
                stage = Jhlog.WORKER_STAGE_FINISHED,
                outcome = outcome.wireValue(),
                durationMs = durationNanos / NANOS_PER_MILLISECOND,
                runAttempt = runAttempt,
                generation = generation,
                stopReason = stopReason,
                stopReasonKnown = stopReasonKnown,
            )
            semantic.recordBoundary(
                JankHunterSemanticWork.WORKER,
                workerId,
                workerName,
                durationNanos,
                outcome,
            )
        }
    }

    private fun recordEvent(
        workerId: Long,
        workerName: String?,
        instanceId: Long,
        stage: Long,
        outcome: Long,
        durationMs: Long = 0L,
        runAttempt: Int = 0,
        generation: Int = 0,
        stopReason: Int = 0,
        stopReasonKnown: Boolean = false,
        periodic: Boolean = false,
    ) {
        val activeWriter = access.writer ?: return
        var flags = access.uiVisibleFlag()
        val mainLooper = Looper.getMainLooper()
        if (mainLooper != null && Looper.myLooper() === mainLooper) {
            flags = flags or BinaryLogWriter.FLAG_THREAD_MAIN
        }
        if (periodic) flags = flags or Jhlog.FLAG_WORKER_PERIODIC
        if (stopReasonKnown) flags = flags or Jhlog.FLAG_WORKER_STOP_REASON_KNOWN
        activeWriter.worker(
            workerId = workerId,
            workerName = workerName,
            instanceId = instanceId,
            stage = stage,
            outcome = outcome,
            durationMs = durationMs,
            runAttempt = runAttempt.coerceAtLeast(0).toLong(),
            generation = generation.coerceAtLeast(0).toLong(),
            stopReason = if (stopReasonKnown) stopReason.coerceAtLeast(0).toLong() else 0L,
            flags = flags,
        )
    }

    private fun mixInstanceId(first: Long, second: Long): Long {
        var value = first xor java.lang.Long.rotateLeft(second, 31) xor ID_GOLDEN_RATIO
        value = (value xor (value ushr 30)) * ID_MIX_1
        value = (value xor (value ushr 27)) * ID_MIX_2
        return value xor (value ushr 31)
    }

    private fun inferOutcome(result: Any?): JankHunterWorkerOutcome {
        val className = result?.javaClass?.name ?: return JankHunterWorkerOutcome.UNKNOWN
        return when {
            className.endsWith("${'$'}Success") -> JankHunterWorkerOutcome.SUCCESS
            className.endsWith("${'$'}Failure") -> JankHunterWorkerOutcome.FAILURE
            className.endsWith("${'$'}Retry") -> JankHunterWorkerOutcome.RETRY
            else -> JankHunterWorkerOutcome.SUCCESS
        }
    }

    private fun JankHunterWorkerOutcome.wireValue(): Long {
        return when (this) {
            JankHunterWorkerOutcome.SUCCESS -> Jhlog.WORKER_OUTCOME_SUCCESS
            JankHunterWorkerOutcome.FAILURE -> Jhlog.WORKER_OUTCOME_FAILURE
            JankHunterWorkerOutcome.RETRY -> Jhlog.WORKER_OUTCOME_RETRY
            JankHunterWorkerOutcome.CANCELLED -> Jhlog.WORKER_OUTCOME_CANCELLED
            JankHunterWorkerOutcome.UNKNOWN -> Jhlog.WORKER_OUTCOME_UNKNOWN
        }
    }

    private fun normalizedName(name: String): String = name.trim().takeIf(String::isNotEmpty) ?: UNKNOWN

    private fun stableId(name: String): Long = JankHunterSemanticWork.stableId("jankhunter.worker.v1\u0000$name")

    private fun Long.nonZero(): Long = if (this == 0L) 1L else this

    private companion object {
        const val UNKNOWN = "unknown"
        const val NANOS_PER_MILLISECOND = 1_000_000L
        const val ID_ROTATION = 29
        const val ID_GOLDEN_RATIO = -7046029254386353131L
        const val ID_MIX_1 = -4658895280553007687L
        const val ID_MIX_2 = -7723592293110705685L
    }
}
