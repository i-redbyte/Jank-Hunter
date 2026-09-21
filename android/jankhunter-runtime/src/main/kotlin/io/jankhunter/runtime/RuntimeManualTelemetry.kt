package io.jankhunter.runtime

import java.util.concurrent.Callable

/** Cohesive application-owned telemetry; automatic collectors use separate callback ports. */
internal class RuntimeManualTelemetry(
    private val contexts: ContextTracker,
    private val contextTelemetry: RuntimeContextTelemetry,
    private val operations: RuntimeOperationTelemetry,
    private val retention: RuntimeRetentionTelemetry,
    private val system: RuntimeSystemTelemetry,
    private val io: RuntimeIOTelemetry,
    private val semantic: RuntimeSemanticTelemetry,
) {
    fun setScreen(screenName: String?) = contextTelemetry.setScreen(screenName)

    fun currentScreen(): String = contexts.currentScreen()

    fun currentOwner(): String = contexts.currentOwner()

    fun captureContext(): JankHunterContextSnapshot = contextTelemetry.captureSnapshot()

    fun withOwner(ownerName: String?, runnable: Runnable) = contextTelemetry.withOwner(ownerName, runnable)

    fun <T> withOwner(ownerName: String?, callable: Callable<T>): T {
        return contextTelemetry.withOwner(ownerName, callable)
    }

    fun enterContext(screenName: String?, ownerName: String?): Any? {
        return contextTelemetry.enterAnnotated(screenName, ownerName)
    }

    fun exitContext(token: Any?) = contextTelemetry.exitAnnotated(token)

    fun startOperation(
        name: String,
        kind: JankHunterOperationKind,
        budgetMs: Long,
        attributes: JankHunterOperationAttributes,
    ): JankHunterOperation = operations.start(name, kind, budgetMs, attributes)

    fun counter(name: String?, delta: Long) = system.recordCounter(name, delta)

    fun gauge(name: String?, value: Long) = system.recordGauge(name, value)

    fun watch(instance: Any?, description: String?, ownerHint: String?) {
        retention.watchObject(instance, description, ownerHint)
    }

    fun recordLog(ownerName: String?, source: String?, level: Int) {
        system.recordLogSpam(ownerName, source, level)
    }

    fun recordIO(
        operation: JankHunterIOOperation,
        durationNanos: Long,
        bytes: Long,
        ownerName: String?,
        outcome: JankHunterIOOutcome,
    ) = io.record(operation, durationNanos, bytes, ownerName, outcome)

    fun recordCompose(phase: JankHunterComposePhase, name: String, durationNanos: Long) {
        semantic.recordCompose(phase, name, durationNanos)
    }
}
