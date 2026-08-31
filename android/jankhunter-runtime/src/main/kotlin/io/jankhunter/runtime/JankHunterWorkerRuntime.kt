package io.jankhunter.runtime

/** Narrow runtime port used by the optional WorkManager adapter. */
object JankHunterWorkerRuntime {
    @JvmSynthetic
    fun isActive(): Boolean = RuntimeHookGuard.value(false) { JankHunter.workerTelemetry().isEnabled() }

    @JvmSynthetic
    fun instanceId(mostSignificantBits: Long, leastSignificantBits: Long): Long {
        return JankHunter.workerTelemetry().instanceId(mostSignificantBits, leastSignificantBits)
    }

    @JvmSynthetic
    fun classify(result: Any?): JankHunterWorkerOutcome = JankHunter.workerTelemetry().classifyResult(result)

    @JvmSynthetic
    fun enqueued(instanceId: Long, periodic: Boolean) = JankHunter.workerTelemetry().enqueued(instanceId, periodic)

    @JvmSynthetic
    fun started(
        instanceId: Long,
        workerName: String,
        runAttempt: Int,
        generation: Int,
    ): Long = JankHunter.workerTelemetry().started(instanceId, workerName, runAttempt, generation)

    @JvmSynthetic
    fun finished(
        token: Long,
        instanceId: Long,
        workerName: String,
        outcome: JankHunterWorkerOutcome,
        runAttempt: Int,
        generation: Int,
        stopReason: Int,
        stopReasonKnown: Boolean,
    ) = JankHunter.workerTelemetry().finished(
        token,
        instanceId,
        workerName,
        outcome,
        runAttempt,
        generation,
        stopReason,
        stopReasonKnown,
    )
}
