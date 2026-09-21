package io.jankhunter.runtime

import android.os.SystemClock
import java.util.concurrent.Callable
import java.util.concurrent.Executor
import java.util.concurrent.ExecutorService
import java.util.concurrent.ScheduledExecutorService

/** Manual, application-owned telemetry. Automatic instrumentation uses a separate internal ABI. */
object JankHunterTelemetry {
    @JvmStatic
    fun setScreen(screenName: String?) = JankHunter.manualTelemetry().setScreen(screenName)

    @JvmStatic
    fun currentScreen(): String = JankHunter.manualTelemetry().currentScreen()

    @JvmStatic
    fun currentOwner(): String = JankHunter.manualTelemetry().currentOwner()

    @JvmStatic
    fun contextSnapshot(): JankHunterContextSnapshot = JankHunter.manualTelemetry().captureContext()

    @JvmStatic
    fun withOwner(ownerName: String?, runnable: Runnable) = JankHunter.manualTelemetry().withOwner(ownerName, runnable)

    @JvmStatic
    fun <T> withOwner(ownerName: String?, callable: Callable<T>): T {
        return JankHunter.manualTelemetry().withOwner(ownerName, callable)
    }

    @JvmSynthetic
    inline fun <T> withContext(screenName: String?, ownerName: String?, block: () -> T): T {
        val token = enterContext(screenName, ownerName)
        try {
            return block()
        } finally {
            exitContext(token)
        }
    }

    @JvmStatic
    @JvmOverloads
    fun startOperation(
        name: String,
        kind: JankHunterOperationKind = JankHunterOperationKind.USER,
        budgetMs: Long = 0L,
        attributes: JankHunterOperationAttributes = JankHunterOperationAttributes.EMPTY,
    ): JankHunterOperation = JankHunter.manualTelemetry().startOperation(name, kind, budgetMs, attributes)

    @JvmSynthetic
    inline fun <T> traceOperation(
        name: String,
        kind: JankHunterOperationKind = JankHunterOperationKind.USER,
        budgetMs: Long = 0L,
        attributes: JankHunterOperationAttributes = JankHunterOperationAttributes.EMPTY,
        block: () -> T,
    ): T {
        val operation = startOperation(name, kind, budgetMs, attributes)
        try {
            return block().also { operation.success() }
        } catch (throwable: Throwable) {
            operation.failure()
            throw throwable
        }
    }

    @JvmStatic
    @JvmOverloads
    fun <T> traceOperation(
        name: String,
        kind: JankHunterOperationKind = JankHunterOperationKind.USER,
        budgetMs: Long = 0L,
        attributes: JankHunterOperationAttributes = JankHunterOperationAttributes.EMPTY,
        block: Callable<T>,
    ): T = traceOperation(name, kind, budgetMs, attributes) { block.call() }

    @JvmStatic
    fun counter(name: String?, delta: Long) = JankHunter.manualTelemetry().counter(name, delta)

    @JvmStatic
    fun gauge(name: String?, value: Long) = JankHunter.manualTelemetry().gauge(name, value)

    @JvmStatic
    @JvmOverloads
    fun watch(instance: Any?, description: String? = null, ownerHint: String? = null) {
        JankHunter.manualTelemetry().watch(instance, description, ownerHint)
    }

    @JvmStatic
    fun recordLog(ownerName: String?, source: String?, level: Int) {
        JankHunter.manualTelemetry().recordLog(ownerName, source, level)
    }

    @JvmStatic
    fun wrapExecutor(executor: Executor?, name: String?, ownerName: String? = name): Executor? {
        return JankHunter.asyncTelemetry().wrapExecutor(executor, name, ownerName)
    }

    @JvmStatic
    fun wrapExecutorService(
        executor: ExecutorService?,
        name: String?,
        ownerName: String? = name,
    ): ExecutorService? = JankHunter.asyncTelemetry().wrapExecutorService(executor, name, ownerName)

    @JvmStatic
    fun wrapScheduledExecutorService(
        executor: ScheduledExecutorService?,
        name: String?,
        ownerName: String? = name,
    ): ScheduledExecutorService? = JankHunter.asyncTelemetry().wrapScheduledExecutorService(executor, name, ownerName)

    @JvmSynthetic
    inline fun <T> traceCompose(
        phase: JankHunterComposePhase,
        name: String?,
        block: () -> T,
    ): T {
        val startedNanos = SystemClock.elapsedRealtimeNanos()
        return block().also {
            recordCompose(phase, name, SystemClock.elapsedRealtimeNanos() - startedNanos)
        }
    }

    @JvmSynthetic
    inline fun <T> traceIO(
        operation: JankHunterIOOperation,
        bytes: Long = -1L,
        ownerName: String? = null,
        block: () -> T,
    ): T {
        val startedNanos = SystemClock.elapsedRealtimeNanos()
        try {
            return block().also {
                recordIO(
                    operation,
                    SystemClock.elapsedRealtimeNanos() - startedNanos,
                    bytes,
                    ownerName,
                    JankHunterIOOutcome.SUCCESS,
                )
            }
        } catch (throwable: Throwable) {
            recordIO(
                operation,
                SystemClock.elapsedRealtimeNanos() - startedNanos,
                bytes,
                ownerName,
                JankHunterIOOutcome.FAILURE,
            )
            throw throwable
        }
    }

    @PublishedApi
    internal fun recordCompose(phase: JankHunterComposePhase, name: String?, durationNanos: Long) {
        if (name != null) JankHunter.manualTelemetry().recordCompose(phase, name, durationNanos)
    }

    @PublishedApi
    internal fun recordIO(
        operation: JankHunterIOOperation,
        durationNanos: Long,
        bytes: Long,
        ownerName: String?,
        outcome: JankHunterIOOutcome,
    ) = JankHunter.manualTelemetry().recordIO(operation, durationNanos, bytes, ownerName, outcome)

    @PublishedApi
    internal fun enterContext(screenName: String?, ownerName: String?): Any? {
        return JankHunter.manualTelemetry().enterContext(screenName, ownerName)
    }

    @PublishedApi
    internal fun exitContext(token: Any?) = JankHunter.manualTelemetry().exitContext(token)
}
