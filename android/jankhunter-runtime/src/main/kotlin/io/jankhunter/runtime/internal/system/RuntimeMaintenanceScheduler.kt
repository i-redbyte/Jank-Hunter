package io.jankhunter.runtime.internal.system

import io.jankhunter.runtime.RuntimeHookGuard
import io.jankhunter.runtime.RuntimeLongSource
import io.jankhunter.runtime.internal.monotonicDeadlineAfterMillis
import java.util.concurrent.CountDownLatch
import java.util.concurrent.RejectedExecutionException
import java.util.concurrent.ScheduledFuture
import java.util.concurrent.ScheduledThreadPoolExecutor
import java.util.concurrent.ThreadFactory
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger

/**
 * A single low-priority worker for Jank Hunter's periodic maintenance.
 *
 * Tasks schedule their next run only after the current run finishes. This prevents an expensive
 * sample from creating a backlog and keeps all collector failures isolated from the host app.
 */
internal class RuntimeMaintenanceScheduler(
    private val exactShutdown: Boolean = false,
) {
    private val closed = AtomicBoolean(false)
    private val pendingOneShotTasks = AtomicInteger(0)

    @Volatile
    private var maintenanceThread: Thread? = null

    private val executor = ScheduledThreadPoolExecutor(1, MaintenanceThreadFactory()).apply {
        removeOnCancelPolicy = true
        executeExistingDelayedTasksAfterShutdownPolicy = false
        continueExistingPeriodicTasksAfterShutdownPolicy = false
    }

    fun schedule(
        initialDelayMs: Long = 0L,
        delayMs: RuntimeLongSource,
        task: () -> Unit,
    ): MaintenanceHandle {
        if (closed.get()) return MaintenanceHandle.NONE
        return RecurringTask(delayMs, task).also { it.schedule(initialDelayMs) }
    }

    fun execute(task: () -> Unit): Boolean {
        if (!reserveOneShotTask()) return false
        return try {
            executor.execute { runReservedTask(task) }
            true
        } catch (_: RejectedExecutionException) {
            pendingOneShotTasks.decrementAndGet()
            false
        }
    }

    fun executeDelayed(delayMs: Long, task: () -> Unit): Boolean {
        if (!reserveOneShotTask()) return false
        return try {
            executor.schedule(
                { runReservedTask(task) },
                delayMs.coerceAtLeast(0L),
                TimeUnit.MILLISECONDS,
            )
            true
        } catch (_: RejectedExecutionException) {
            pendingOneShotTasks.decrementAndGet()
            false
        }
    }

    fun executeAndWait(timeoutMs: Long, task: () -> Unit): Boolean {
        if (closed.get()) return false
        if (Thread.currentThread() === maintenanceThread) {
            runSafely(task)
            return true
        }
        val completed = CountDownLatch(1)
        val accepted = execute {
            try {
                task()
            } finally {
                completed.countDown()
            }
        }
        if (!accepted) {
            return false
        }
        return try {
            completed.await(timeoutMs.coerceAtLeast(0L), TimeUnit.MILLISECONDS)
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
            false
        }
    }

    fun shutdown(timeoutMs: Long = DEFAULT_SHUTDOWN_TIMEOUT_MS): Boolean {
        if (!closed.compareAndSet(false, true)) return executor.isTerminated
        if (exactShutdown) {
            executor.shutdown()
            if (Thread.currentThread() === maintenanceThread) {
                discardQueuedTasks()
            } else {
                awaitTermination(timeoutMs)
                if (!executor.isTerminated) executor.shutdownNow()
            }
        } else {
            executor.shutdownNow()
        }
        executor.purge()
        return executor.isTerminated
    }

    private fun discardQueuedTasks() {
        while (true) {
            val queued = executor.queue.poll() ?: return
            (queued as? ScheduledFuture<*>)?.cancel(false)
        }
    }

    private fun runSafely(task: () -> Unit) {
        RuntimeHookGuard.run(task)
    }

    private fun reserveOneShotTask(): Boolean {
        while (!closed.get()) {
            val current = pendingOneShotTasks.get()
            if (current >= MAX_PENDING_ONE_SHOT_TASKS) return false
            if (pendingOneShotTasks.compareAndSet(current, current + 1)) return true
        }
        return false
    }

    private fun runReservedTask(task: () -> Unit) {
        try {
            runSafely(task)
        } finally {
            pendingOneShotTasks.decrementAndGet()
        }
    }

    private inner class RecurringTask(
        private val delayMs: RuntimeLongSource,
        private val task: () -> Unit,
    ) : Runnable, MaintenanceHandle {
        private val futureSlot = CancelableScheduledFutureSlot()

        fun schedule(delay: Long) {
            if (futureSlot.isCancelled() || closed.get()) return
            try {
                val scheduled = executor.schedule(this, delay.coerceAtLeast(0L), TimeUnit.MILLISECONDS)
                futureSlot.publish(scheduled)
            } catch (_: RejectedExecutionException) {
                futureSlot.cancel()
            }
        }

        override fun run() {
            if (futureSlot.isCancelled() || closed.get()) return
            runSafely(task)
            if (!futureSlot.isCancelled() && !closed.get()) {
                schedule(safeDelay())
            }
        }

        override fun cancel() {
            futureSlot.cancel()
        }

        private fun safeDelay(): Long {
            return RuntimeHookGuard.value(DEFAULT_RETRY_DELAY_MS) {
                delayMs.getAsLong().coerceAtLeast(MIN_DELAY_MS)
            }
        }
    }

    private fun awaitTermination(timeoutMs: Long) {
        val deadlineNs = monotonicDeadlineAfterMillis(timeoutMs)
        var interrupted = false
        while (!executor.isTerminated) {
            val remainingNs = deadlineNs - System.nanoTime()
            if (remainingNs <= 0L) break
            try {
                executor.awaitTermination(minOf(remainingNs, TERMINATION_POLL_NS), TimeUnit.NANOSECONDS)
            } catch (_: InterruptedException) {
                interrupted = true
                break
            }
        }
        if (interrupted) Thread.currentThread().interrupt()
    }

    private inner class MaintenanceThreadFactory : ThreadFactory {
        override fun newThread(runnable: Runnable): Thread {
            return Thread(runnable, "JankHunterMaintenance").apply {
                isDaemon = true
                priority = Thread.MIN_PRIORITY
                maintenanceThread = this
            }
        }
    }

    private companion object {
        private const val MIN_DELAY_MS = 100L
        private const val MAX_PENDING_ONE_SHOT_TASKS = 64
        private const val DEFAULT_RETRY_DELAY_MS = 5_000L
        private const val DEFAULT_SHUTDOWN_TIMEOUT_MS = 5_000L
        private val TERMINATION_POLL_NS = TimeUnit.SECONDS.toNanos(1L)
    }
}

/** Publishes and cancels a scheduled future without leaving a late publication queued. */
internal class CancelableScheduledFutureSlot {
    @Volatile
    private var cancelled = false
    private var future: ScheduledFuture<*>? = null

    fun isCancelled(): Boolean = cancelled

    fun publish(scheduled: ScheduledFuture<*>) {
        val reject = synchronized(this) {
            if (cancelled) {
                true
            } else {
                future = scheduled
                false
            }
        }
        if (reject) scheduled.cancel(false)
    }

    fun cancel() {
        val scheduled = synchronized(this) {
            if (cancelled) return
            cancelled = true
            future.also { future = null }
        }
        scheduled?.cancel(false)
    }
}

internal fun interface MaintenanceHandle {
    fun cancel()

    companion object {
        val NONE = MaintenanceHandle {}
    }
}
