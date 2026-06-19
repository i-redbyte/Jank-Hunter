package io.jankhunter.runtime

import android.os.SystemClock
import java.util.concurrent.AbstractExecutorService
import java.util.concurrent.Callable
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Delayed
import java.util.concurrent.Executor
import java.util.concurrent.ExecutorService
import java.util.concurrent.ScheduledExecutorService
import java.util.concurrent.ScheduledFuture
import java.util.concurrent.ThreadPoolExecutor
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger

internal class JankHunterExecutor internal constructor(
    private val delegate: Executor,
    private val name: String?,
    private val ownerName: String?,
    private val clock: () -> Long = SystemClock::elapsedRealtime,
) : Executor {
    private val tracker = ExecutorTaskTracker(delegate, name, ownerName, clock)

    override fun execute(command: Runnable) {
        tracker.execute(command)
    }
}

internal class JankHunterExecutorService internal constructor(
    private val delegate: ExecutorService,
    private val name: String?,
    private val ownerName: String?,
    private val clock: () -> Long = SystemClock::elapsedRealtime,
) : AbstractExecutorService() {
    private val tracker = ExecutorTaskTracker(delegate, name, ownerName, clock)

    override fun execute(command: Runnable) {
        tracker.execute(command)
    }

    override fun shutdown() {
        delegate.shutdown()
    }

    override fun shutdownNow(): MutableList<Runnable> = tracker.unwrapShutdownNow(delegate.shutdownNow())

    override fun isShutdown(): Boolean = delegate.isShutdown

    override fun isTerminated(): Boolean = delegate.isTerminated

    override fun awaitTermination(timeout: Long, unit: TimeUnit): Boolean {
        return delegate.awaitTermination(timeout, unit)
    }

}

internal class JankHunterScheduledExecutorService internal constructor(
    private val delegate: ScheduledExecutorService,
    private val name: String?,
    private val ownerName: String?,
    private val clock: () -> Long = SystemClock::elapsedRealtime,
) : AbstractExecutorService(), ScheduledExecutorService {
    private val tracker = ExecutorTaskTracker(delegate, name, ownerName, clock)

    override fun execute(command: Runnable) {
        tracker.execute(command)
    }

    override fun schedule(command: Runnable, delay: Long, unit: TimeUnit): ScheduledFuture<*> {
        return tracker.scheduleRunnable(command) { scheduledCommand ->
            delegate.schedule(scheduledCommand, delay, unit)
        }
    }

    override fun <V> schedule(callable: Callable<V>, delay: Long, unit: TimeUnit): ScheduledFuture<V> {
        return tracker.scheduleCallable(callable) { scheduledCallable ->
            delegate.schedule(scheduledCallable, delay, unit)
        }
    }

    override fun scheduleAtFixedRate(
        command: Runnable,
        initialDelay: Long,
        period: Long,
        unit: TimeUnit,
    ): ScheduledFuture<*> {
        return tracker.schedulePeriodic(command) { scheduledCommand ->
            delegate.scheduleAtFixedRate(scheduledCommand, initialDelay, period, unit)
        }
    }

    override fun scheduleWithFixedDelay(
        command: Runnable,
        initialDelay: Long,
        delay: Long,
        unit: TimeUnit,
    ): ScheduledFuture<*> {
        return tracker.schedulePeriodic(command) { scheduledCommand ->
            delegate.scheduleWithFixedDelay(scheduledCommand, initialDelay, delay, unit)
        }
    }

    override fun shutdown() {
        delegate.shutdown()
    }

    override fun shutdownNow(): MutableList<Runnable> = tracker.unwrapShutdownNow(delegate.shutdownNow())

    override fun isShutdown(): Boolean = delegate.isShutdown

    override fun isTerminated(): Boolean = delegate.isTerminated

    override fun awaitTermination(timeout: Long, unit: TimeUnit): Boolean {
        return delegate.awaitTermination(timeout, unit)
    }
}

