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

class JankHunterExecutor internal constructor(
    private val delegate: Executor,
    private val name: String?,
    private val ownerName: String?,
    private val clock: () -> Long = SystemClock::elapsedRealtime,
) : Executor {
    private val tracker = ExecutorTaskTracker(delegate, name, ownerName, clock)

    override fun execute(command: Runnable) {
        val state = tracker.enqueue()
        try {
            delegate.execute(tracker.oneShotRunnable(command, state))
        } catch (throwable: Throwable) {
            state.cancelIfQueued()
            throw throwable
        }
    }
}

class JankHunterExecutorService internal constructor(
    private val delegate: ExecutorService,
    private val name: String?,
    private val ownerName: String?,
    private val clock: () -> Long = SystemClock::elapsedRealtime,
) : AbstractExecutorService() {
    private val tracker = ExecutorTaskTracker(delegate, name, ownerName, clock)

    override fun execute(command: Runnable) {
        val state = tracker.enqueue()
        try {
            delegate.execute(tracker.oneShotRunnable(command, state))
        } catch (throwable: Throwable) {
            state.cancelIfQueued()
            throw throwable
        }
    }

    override fun shutdown() {
        delegate.shutdown()
    }

    override fun shutdownNow(): MutableList<Runnable> = delegate.shutdownNow()

    override fun isShutdown(): Boolean = delegate.isShutdown

    override fun isTerminated(): Boolean = delegate.isTerminated

    override fun awaitTermination(timeout: Long, unit: TimeUnit): Boolean {
        return delegate.awaitTermination(timeout, unit)
    }

}

class JankHunterScheduledExecutorService internal constructor(
    private val delegate: ScheduledExecutorService,
    private val name: String?,
    private val ownerName: String?,
    private val clock: () -> Long = SystemClock::elapsedRealtime,
) : AbstractExecutorService(), ScheduledExecutorService {
    private val tracker = ExecutorTaskTracker(delegate, name, ownerName, clock)

    override fun execute(command: Runnable) {
        val state = tracker.enqueue()
        try {
            delegate.execute(tracker.oneShotRunnable(command, state))
        } catch (throwable: Throwable) {
            state.cancelIfQueued()
            throw throwable
        }
    }

    override fun schedule(command: Runnable, delay: Long, unit: TimeUnit): ScheduledFuture<*> {
        val state = tracker.enqueue()
        return try {
            TrackedScheduledFuture(delegate.schedule(tracker.oneShotRunnable(command, state), delay, unit), state)
        } catch (throwable: Throwable) {
            state.cancelIfQueued()
            throw throwable
        }
    }

    override fun <V> schedule(callable: Callable<V>, delay: Long, unit: TimeUnit): ScheduledFuture<V> {
        val state = tracker.enqueue()
        return try {
            TrackedScheduledFuture(delegate.schedule(tracker.oneShotCallable(callable, state), delay, unit), state)
        } catch (throwable: Throwable) {
            state.cancelIfQueued()
            throw throwable
        }
    }

    override fun scheduleAtFixedRate(
        command: Runnable,
        initialDelay: Long,
        period: Long,
        unit: TimeUnit,
    ): ScheduledFuture<*> {
        val state = tracker.enqueue()
        return try {
            TrackedScheduledFuture(
                delegate.scheduleAtFixedRate(tracker.periodicRunnable(command, state), initialDelay, period, unit),
                state,
            )
        } catch (throwable: Throwable) {
            state.cancelIfQueued()
            throw throwable
        }
    }

    override fun scheduleWithFixedDelay(
        command: Runnable,
        initialDelay: Long,
        delay: Long,
        unit: TimeUnit,
    ): ScheduledFuture<*> {
        val state = tracker.enqueue()
        return try {
            TrackedScheduledFuture(
                delegate.scheduleWithFixedDelay(tracker.periodicRunnable(command, state), initialDelay, delay, unit),
                state,
            )
        } catch (throwable: Throwable) {
            state.cancelIfQueued()
            throw throwable
        }
    }

    override fun shutdown() {
        delegate.shutdown()
    }

    override fun shutdownNow(): MutableList<Runnable> = delegate.shutdownNow()

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

    fun enqueue(): QueuedTaskState {
        val state = QueuedTaskState(clock())
        queued.incrementAndGet()
        recordSnapshot()
        return state
    }

    fun oneShotRunnable(command: Runnable, state: QueuedTaskState): Runnable {
        return Runnable {
            markStarted(state)
            JankHunter.runExecutorTask(name, ownerName, command, clock)
        }
    }

    fun <T> oneShotCallable(callable: Callable<T>, state: QueuedTaskState): Callable<T> {
        return Callable {
            markStarted(state)
            JankHunter.callExecutorTask(name, ownerName, callable, clock)
        }
    }

    fun periodicRunnable(command: Runnable, state: QueuedTaskState): Runnable {
        return Runnable {
            markStarted(state)
            JankHunter.runExecutorTask(name, ownerName, command, clock)
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
}

private class TrackedScheduledFuture<V>(
    private val delegate: ScheduledFuture<V>,
    private val state: ExecutorTaskTracker.QueuedTaskState,
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
        ?.replace(Regex("[^A-Za-z0-9_.-]+"), "_")
        ?: "unknown"
}

internal fun ThreadPoolExecutor.snapshotActiveCount(): Int = activeCount
