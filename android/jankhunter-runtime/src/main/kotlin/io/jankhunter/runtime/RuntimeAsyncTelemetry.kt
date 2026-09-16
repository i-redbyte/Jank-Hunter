package io.jankhunter.runtime

import android.view.View
import java.util.concurrent.Callable
import java.util.concurrent.Executor
import java.util.concurrent.ExecutorService
import java.util.concurrent.ScheduledExecutorService
import java.util.concurrent.ThreadPoolExecutor

internal interface RuntimeAsyncCallbacks {
    fun captureContext(ownerName: String?): JankHunterContext

    fun isActive(): Boolean

    fun isExecutorActive(): Boolean = isActive()

    fun <T> callWithContext(context: JankHunterContext, ownerName: String?, block: () -> T): T

    fun startOperation(name: String, kind: JankHunterOperationKind): JankHunterOperation

    fun recordWrappedWork(ownerName: String?, kind: String, durationMs: Long, failed: Boolean)

    fun recordClick(ownerName: String?, durationMs: Long, failed: Boolean)

    fun recordExecutorQueueChanged(keys: ExecutorMetricKeys, executor: Executor, queued: Int)

    fun recordExecutorStarted(keys: ExecutorMetricKeys, executor: Executor, queued: Int, waitMs: Long, scheduled: Boolean = false)

    fun recordExecutorScheduledDelay(keys: ExecutorMetricKeys, delayMs: Long) = Unit

    fun runExecutorTask(
        keys: ExecutorMetricKeys, ownerName: String?, context: JankHunterContext?, executor: Executor,
        queued: Int, waitMs: Long, command: Runnable, clock: RuntimeLongSource, scheduled: Boolean = false,
    )

    fun <T> callExecutorTask(
        keys: ExecutorMetricKeys,
        ownerName: String?,
        context: JankHunterContext?,
        executor: Executor,
        queued: Int,
        waitMs: Long,
        callable: Callable<T>,
        clock: RuntimeLongSource,
        scheduled: Boolean = false,
    ): T
}

