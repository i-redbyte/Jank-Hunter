package io.jankhunter.runtime

import android.os.SystemClock
import java.util.concurrent.AbstractExecutorService
import java.util.concurrent.Callable
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
    name: String?,
    ownerName: String?,
    clock: () -> Long = SystemClock::elapsedRealtime,
) : Executor {
    private val tracker = ExecutorTaskTracker(delegate, name, ownerName, clock)

    override fun execute(command: Runnable) {
        tracker.execute(command)
    }
}

internal class JankHunterExecutorService internal constructor(
    private val delegate: ExecutorService,
    name: String?,
    ownerName: String?,
    clock: () -> Long = SystemClock::elapsedRealtime,
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
    name: String?,
    ownerName: String?,
    clock: () -> Long = SystemClock::elapsedRealtime,
) : AbstractExecutorService(), ScheduledExecutorService {
    private val tracker = ExecutorTaskTracker(delegate, name, ownerName, clock)

    override fun execute(command: Runnable) {
        tracker.execute(command)
    }

    override fun schedule(command: Runnable, delay: Long, unit: TimeUnit): ScheduledFuture<*> {
        return tracker.scheduleTrackedRunnable(command) { scheduledCommand ->
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
        return tracker.scheduleTrackedRunnable(command) { scheduledCommand ->
            delegate.scheduleAtFixedRate(scheduledCommand, initialDelay, period, unit)
        }
    }

    override fun scheduleWithFixedDelay(
        command: Runnable,
        initialDelay: Long,
        delay: Long,
        unit: TimeUnit,
    ): ScheduledFuture<*> {
        return tracker.scheduleTrackedRunnable(command) { scheduledCommand ->
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
    private val metricName = metricExecutorName(name)

    private fun <T : QueuedTask> enqueue(task: T): T {
        queued.incrementAndGet()
        recordSnapshot()
        return task
    }

    fun execute(command: Runnable) {
        val wrapped = enqueue(TrackedRunnable(command))
        try {
            delegate.execute(wrapped)
        } catch (throwable: Throwable) {
            wrapped.cancelIfQueued()
            throw throwable
        }
    }

    fun scheduleTrackedRunnable(
        command: Runnable,
        schedule: (Runnable) -> ScheduledFuture<*>,
    ): ScheduledFuture<*> {
        val wrapped = enqueue(TrackedRunnable(command))
        return trackScheduled(wrapped) {
            schedule(wrapped)
        }
    }

    fun <T> scheduleCallable(
        callable: Callable<T>,
        schedule: (Callable<T>) -> ScheduledFuture<T>,
    ): ScheduledFuture<T> {
        val wrapped = enqueue(TrackedCallable(callable))
        return trackScheduled(wrapped) {
            schedule(wrapped)
        }
    }

    private inner class TrackedRunnable(
        val original: Runnable,
    ) : QueuedTask(), Runnable {
        override fun run() {
            markStarted(this)
            JankHunter.runExecutorTask(metricName, ownerName, original, clock)
        }

        fun belongsTo(tracker: ExecutorTaskTracker): Boolean = this@ExecutorTaskTracker === tracker
    }

    private inner class TrackedCallable<T>(
        private val original: Callable<T>,
    ) : QueuedTask(), Callable<T> {
        override fun call(): T {
            markStarted(this)
            return JankHunter.callExecutorTask(metricName, ownerName, original, clock)
        }
    }

    fun unwrapShutdownNow(tasks: MutableList<Runnable>): MutableList<Runnable> {
        val unwrapped = ArrayList<Runnable>(tasks.size)
        tasks.forEach { task ->
            if (task is TrackedRunnable && task.belongsTo(this)) {
                task.cancelIfQueued()
                unwrapped.add(task.original)
            } else {
                unwrapped.add(task)
            }
        }
        return unwrapped
    }

    private fun markStarted(state: QueuedTask) {
        val waitMs = if (state.markDequeued()) {
            queued.decrementAndGet()
            clock() - state.enqueuedAtMs
        } else {
            0L
        }
        JankHunter.recordExecutorWait(metricName, ownerName, waitMs)
        recordSnapshot()
    }

    private fun recordSnapshot() {
        JankHunter.recordExecutorSnapshot(metricName, delegate, queued.get())
    }

    private fun <T> trackScheduled(
        state: QueuedTask,
        schedule: () -> ScheduledFuture<T>,
    ): ScheduledFuture<T> {
        return try {
            TrackedScheduledFuture(schedule(), state)
        } catch (throwable: Throwable) {
            state.cancelIfQueued()
            throw throwable
        }
    }

    abstract inner class QueuedTask {
        val enqueuedAtMs: Long = clock()
        private val queuedState = AtomicBoolean(true)

        fun markDequeued(): Boolean = queuedState.compareAndSet(true, false)

        fun cancelIfQueued() {
            if (markDequeued()) {
                queued.decrementAndGet()
                recordSnapshot()
            }
        }
    }
}

private class TrackedScheduledFuture<V>(
    private val delegate: ScheduledFuture<V>,
    private val state: ExecutorTaskTracker.QueuedTask,
) : ScheduledFuture<V> {
    override fun cancel(mayInterruptIfRunning: Boolean): Boolean {
        val cancelled = delegate.cancel(mayInterruptIfRunning)
        if (cancelled) {
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
