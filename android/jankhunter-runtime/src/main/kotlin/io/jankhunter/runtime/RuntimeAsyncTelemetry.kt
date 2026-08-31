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

    fun <T> callWithContext(context: JankHunterContext, ownerName: String?, block: () -> T): T

    fun startOperation(name: String, kind: JankHunterOperationKind): JankHunterOperation

    fun recordWrappedWork(ownerName: String?, kind: String, durationMs: Long, failed: Boolean)

    fun recordClick(ownerName: String?, durationMs: Long, failed: Boolean)

    fun recordExecutorWait(executorName: String, ownerName: String?, waitMs: Long)

    fun recordExecutorSnapshot(executorName: String, executor: Executor, queued: Int)

    fun runExecutorTask(executorName: String, ownerName: String?, command: Runnable, clock: RuntimeLongSource)

    fun <T> callExecutorTask(
        executorName: String,
        ownerName: String?,
        callable: Callable<T>,
        clock: RuntimeLongSource,
    ): T
}

internal class RuntimeAsyncTelemetry(
    private val access: RuntimeTelemetryAccess,
    private val metrics: RuntimeMetricsService,
    private val operations: RuntimeOperationTelemetry,
) : RuntimeAsyncCallbacks {
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

    fun wrapClickListener(listener: View.OnClickListener?, ownerName: String?): View.OnClickListener? {
        return RuntimeHookGuard.value(listener) {
            wrapClickListenerDecorator(listener, ownerName, access.isActive(), this)
        }
    }

    fun wrapExecutor(executor: Executor?, name: String?, ownerName: String?): Executor? {
        if (executor == null || isWrappedExecutor(executor)) return executor
        if (!access.isActive()) return executor
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
        if (!access.isActive()) return executor
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
        if (!access.isActive()) return executor
        return JankHunterScheduledExecutorService(executor, name, ownerName, callbacks = this)
    }

    override fun captureContext(ownerName: String?): JankHunterContext {
        return access.captureContext(ownerOverride = ownerName)
    }

    override fun isActive(): Boolean = RuntimeHookGuard.value(false) { access.isActive() }

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

    override fun recordExecutorWait(executorName: String, ownerName: String?, waitMs: Long) {
        if (waitMs > 0) {
            recordGauge("executor.$executorName.wait_ms", waitMs)
        }
        recordCounter("executor.$executorName.started.count", 1)
        ownerName?.takeIf { it.isNotBlank() }?.let {
            recordCounter("owner.${metricOwner(it)}.executor.started.count", 1)
        }
    }

    override fun recordExecutorSnapshot(executorName: String, executor: Executor, queued: Int) {
        recordGauge("executor.$executorName.queue_depth", queued.toLong())
        if (executor is ThreadPoolExecutor) {
            recordGauge("executor.$executorName.active_count", executor.snapshotActiveCount().toLong())
            recordGauge("executor.$executorName.pool_size", executor.poolSize.toLong())
            recordGauge("executor.$executorName.completed_task_count", executor.completedTaskCount)
        }
    }

    override fun runExecutorTask(
        executorName: String,
        ownerName: String?,
        command: Runnable,
        clock: RuntimeLongSource,
    ) {
        val start = clock.getAsLong()
        var failed = false
        try {
            access.callWithOwner(ownerName) {
                command.run()
            }
        } catch (throwable: Throwable) {
            failed = true
            throw throwable
        } finally {
            val durationMs = clock.getAsLong() - start
            recordGauge("executor.$executorName.service_ms", durationMs)
            if (failed) recordCounter("executor.$executorName.failure.count", 1)
            recordWrappedWork(ownerName, "executor", durationMs, failed)
        }
    }

    override fun <T> callExecutorTask(
        executorName: String,
        ownerName: String?,
        callable: Callable<T>,
        clock: RuntimeLongSource,
    ): T {
        val start = clock.getAsLong()
        var failed = false
        try {
            return access.callWithOwner(ownerName) {
                callable.call()
            }
        } catch (throwable: Throwable) {
            failed = true
            throw throwable
        } finally {
            val durationMs = clock.getAsLong() - start
            recordGauge("executor.$executorName.service_ms", durationMs)
            if (failed) recordCounter("executor.$executorName.failure.count", 1)
            recordWrappedWork(ownerName, "executor", durationMs, failed)
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
        if (failed || durationMs >= ownerBlockThresholdMs()) {
            recordProblemWindow("wrapped_$kind", durationMs, ownerName)
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
    }
}
