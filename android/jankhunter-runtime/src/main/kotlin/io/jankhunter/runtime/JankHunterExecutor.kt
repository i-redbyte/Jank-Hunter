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
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicIntegerFieldUpdater

internal class JankHunterExecutor internal constructor(
    private val delegate: Executor,
    name: String?,
    ownerName: String?,
    clock: RuntimeLongSource = RuntimeLongSource(SystemClock::elapsedRealtime),
    callbacks: RuntimeAsyncCallbacks,
) : Executor {
    private val tracker = ExecutorTaskTracker(delegate, name, ownerName, clock, callbacks)

    override fun execute(command: Runnable) {
        tracker.execute(command)
    }
}

internal class JankHunterExecutorService internal constructor(
    private val delegate: ExecutorService,
    name: String?,
    ownerName: String?,
    clock: RuntimeLongSource = RuntimeLongSource(SystemClock::elapsedRealtime),
    callbacks: RuntimeAsyncCallbacks,
) : AbstractExecutorService() {
    private val tracker = ExecutorTaskTracker(delegate, name, ownerName, clock, callbacks)

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
    clock: RuntimeLongSource = RuntimeLongSource(SystemClock::elapsedRealtime),
    callbacks: RuntimeAsyncCallbacks,
    scheduledClock: RuntimeLongSource = RuntimeLongSource(System::nanoTime),
) : AbstractExecutorService(), ScheduledExecutorService {
    private val tracker = ExecutorTaskTracker(delegate, name, ownerName, clock, callbacks, scheduledClock)

    override fun execute(command: Runnable) {
        tracker.execute(command)
    }

    override fun schedule(command: Runnable, delay: Long, unit: TimeUnit): ScheduledFuture<*> {
        if (!tracker.isActive()) return delegate.schedule(command, delay, unit)
        return tracker.scheduleTrackedRunnable(command, unit.toNanos(delay).coerceAtLeast(0L), 0L, ScheduleCadence.ONCE) { scheduledCommand ->
            delegate.schedule(scheduledCommand, delay, unit)
        }.also { tracker.recordScheduledDelay(unit.toMillis(delay).coerceAtLeast(0L)) }
    }

    override fun <V> schedule(callable: Callable<V>, delay: Long, unit: TimeUnit): ScheduledFuture<V> {
        if (!tracker.isActive()) return delegate.schedule(callable, delay, unit)
        return tracker.scheduleCallable(callable, unit.toNanos(delay).coerceAtLeast(0L)) { scheduledCallable ->
            delegate.schedule(scheduledCallable, delay, unit)
        }.also { tracker.recordScheduledDelay(unit.toMillis(delay).coerceAtLeast(0L)) }
    }

    override fun scheduleAtFixedRate(
        command: Runnable,
        initialDelay: Long,
        period: Long,
        unit: TimeUnit,
    ): ScheduledFuture<*> {
        if (!tracker.isActive()) return delegate.scheduleAtFixedRate(command, initialDelay, period, unit)
        return tracker.scheduleTrackedRunnable(command, unit.toNanos(initialDelay).coerceAtLeast(0L), unit.toNanos(period), ScheduleCadence.FIXED_RATE) { scheduledCommand ->
            delegate.scheduleAtFixedRate(scheduledCommand, initialDelay, period, unit)
        }.also { tracker.recordScheduledDelay(unit.toMillis(initialDelay).coerceAtLeast(0L)) }
    }

    override fun scheduleWithFixedDelay(
        command: Runnable,
        initialDelay: Long,
        delay: Long,
        unit: TimeUnit,
    ): ScheduledFuture<*> {
        if (!tracker.isActive()) return delegate.scheduleWithFixedDelay(command, initialDelay, delay, unit)
        return tracker.scheduleTrackedRunnable(command, unit.toNanos(initialDelay).coerceAtLeast(0L), unit.toNanos(delay), ScheduleCadence.FIXED_DELAY) { scheduledCommand ->
            delegate.scheduleWithFixedDelay(scheduledCommand, initialDelay, delay, unit)
        }.also { tracker.recordScheduledDelay(unit.toMillis(initialDelay).coerceAtLeast(0L)) }
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
    private val clock: RuntimeLongSource,
    private val callbacks: RuntimeAsyncCallbacks,
    private val scheduledClock: RuntimeLongSource? = null,
) {
    private val queued = AtomicInteger()
    private val metricKeys = ExecutorMetricKeys(name, ownerName, scheduledClock != null)

    private fun <T : QueuedTask> enqueue(task: T): T {
        queued.incrementAndGet()
        // Admission already checked the feature bit before constructing this task.
        callbacks.recordExecutorQueueChanged(metricKeys, delegate, queued.get())
        return task
    }

    fun isActive(): Boolean = callbacks.isExecutorActive()

    fun execute(command: Runnable) {
        if (!isActive()) {
            delegate.execute(command)
            return
        }
        val wrapped = enqueue(TrackedRunnable(this, command))
        try {
            delegate.execute(wrapped)
        } catch (throwable: Throwable) {
            wrapped.cancelIfQueued()
            throw throwable
        }
    }

    fun scheduleTrackedRunnable(
        command: Runnable,
        initialDelayNs: Long,
        intervalNs: Long,
        cadence: ScheduleCadence,
        schedule: (Runnable) -> ScheduledFuture<*>,
    ): ScheduledFuture<*> {
        val wrapped = enqueue(TrackedScheduledRunnable(this, command, initialDelayNs, intervalNs, cadence))
        return trackScheduled(wrapped) {
            schedule(wrapped)
        }
    }

    fun <T> scheduleCallable(
        callable: Callable<T>,
        initialDelayNs: Long,
        schedule: (Callable<T>) -> ScheduledFuture<T>,
    ): ScheduledFuture<T> {
        val wrapped = enqueue(TrackedCallable(this, callable, initialDelayNs))
        return trackScheduled(wrapped) {
            schedule(wrapped)
        }
    }

    private class TrackedRunnable(
        tracker: ExecutorTaskTracker,
        val original: Runnable,
    ) : QueuedTask(tracker), Runnable {
        override fun run() {
            runTracked(original, enqueueContext)
        }

        // Executor owns the command's lifetime and may retry it. Unlike ScheduledFuture,
        // this adapter has no terminal state authorizing it to discard the command.
        override fun finish() = Unit
    }

    private class TrackedScheduledRunnable(
        tracker: ExecutorTaskTracker,
        original: Runnable,
        initialDelayNs: Long,
        intervalNs: Long,
        private val cadence: ScheduleCadence,
    ) : ScheduledQueuedTask(tracker, initialDelayNs, intervalNs), Runnable {
        @Volatile var original: Runnable? = original
            private set

        override fun run() {
            val context = enqueueContext
            val command = original ?: return
            var completed = false
            try {
                runTracked(command, context)
                completed = true
            } finally {
                if (completed) advanceSchedule(cadence)
                if (cadence == ScheduleCadence.ONCE || !completed) finish()
            }
        }

        override fun finish() {
            original = null
            enqueueContext = null
            releaseFuture()
        }
    }

    private class TrackedCallable<T>(
        tracker: ExecutorTaskTracker,
        original: Callable<T>,
        initialDelayNs: Long,
    ) : ScheduledQueuedTask(tracker, initialDelayNs, 0L), Callable<T> {
        @Volatile private var original: Callable<T>? = original

        override fun call(): T {
            val context = enqueueContext
            val command = original ?: throw java.util.concurrent.CancellationException()
            try {
                return callTracked(command, context)
            } finally {
                finish()
            }
        }

        override fun finish() {
            original = null
            enqueueContext = null
            releaseFuture()
        }
    }

    fun unwrapShutdownNow(tasks: MutableList<Runnable>): MutableList<Runnable> {
        val unwrapped = ArrayList<Runnable>(tasks.size)
        tasks.forEach { task ->
            if (task is QueuedTask && task.belongsTo(this)) {
                val original = when (task) {
                    is TrackedRunnable -> task.original
                    is TrackedScheduledRunnable -> task.original
                    else -> task
                }
                task.cancelIfQueued()
                if (original != null) unwrapped.add(original)
            } else {
                unwrapped.add(task)
            }
        }
        return unwrapped
    }

    private fun startWaitMillis(state: QueuedTask): Long {
        // Accepted tasks must leave the queue exactly once, including while telemetry is disabled.
        val wasQueued = state.markDequeued()
        if (wasQueued) queued.decrementAndGet()
        if (!isActive()) return INACTIVE_START
        if (!wasQueued) return 0L
        val now = executorTimeMillis(clock)
        return if (state.enqueuedAtMs >= 0L && now >= state.enqueuedAtMs) now - state.enqueuedAtMs else -1L
    }

    private fun recordQueueChanged() {
        if (isActive()) callbacks.recordExecutorQueueChanged(metricKeys, delegate, queued.get())
    }

    fun recordScheduledDelay(delayMs: Long) {
        if (isActive()) RuntimeHookGuard.run { callbacks.recordExecutorScheduledDelay(metricKeys, delayMs) }
    }

    private fun <T> trackScheduled(
        state: ScheduledTask,
        schedule: () -> ScheduledFuture<T>,
    ): ScheduledFuture<T> {
        // Attach before submission: a zero-delay delegate may finish before schedule returns.
        val future = TrackedScheduledFuture<T>(state)
        state.scheduledFuture = future
        return try {
            future.bind(schedule())
            future
        } catch (throwable: Throwable) {
            state.cancelIfQueued()
            throw throwable
        }
    }

    // Own the tracker once: inner subclasses would each add another outer reference on ART.
    abstract class QueuedTask(protected val tracker: ExecutorTaskTracker, measureQueueWait: Boolean = true) {
        fun belongsTo(tracker: ExecutorTaskTracker): Boolean = this.tracker === tracker

        val enqueuedAtMs: Long = if (measureQueueWait) executorTimeMillis(tracker.clock) else -1L
        protected open val scheduled: Boolean get() = false
        protected open fun startTimingMillis(): Long = tracker.startWaitMillis(this)
        // Only immutable strings/operation ID; ordinary delegates may retry this command.
        @Volatile var enqueueContext: JankHunterContext? = RuntimeHookGuard.value(null) {
            tracker.callbacks.captureContext(tracker.ownerName)
        }
        @JvmSynthetic
        @JvmField
        @Volatile
        internal var queuedState = STATE_QUEUED

        fun markDequeued(): Boolean = QUEUED_STATE.compareAndSet(this, STATE_QUEUED, STATE_DEQUEUED)

        fun cancelIfQueued() {
            try {
                if (markDequeued()) {
                    tracker.queued.decrementAndGet()
                    tracker.recordQueueChanged()
                }
            } finally {
                finish()
            }
        }

        protected abstract fun finish()

        protected fun runTracked(command: Runnable, context: JankHunterContext?) {
            val waitMs = startTimingMillis()
            if (waitMs == INACTIVE_START) {
                command.run()
            } else {
                tracker.callbacks.runExecutorTask(
                    tracker.metricKeys, tracker.ownerName, context, tracker.delegate,
                    tracker.queued.get(), waitMs, command, tracker.clock, scheduled,
                )
            }
        }

        protected fun <T> callTracked(command: Callable<T>, context: JankHunterContext?): T {
            val waitMs = startTimingMillis()
            return if (waitMs == INACTIVE_START) {
                command.call()
            } else {
                tracker.callbacks.callExecutorTask(
                    tracker.metricKeys, tracker.ownerName, context, tracker.delegate,
                    tracker.queued.get(), waitMs, command, tracker.clock, scheduled,
                )
            }
        }
    }

    private abstract class ScheduledQueuedTask(
        tracker: ExecutorTaskTracker,
        initialDelayNs: Long,
        private val intervalNs: Long,
    ) : QueuedTask(tracker, measureQueueWait = false), ScheduledTask {
        @Volatile override var scheduledFuture: TrackedScheduledFuture<*>? = null
        override val scheduled: Boolean get() = true
        private var clockAvailable = false
        private var originNs = readClock()
        private var originKnown = clockAvailable
        private var dueAfterOriginNs = initialDelayNs

        override fun startTimingMillis(): Long {
            if (markDequeued()) tracker.queued.decrementAndGet()
            if (!tracker.isActive()) return INACTIVE_START
            val now = readClock()
            if (!originKnown || !clockAvailable) return -1L
            // Subtraction also works across nanoTime's signed wrap for intervals below 2^63 ns.
            val elapsed = now - originNs
            if (elapsed < 0L) return -1L
            return if (elapsed <= dueAfterOriginNs) 0L else
                TimeUnit.NANOSECONDS.toMillis(elapsed - dueAfterOriginNs)
        }

        protected fun advanceSchedule(cadence: ScheduleCadence) {
            when (cadence) {
                ScheduleCadence.ONCE -> Unit
                ScheduleCadence.FIXED_RATE -> {
                    dueAfterOriginNs = if (intervalNs > Long.MAX_VALUE - dueAfterOriginNs) Long.MAX_VALUE else
                        dueAfterOriginNs + intervalNs
                }
                ScheduleCadence.FIXED_DELAY -> {
                    // Required even while telemetry is disabled: the next due time uses this completion.
                    originNs = readClock()
                    originKnown = clockAvailable
                    dueAfterOriginNs = intervalNs
                }
            }
        }

        private fun readClock(): Long {
            clockAvailable = false
            return RuntimeHookGuard.value(0L) {
                checkNotNull(tracker.scheduledClock).getAsLong().also { clockAvailable = true }
            }
        }
    }

    private companion object {
        const val STATE_QUEUED = 0
        const val INACTIVE_START = Long.MIN_VALUE
        const val STATE_DEQUEUED = 1

        val QUEUED_STATE = AtomicIntegerFieldUpdater.newUpdater(
            QueuedTask::class.java,
            "queuedState",
        )
    }
}

internal fun executorTimeMillis(clock: RuntimeLongSource): Long = RuntimeHookGuard.value(-1L) { clock.getAsLong() }

internal class ExecutorMetricKeys(name: String?, ownerName: String?, scheduled: Boolean = false) {
    private val executor = "executor.${metricExecutorName(name)}."
    private val owner = "owner.${metricOwner(ownerName)}.executor."

    val wait = executor + "wait_ms"
    val scheduledDelay = if (scheduled) executor + "scheduled_delay_ms" else null
    val scheduledLateness = if (scheduled) executor + "scheduled_lateness_ms" else null
    val started = executor + "started.count"
    val queueDepth = executor + "queue_depth"
    val activeCount = executor + "active_count"
    val poolSize = executor + "pool_size"
    val completedTaskCount = executor + "completed_task_count"
    val service = executor + "service_ms"
    val failure = executor + "failure.count"
    val ownerStarted = ownerName?.takeIf(String::isNotBlank)?.let { owner + "started.count" }
    val ownerFailure = owner + "failure.count"
    val ownerDuration = owner + "duration_ms"
}

private enum class ScheduleCadence { ONCE, FIXED_RATE, FIXED_DELAY }

private interface ScheduledTask {
    var scheduledFuture: TrackedScheduledFuture<*>?

    fun cancelIfQueued()

    fun releaseFuture() {
        scheduledFuture?.releaseTaskState()
        scheduledFuture = null
    }
}

private class TrackedScheduledFuture<V>(
    @Volatile private var state: ScheduledTask?,
) : ScheduledFuture<V> {
    private lateinit var delegate: ScheduledFuture<V>

    fun bind(delegate: ScheduledFuture<V>) { this.delegate = delegate }

    fun releaseTaskState() { state = null }

    override fun cancel(mayInterruptIfRunning: Boolean): Boolean {
        val task = state
        val cancelled = delegate.cancel(mayInterruptIfRunning)
        if (cancelled) {
            task?.cancelIfQueued()
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