internal class RuntimeAsyncTelemetry(
    private val access: RuntimeTelemetryAccess,
    private val metrics: RuntimeMetricsService,
    private val operations: RuntimeOperationTelemetry,
    nowMs: RuntimeLongSource,
) : RuntimeAsyncCallbacks {
    private val coroutineExecutions = CoroutineExecutionTracker(
        clock = nowMs,
        threadId = RuntimeLongSource { Thread.currentThread().id },
        onComplete = ::recordCoroutineExecution,
        onEviction = { recordCounter(COROUTINE_REGISTRY_EVICTION_METRIC, 1L) },
        onInvalidTransition = { recordCounter(COROUTINE_INVALID_TRANSITION_METRIC, 1L) },
        onResolutionMiss = { recordCounter(COROUTINE_RESOLUTION_MISS_METRIC, 1L) },
    )

    fun wrapRunnable(runnable: Runnable?, ownerName: String?): Runnable? {
        return RuntimeHookGuard.value(runnable) {
            wrapRunnableDecorator(runnable, ownerName, access.isActive(), this)
        }
    }

    fun <T> wrapCallable(callable: Callable<T>?, ownerName: String?): Callable<T>? {
        return RuntimeHookGuard.value(callable) {
            wrapCallableDecorator(callable, ownerName, access.isActive(), this)
        }
    }

    fun wrapCoroutineBlock(block: Function2<*, *, *>?, ownerName: String?): Function2<*, *, *>? {
        return RuntimeHookGuard.value(block) {
            wrapCoroutineBlockDecorator(block, ownerName, access.isActive(), this)
        }
    }

    fun enterCoroutineSegment(continuation: Any?, ownerName: String?, collectNew: Boolean): Long {
        val owner = ownerName?.takeIf(String::isNotBlank) ?: "unknown"
        return coroutineExecutions.enter(continuation, owner, collectNew, access.lifecycleGeneration())
    }

    fun exitCoroutineSegment(
        token: Long,
        continuation: Any?,
        suspended: Boolean,
        outcome: CoroutineExecutionOutcome,
    ) {
        coroutineExecutions.exit(token, continuation, suspended, outcome)
    }

    fun clearCoroutineExecutions() = coroutineExecutions.clear()

    fun wrapClickListener(listener: View.OnClickListener?, ownerName: String?): View.OnClickListener? {
        return RuntimeHookGuard.value(listener) {
            wrapClickListenerDecorator(listener, ownerName, access.isActive(), this)
        }
    }

    fun wrapExecutor(executor: Executor?, name: String?, ownerName: String?): Executor? {
        if (executor == null || isWrappedExecutor(executor)) return executor
        if (!isExecutorActive()) return executor
        return if (executor is ExecutorService) {
            wrapExecutorService(executor, name, ownerName)
        } else {
            JankHunterExecutor(executor, name, ownerName, callbacks = this)
        }
    }

    fun wrapExecutorService(
        executor: ExecutorService?,
        name: String?,
        ownerName: String?,
    ): ExecutorService? {
        if (executor == null || executor is JankHunterExecutorService ||
            executor is JankHunterScheduledExecutorService
        ) {
            return executor
        }
        if (!isExecutorActive()) return executor
        return if (executor is ScheduledExecutorService) {
            JankHunterScheduledExecutorService(executor, name, ownerName, callbacks = this)
        } else {
            JankHunterExecutorService(executor, name, ownerName, callbacks = this)
        }
    }

    fun wrapScheduledExecutorService(
        executor: ScheduledExecutorService?,
        name: String?,
        ownerName: String?,
    ): ScheduledExecutorService? {
        if (executor == null || executor is JankHunterScheduledExecutorService) return executor
        if (!isExecutorActive()) return executor
        return JankHunterScheduledExecutorService(executor, name, ownerName, callbacks = this)
    }

    override fun captureContext(ownerName: String?): JankHunterContext {
        return access.captureContext(ownerOverride = ownerName)
    }

    override fun isActive(): Boolean = RuntimeHookGuard.value(false) { access.isActive() }

    override fun isExecutorActive(): Boolean = access.isFeatureActive(JankHunterRuntimeFeature.EXECUTORS)

    override fun <T> callWithContext(
        context: JankHunterContext,
        ownerName: String?,
        block: () -> T,
    ): T = access.callWithContext(context, ownerName, block)

    override fun startOperation(name: String, kind: JankHunterOperationKind): JankHunterOperation {
        return operations.start(name, kind, 0L, JankHunterOperationAttributes.EMPTY)
    }

    override fun recordWrappedWork(ownerName: String?, kind: String, durationMs: Long, failed: Boolean) {
        RuntimeHookGuard.run {
            recordWrappedWorkUnsafe(ownerName, kind, durationMs, failed)
        }
    }

    override fun recordExecutorQueueChanged(keys: ExecutorMetricKeys, executor: Executor, queued: Int) {
        RuntimeHookGuard.run {
            val pool = executor as? ThreadPoolExecutor
            metrics.recordExecutorQueueChanged(
                keys = keys,
                queueDepth = queued,
                activeCount = pool?.snapshotActiveCount() ?: NO_POOL_SNAPSHOT,
                poolSize = pool?.poolSize ?: NO_POOL_SNAPSHOT,
                completedTaskCount = pool?.completedTaskCount ?: NO_POOL_SNAPSHOT.toLong(),
            )
        }
    }

    override fun recordExecutorStarted(keys: ExecutorMetricKeys, executor: Executor, queued: Int, waitMs: Long, scheduled: Boolean) {
        RuntimeHookGuard.run {
            val pool = executor as? ThreadPoolExecutor
            metrics.recordExecutorStarted(
                keys = keys,
                waitMs = waitMs,
                scheduled = scheduled,
                queueDepth = queued,
                activeCount = pool?.snapshotActiveCount() ?: NO_POOL_SNAPSHOT,
                poolSize = pool?.poolSize ?: NO_POOL_SNAPSHOT,
                completedTaskCount = pool?.completedTaskCount ?: NO_POOL_SNAPSHOT.toLong(),
            )
        }
    }

    override fun recordExecutorScheduledDelay(keys: ExecutorMetricKeys, delayMs: Long) {
        RuntimeHookGuard.run { metrics.recordGauge(keys.scheduledDelay, delayMs) }
    }

    override fun runExecutorTask(
        keys: ExecutorMetricKeys,
        ownerName: String?,
        context: JankHunterContext?,
        executor: Executor,
        queued: Int,
        waitMs: Long,
        command: Runnable,
        clock: RuntimeLongSource,
        scheduled: Boolean,
    ) {
        access.callWithContext(context ?: JankHunterContext(null, null), ownerName) {
            recordExecutorStarted(keys, executor, queued, waitMs, scheduled)
            val start = executorTimeMillis(clock)
            var failed = false
            try {
                command.run()
            } catch (throwable: Throwable) {
                failed = true
                throw throwable
            } finally {
                val end = executorTimeMillis(clock)
                val durationMs = if (start >= 0L && end >= start) end - start else -1L
                recordExecutorWork(keys, ownerName, durationMs, failed)
            }
        }
    }

    override fun <T> callExecutorTask(
        keys: ExecutorMetricKeys,
        ownerName: String?,
        context: JankHunterContext?,
        executor: Executor,
        queued: Int,
        waitMs: Long,
        callable: Callable<T>,
        clock: RuntimeLongSource,
        scheduled: Boolean,
    ): T {
        return access.callWithContext(context ?: JankHunterContext(null, null), ownerName) {
            recordExecutorStarted(keys, executor, queued, waitMs, scheduled)
            val start = executorTimeMillis(clock)
            var failed = false
            try {
                callable.call()
            } catch (throwable: Throwable) {
                failed = true
                throw throwable
            } finally {
                val end = executorTimeMillis(clock)
                val durationMs = if (start >= 0L && end >= start) end - start else -1L
                recordExecutorWork(keys, ownerName, durationMs, failed)
            }
        }
    }

    private fun recordExecutorWork(
        keys: ExecutorMetricKeys,
        ownerName: String?,
        durationMs: Long,
        failed: Boolean,
    ) {
        RuntimeHookGuard.run {
            metrics.recordExecutorFinished(
                keys = keys,
                durationMs = durationMs,
                failed = failed,
                recordOwnerDuration = durationMs >= WRAPPED_WORK_GAUGE_THRESHOLD_MS,
            )
            if (durationMs >= 0L && (failed || durationMs >= ownerBlockThresholdMs())) {
                recordProblemWindow("wrapped_executor", durationMs, ownerName)
            }
        }
    }

    fun recordMainThreadDispatch(durationMs: Long, thresholdMs: Long, source: String?) {
        if (durationMs < thresholdMs) return
        recordGauge("main_thread.dispatch.duration_ms", durationMs)
        val overThresholdMs = durationMs - thresholdMs
        recordCounter("main_thread.dispatch.slow.count", 1)
        recordGauge("main_thread.dispatch.over_threshold_ms", overThresholdMs)
        recordCounter("screen.${metricOwner(access.currentScreen())}.main_thread.slow_dispatch.count", 1)
        recordCounter("main_thread.dispatch.source.${metricOwner(source)}.slow.count", 1)
        recordProblemWindow("main_thread_dispatch", durationMs, source)
    }

    override fun recordClick(ownerName: String?, durationMs: Long, failed: Boolean) {
        recordWrappedWork(ownerName, "click", durationMs, failed)
    }

    private fun recordWrappedWorkUnsafe(ownerName: String?, kind: String, durationMs: Long, failed: Boolean) {
        val owner = metricOwner(ownerName)
        if (failed) recordCounter("owner.$owner.$kind.failure.count", 1)
        if (durationMs >= WRAPPED_WORK_GAUGE_THRESHOLD_MS) {
            recordGauge("owner.$owner.$kind.duration_ms", durationMs)
        }
        if (kind != COROUTINE_KIND && (failed || durationMs >= ownerBlockThresholdMs())) {
            recordProblemWindow("wrapped_$kind", durationMs, ownerName)
        }
    }

    private fun recordCoroutineExecution(
        owner: String,
        activeDurationMs: Long,
        suspendedDurationMs: Long,
        suspensionCount: Int,
        threadMigrationCount: Int,
        generation: Long,
        outcome: CoroutineExecutionOutcome,
    ) {
        if (generation != access.lifecycleGeneration() || !access.isActive()) return
        val prefix = "owner.${metricOwner(owner)}.$COROUTINE_KIND."
        recordGauge(prefix + "active_duration_ms", activeDurationMs)
        recordGauge(prefix + "suspended_duration_ms", suspendedDurationMs)
        if (suspensionCount > 0) {
            recordCounter(prefix + "suspension.count", suspensionCount.toLong())
        }
        if (threadMigrationCount > 0) {
            recordCounter(prefix + "thread_migration.count", threadMigrationCount.toLong())
        }
        when (outcome) {
            CoroutineExecutionOutcome.SUCCESS -> Unit
            CoroutineExecutionOutcome.FAILURE -> recordCounter(prefix + "segmented_failure.count", 1L)
            CoroutineExecutionOutcome.CANCELLED -> recordCounter(prefix + "segmented_cancellation.count", 1L)
        }
        if (outcome == CoroutineExecutionOutcome.FAILURE || activeDurationMs >= ownerBlockThresholdMs()
        ) {
            recordProblemWindow("wrapped_coroutine_active", activeDurationMs, owner)
        }
    }

    private fun recordProblemWindow(kind: String, durationMs: Long, ownerOverride: String?) {
        val owner = firstContextValue(ownerOverride, access.currentOwnerOrNull())
        val context = access.captureContext(ownerOverride = owner)
        access.writer?.problemWindow(
            context.screen,
            context.owner,
            kind,
            durationMs,
            count = 1,
            maxMs = durationMs,
            foreground = access.isUiVisible(),
        )
    }

    private fun recordCounter(name: String, value: Long) {
        RuntimeHookGuard.run { metrics.recordCounter(name, value) }
    }

    private fun recordGauge(name: String, value: Long) {
        RuntimeHookGuard.run { metrics.recordGauge(name, value) }
    }

    private fun ownerBlockThresholdMs(): Long {
        return access.config?.ownerBlockThresholdMs() ?: DEFAULT_OWNER_BLOCK_THRESHOLD_MS
    }

    private fun isWrappedExecutor(executor: Executor): Boolean {
        return executor is JankHunterExecutor ||
            executor is JankHunterExecutorService ||
            executor is JankHunterScheduledExecutorService
    }

    private companion object {
        const val WRAPPED_WORK_GAUGE_THRESHOLD_MS = 50L
        const val DEFAULT_OWNER_BLOCK_THRESHOLD_MS = 250L
        const val NO_POOL_SNAPSHOT = -1
        const val COROUTINE_KIND = "coroutine"
        const val COROUTINE_REGISTRY_EVICTION_METRIC = "jankhunter.coroutine.state_registry.eviction.count"
        const val COROUTINE_INVALID_TRANSITION_METRIC = "jankhunter.coroutine.state_registry.invalid_transition.count"
        const val COROUTINE_RESOLUTION_MISS_METRIC = "jankhunter.coroutine.state_registry.resolution_miss.count"
    }
}