private class ExecutorTaskTracker(
    private val delegate: Executor,
    private val name: String?,
    private val ownerName: String?,
    private val clock: () -> Long,
) {
    private val queued = AtomicInteger()
    private val trackedRunnables = ConcurrentHashMap<Runnable, PendingRunnable>()

    private fun enqueue(): QueuedTaskState {
        val state = QueuedTaskState(clock())
        queued.incrementAndGet()
        recordSnapshot()
        return state
    }

    fun execute(command: Runnable) {
        val state = enqueue()
        val wrapped = oneShotRunnable(command, state)
        try {
            delegate.execute(wrapped)
        } catch (throwable: Throwable) {
            trackedRunnables.remove(wrapped)
            state.cancelIfQueued()
            throw throwable
        }
    }

    fun scheduleRunnable(
        command: Runnable,
        schedule: (Runnable) -> ScheduledFuture<*>,
    ): ScheduledFuture<*> {
        val state = enqueue()
        val wrapped = oneShotRunnable(command, state)
        return trackScheduled(state, onCancel = { trackedRunnables.remove(wrapped) }) {
            schedule(wrapped)
        }
    }

    fun <T> scheduleCallable(
        callable: Callable<T>,
        schedule: (Callable<T>) -> ScheduledFuture<T>,
    ): ScheduledFuture<T> {
        val state = enqueue()
        return trackScheduled(state, onCancel = {}) {
            schedule(oneShotCallable(callable, state))
        }
    }

    fun schedulePeriodic(
        command: Runnable,
        schedule: (Runnable) -> ScheduledFuture<*>,
    ): ScheduledFuture<*> {
        val state = enqueue()
        val wrapped = periodicRunnable(command, state)
        return trackScheduled(state, onCancel = { trackedRunnables.remove(wrapped) }) {
            schedule(wrapped)
        }
    }

    private fun oneShotRunnable(command: Runnable, state: QueuedTaskState): Runnable {
        return trackRunnable(command, state, removeAfterRun = true)
    }

    private fun <T> oneShotCallable(callable: Callable<T>, state: QueuedTaskState): Callable<T> {
        return Callable {
            markStarted(state)
            JankHunter.callExecutorTask(name, ownerName, callable, clock)
        }
    }

    private fun periodicRunnable(command: Runnable, state: QueuedTaskState): Runnable {
        return trackRunnable(command, state, removeAfterRun = false, removeOnFailure = true)
    }

    private fun trackRunnable(
        command: Runnable,
        state: QueuedTaskState,
        removeAfterRun: Boolean,
        removeOnFailure: Boolean = false,
    ): Runnable {
        val wrapped = object : Runnable {
            override fun run() {
                markStarted(state)
                var failed = false
                try {
                    JankHunter.runExecutorTask(name, ownerName, command, clock)
                } catch (throwable: Throwable) {
                    failed = true
                    throw throwable
                } finally {
                    if (removeAfterRun || (failed && removeOnFailure)) {
                        trackedRunnables.remove(this)
                    }
                }
            }
        }
        trackedRunnables[wrapped] = PendingRunnable(command, state)
        return wrapped
    }

    fun unwrapShutdownNow(tasks: MutableList<Runnable>): MutableList<Runnable> {
        return tasks.mapTo(mutableListOf()) { task ->
            val pending = trackedRunnables.remove(task)
            if (pending != null) {
                pending.state.cancelIfQueued()
                pending.original
            } else {
                task
            }
        }
    }

    private fun markStarted(state: QueuedTaskState) {
        val waitMs = if (state.markDequeued()) {
            queued.decrementAndGet()
            clock() - state.enqueuedAtMs
        } else {
            0L
        }
        JankHunter.recordExecutorWait(name, ownerName, waitMs)
        recordSnapshot()
    }

    private fun recordSnapshot() {
        JankHunter.recordExecutorSnapshot(name, delegate, queued.get())
    }

    private fun <T> trackScheduled(
        state: QueuedTaskState,
        onCancel: () -> Unit,
        schedule: () -> ScheduledFuture<T>,
    ): ScheduledFuture<T> {
        return try {
            TrackedScheduledFuture(schedule(), state, onCancel)
        } catch (throwable: Throwable) {
            onCancel()
            state.cancelIfQueued()
            throw throwable
        }
    }

    inner class QueuedTaskState(
        val enqueuedAtMs: Long,
    ) {
        private val queuedState = AtomicBoolean(true)

        fun markDequeued(): Boolean = queuedState.compareAndSet(true, false)

        fun cancelIfQueued() {
            if (markDequeued()) {
                queued.decrementAndGet()
                recordSnapshot()
            }
        }
    }

    private data class PendingRunnable(
        val original: Runnable,
        val state: QueuedTaskState,
    )
}

private class TrackedScheduledFuture<V>(
    private val delegate: ScheduledFuture<V>,
    private val state: ExecutorTaskTracker.QueuedTaskState,
    private val onCancel: () -> Unit,
) : ScheduledFuture<V> {
    override fun cancel(mayInterruptIfRunning: Boolean): Boolean {
        val cancelled = delegate.cancel(mayInterruptIfRunning)
        if (cancelled) {
            onCancel()
            state.cancelIfQueued()
        }
        return cancelled
    }

    override fun isCancelled(): Boolean = delegate.isCancelled

    override fun isDone(): Boolean = delegate.isDone

    override fun get(): V = delegate.get()

    override fun get(timeout: Long, unit: TimeUnit): V = delegate.get(timeout, unit)

    override fun getDelay(unit: TimeUnit): Long = delegate.getDelay(unit)

    override fun compareTo(other: Delayed): Int = delegate.compareTo(other)
}

internal fun metricExecutorName(name: String?): String {
    return name
        ?.takeIf { it.isNotBlank() }
        ?.replace(EXECUTOR_METRIC_UNSAFE_CHARS, "_")
        ?: "unknown"
}

internal fun ThreadPoolExecutor.snapshotActiveCount(): Int = activeCount

private val EXECUTOR_METRIC_UNSAFE_CHARS = Regex("[^A-Za-z0-9_.-]+")
