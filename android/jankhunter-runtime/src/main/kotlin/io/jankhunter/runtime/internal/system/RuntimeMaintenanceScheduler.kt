package io.jankhunter.runtime.internal.system

import java.util.concurrent.CountDownLatch
import java.util.concurrent.RejectedExecutionException
import java.util.concurrent.ScheduledFuture
import java.util.concurrent.ScheduledThreadPoolExecutor
import java.util.concurrent.ThreadFactory
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean

/**
 * A single low-priority worker for Jank Hunter's periodic maintenance.
 *
 * Tasks schedule their next run only after the current run finishes. This prevents an expensive
 * sample from creating a backlog and keeps all collector failures isolated from the host app.
 */
internal class RuntimeMaintenanceScheduler {
    private val closed = AtomicBoolean(false)

    @Volatile
    private var maintenanceThread: Thread? = null

    private val executor = ScheduledThreadPoolExecutor(1, MaintenanceThreadFactory()).apply {
        removeOnCancelPolicy = true
        executeExistingDelayedTasksAfterShutdownPolicy = false
        continueExistingPeriodicTasksAfterShutdownPolicy = false
    }

    fun schedule(
        initialDelayMs: Long = 0L,
        delayMs: () -> Long,
        task: () -> Unit,
    ): MaintenanceHandle {
        if (closed.get()) return MaintenanceHandle.NONE
        return RecurringTask(delayMs, task).also { it.schedule(initialDelayMs) }
    }

    fun execute(task: () -> Unit): Boolean {
        if (closed.get()) return false
        return try {
            executor.execute { runSafely(task) }
            true
        } catch (_: RejectedExecutionException) {
            false
        }
    }

    fun executeDelayed(delayMs: Long, task: () -> Unit): Boolean {
        if (closed.get()) return false
        return try {
            executor.schedule(
                { runSafely(task) },
                delayMs.coerceAtLeast(0L),
                TimeUnit.MILLISECONDS,
            )
            true
        } catch (_: RejectedExecutionException) {
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

    fun shutdown() {
        if (!closed.compareAndSet(false, true)) return
        executor.shutdownNow()
        executor.purge()
    }

    private fun runSafely(task: () -> Unit) {
        try {
            task()
        } catch (throwable: Throwable) {
            if (throwable is VirtualMachineError || throwable is ThreadDeath) throw throwable
        }
    }

    private inner class RecurringTask(
        private val delayMs: () -> Long,
        private val task: () -> Unit,
    ) : Runnable, MaintenanceHandle {
        private val cancelled = AtomicBoolean(false)

        @Volatile
        private var future: ScheduledFuture<*>? = null

        fun schedule(delay: Long) {
            if (cancelled.get() || closed.get()) return
            try {
                future = executor.schedule(this, delay.coerceAtLeast(0L), TimeUnit.MILLISECONDS)
            } catch (_: RejectedExecutionException) {
                cancelled.set(true)
            }
        }

        override fun run() {
            if (cancelled.get() || closed.get()) return
            runSafely(task)
            if (!cancelled.get() && !closed.get()) {
                schedule(safeDelay())
            }
        }

        override fun cancel() {
            if (!cancelled.compareAndSet(false, true)) return
            future?.cancel(false)
            future = null
        }

        private fun safeDelay(): Long {
            return try {
                delayMs().coerceAtLeast(MIN_DELAY_MS)
            } catch (throwable: Throwable) {
                if (throwable is VirtualMachineError || throwable is ThreadDeath) throw throwable
                DEFAULT_RETRY_DELAY_MS
            }
        }
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
        private const val DEFAULT_RETRY_DELAY_MS = 5_000L
    }
}

internal fun interface MaintenanceHandle {
    fun cancel()

    companion object {
        val NONE = MaintenanceHandle {}
    }
}
