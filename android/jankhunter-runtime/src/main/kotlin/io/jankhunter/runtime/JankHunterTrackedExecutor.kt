package io.jankhunter.runtime

import java.util.concurrent.Executor

/** Opt-in measured Executor boundary; callers knowingly allow wrapping the submitted command. */
class JankHunterTrackedExecutor private constructor(
    private val delegate: Executor,
    private val telemetryProvider: () -> RuntimeConcurrencyTelemetry,
) : Executor {
    constructor(delegate: Executor) : this(delegate, JankHunter::concurrencyTelemetry)

    internal constructor(delegate: Executor, telemetry: RuntimeConcurrencyTelemetry) : this(delegate, { telemetry })

    override fun execute(command: Runnable) {
        val telemetry = telemetryProvider()
        val handoff = RuntimeHookGuard.value(RuntimeCausalExecutorTelemetry.TrackedHandoff(command)) {
            telemetry.beginTrackedExecutorHandoff(this, command)
        }
        try {
            delegate.execute(handoff.runnable)
        } catch (throwable: Throwable) {
            RuntimeHookGuard.run { telemetry.finishTrackedExecutorHandoff(handoff, true) }
            throw throwable
        }
        RuntimeHookGuard.run { telemetry.finishTrackedExecutorHandoff(handoff, false) }
    }
}
