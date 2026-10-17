package io.jankhunter.runtime

/** Narrow runtime port used by the optional WorkManager adapter. */
object JankHunterWorkerRuntime {
    @JvmSynthetic
    fun isConcurrencyActive(): Boolean = RuntimeHookGuard.value(false) {
        JankHunter.concurrencyTelemetry().isEnabled()
    }

    @JvmSynthetic
    fun beginConcurrencyWorker(
        worker: Any?, instanceId: Long, runAttempt: Int, generation: Int, uuidMost: Long, uuidLeast: Long,
    ): JankHunterCausalContext? = RuntimeHookGuard.value(null) {
        JankHunter.concurrencyTelemetry().beginConcurrencyWorker(worker, instanceId, runAttempt, generation, uuidMost, uuidLeast)
    }

    @JvmSynthetic
    fun installConcurrencyContext(context: JankHunterCausalContext): Long = RuntimeHookGuard.value(0L) {
        JankHunter.concurrencyTelemetry().installConcurrencyContext(context)
    }

    @JvmSynthetic
    fun restoreConcurrencyContext(previous: Long) = RuntimeHookGuard.run {
        JankHunter.concurrencyTelemetry().restoreConcurrencyContext(previous)
    }

    @JvmSynthetic
    fun endConcurrencyWorker(worker: Any?, context: JankHunterCausalContext, outcome: JankHunterWorkerOutcome) =
        RuntimeHookGuard.run {
            JankHunter.concurrencyTelemetry().endConcurrencyWorker(worker, context, outcome)
        }

    @JvmSynthetic
    fun isActive(): Boolean = RuntimeHookGuard.value(false) { JankHunter.workerTelemetry().isEnabled() }

    @JvmSynthetic
    fun instanceId(mostSignificantBits: Long, leastSignificantBits: Long): Long {
        return JankHunter.workerTelemetry().instanceId(mostSignificantBits, leastSignificantBits)
    }

    @JvmSynthetic
    fun classify(result: Any?): JankHunterWorkerOutcome = JankHunter.workerTelemetry().classifyResult(result)

    @JvmSynthetic
    fun submitted(most: Long, least: Long, periodic: Boolean, uniqueName: String?, policy: String?, omitted: Int) =
        JankHunter.workerTelemetry().submitted(most, least, periodic, uniqueName, policy, omitted)

    @JvmSynthetic
    fun registrationEpoch(): Long = JankHunter.workerTelemetry().registrationEpoch()

    /** The lease stops publication immediately; onStopped is dispatched asynchronously on a bounded lane. */
    @JvmSynthetic
    fun observeRegistration(expectedEpoch: Long, onStopped: Runnable): JankHunterWorkRegistration? =
        JankHunter.workerTelemetry().observeRegistration(expectedEpoch, onStopped)

    @JvmSynthetic
    fun submittedForEpoch(epoch: Long, most: Long, least: Long, periodic: Boolean, uniqueName: String?, policy: String?, omitted: Int) =
        JankHunter.workerTelemetry().submitted(most, least, periodic, uniqueName, policy, omitted, epoch)

    @JvmSynthetic
    fun registeredObserved(instanceId: Long, periodic: Boolean) =
        JankHunter.workerTelemetry().registeredObserved(instanceId, periodic)

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
    fun failed(token: Long, instanceId: Long, runAttempt: Int, generation: Int, throwable: Throwable) =
        RuntimeHookGuard.run { JankHunter.workerTelemetry().failed(token, instanceId, runAttempt, generation, throwable) }

    @JvmSynthetic
    fun progressRequested(token: Long, instanceId: Long, runAttempt: Int, generation: Int, observedNanos: Long,
                          fields: List<JankHunterWorkDataField>, omittedKeys: Int, truncatedFields: Int) =
        RuntimeHookGuard.run { JankHunter.workerTelemetry().progressRequested(token, instanceId, runAttempt,
            generation, observedNanos, fields, omittedKeys, truncatedFields) }

    @JvmSynthetic
    fun progressOmitted(token: Long, instanceId: Long, runAttempt: Int, generation: Int,
                        observedNanos: Long, omittedUpdates: Int) =
        RuntimeHookGuard.run { JankHunter.workerTelemetry().progressOmitted(token, instanceId, runAttempt,
            generation, observedNanos, omittedUpdates) }

    @JvmSynthetic
    fun foreground(token: Long, instanceId: Long, workerName: String, runAttempt: Int,
                   generation: Int, entered: Boolean) = RuntimeHookGuard.run {
        JankHunter.workerTelemetry().foreground(token, instanceId, workerName, runAttempt, generation, entered)
    }

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
