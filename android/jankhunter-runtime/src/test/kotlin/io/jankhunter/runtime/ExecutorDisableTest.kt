package io.jankhunter.runtime

import java.util.concurrent.Callable
import java.util.concurrent.Delayed
import java.util.concurrent.Executor
import java.util.concurrent.Executors
import java.util.concurrent.FutureTask
import java.util.concurrent.ScheduledFuture
import java.util.concurrent.ScheduledThreadPoolExecutor
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Test

class ExecutorDisableTest {
    @Test
    fun existingExecutorPassesOriginalCommandWithoutTelemetryAfterDisable() {
        val callbacks = Callbacks()
        var queued: Runnable? = null
        val executor = JankHunterExecutor(Executor { queued = it }, "pool", "owner", callbacks.clock, callbacks)
        callbacks.active = false
        var runs = 0
        val command = Runnable { runs++ }
        executor.execute(command)
        assertSame("disabled SDK replaced the application command", command, queued)
        checkNotNull(queued).run()
        assertEquals(1, runs)
        assertEquals(0, callbacks.clockReads)
        assertEquals(0, callbacks.telemetryCalls)
    }

    @Test
    fun inactiveSchedulingPreservesCommandsAndDelegateFuturesForEveryOverload() {
        val callbacks = Callbacks()
        val delegate = CapturingScheduler()
        val executor = JankHunterScheduledExecutorService(delegate, "scheduler", null, callbacks.clock, callbacks)
        callbacks.active = false
        val command = Runnable {}
        val callable = Callable { "result" }
        try {
            assertSame(delegateFuture(delegate) { executor.schedule(command, 7L, TimeUnit.DAYS) }, delegate.future)
            assertSame(command, delegate.command)
            assertEquals(7L, delegate.delay)
            assertEquals(TimeUnit.DAYS, delegate.unit)
            assertSame(delegateFuture(delegate) { executor.schedule(callable, 9L, TimeUnit.HOURS) }, delegate.future)
            assertSame(callable, delegate.command)
            assertSame(delegateFuture(delegate) { executor.scheduleAtFixedRate(command, 2L, 3L, TimeUnit.MINUTES) }, delegate.future)
            assertSame(command, delegate.command)
            assertEquals(3L, delegate.period)
            assertSame(delegateFuture(delegate) { executor.scheduleWithFixedDelay(command, 4L, 5L, TimeUnit.SECONDS) }, delegate.future)
            assertSame(command, delegate.command)
            assertEquals(5L, delegate.period)
            assertEquals(0, callbacks.clockReads)
            assertEquals(0, callbacks.telemetryCalls)
        } finally {
            delegate.shutdownNow()
        }
    }

    @Test
    fun previouslyQueuedTaskDrainsBookkeepingWithoutClockOrMetricsWhileInactive() {
        val callbacks = Callbacks()
        var queued: Runnable? = null
        val executor = JankHunterExecutor(Executor { queued = it }, "pool", null, callbacks.clock, callbacks)
        var runs = 0
        executor.execute { runs++ }
        callbacks.active = false
        callbacks.resetObservations()
        checkNotNull(queued).run()
        assertEquals(1, runs)
        assertEquals(0, callbacks.clockReads)
        assertEquals(0, callbacks.telemetryCalls)
        callbacks.active = true
        executor.execute { runs++ }
        assertEquals(1, callbacks.lastQueueDepth)
        checkNotNull(queued).run()
        assertEquals(0, callbacks.lastQueueDepth)
        assertEquals(2, runs)
    }

    @Test
    fun acceptedPeriodicTaskKeepsApplicationExecutionAcrossDisableAndEnable() {
        val callbacks = Callbacks()
        val delegate = CapturingScheduler()
        val executor = JankHunterScheduledExecutorService(delegate, "periodic", null, callbacks.clock, callbacks)
        var runs = 0
        try {
            val future = executor.scheduleAtFixedRate({ runs++ }, 1L, 2L, TimeUnit.SECONDS)
            val command = delegate.command as Runnable
            command.run()
            callbacks.active = false
            callbacks.resetObservations()
            repeat(3) { command.run() }
            assertEquals(4, runs)
            assertEquals(0, callbacks.clockReads)
            assertEquals(0, callbacks.telemetryCalls)
            callbacks.active = true
            command.run()
            assertEquals(5, runs)
            assertEquals(0, callbacks.lastQueueDepth)
            future.cancel(false)
        } finally {
            delegate.shutdownNow()
        }
    }

    @Test
    fun acceptedCallableReturnsItsResultWhileTelemetryIsDisabled() {
        val callbacks = Callbacks()
        val delegate = CapturingScheduler()
        val executor = JankHunterScheduledExecutorService(delegate, "callable", null, callbacks.clock, callbacks)
        val result = Any()
        try {
            executor.schedule(Callable { result }, 1L, TimeUnit.DAYS)
            callbacks.active = false
            callbacks.resetObservations()
            assertSame(result, (delegate.command as Callable<*>).call())
            assertEquals(0, callbacks.clockReads)
            assertEquals(0, callbacks.telemetryCalls)
        } finally {
            delegate.shutdownNow()
        }
    }

    @Test
    fun cancellationWhileInactiveStillReleasesTheAcceptedQueueSlot() {
        val callbacks = Callbacks()
        val delegate = CapturingScheduler()
        val executor = JankHunterScheduledExecutorService(delegate, "cancel", null, callbacks.clock, callbacks)
        try {
            val future = executor.schedule(Runnable {}, 1L, TimeUnit.DAYS)
            callbacks.active = false
            callbacks.resetObservations()
            org.junit.Assert.assertTrue(future.cancel(false))
            assertEquals(0, callbacks.clockReads)
            assertEquals(0, callbacks.telemetryCalls)
            callbacks.active = true
            executor.schedule(Runnable {}, 1L, TimeUnit.DAYS)
            assertEquals(1, callbacks.lastQueueDepth)
            (delegate.command as Runnable).run()
            assertEquals(0, callbacks.lastQueueDepth)
        } finally {
            delegate.shutdownNow()
        }
    }

    @Test
    fun executorFeatureDisableIsHonoredByAnAlreadyCreatedWrapper() {
        val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1L })
        val config = JankHunterConfig.builder().build()
        graph.state.featureGate.activate(config)
        var reads = 0
        var command: Runnable? = null
        val executor = JankHunterExecutor(Executor { command = it }, "feature", null, { reads++; 1L }, graph.asyncTelemetry)
        graph.state.featureGate.activate(
            JankHunterConfig.builder().runtimeFeatureEnabled(JankHunterRuntimeFeature.EXECUTORS, false).build(),
        )
        val original = Runnable {}
        executor.execute(original)
        assertSame(original, command)
        assertEquals(0, reads)
    }

    private fun delegateFuture(delegate: CapturingScheduler, schedule: () -> ScheduledFuture<*>): ScheduledFuture<*> {
        delegate.future = null
        return schedule()
    }

    private class Callbacks : RuntimeAsyncCallbacks by JankHunter.asyncTelemetry() {
        var active = true
        var clockReads = 0
        var telemetryCalls = 0
        var lastQueueDepth = -1
        val clock = RuntimeLongSource { clockReads++; 1L }
        override fun isActive(): Boolean = active
        override fun isExecutorActive(): Boolean = active
        override fun recordExecutorQueueChanged(keys: ExecutorMetricKeys, executor: Executor, queued: Int) {
            telemetryCalls++
            lastQueueDepth = queued
        }
        override fun recordExecutorStarted(keys: ExecutorMetricKeys, executor: Executor, queued: Int, waitMs: Long, scheduled: Boolean) {
            telemetryCalls++
            lastQueueDepth = queued
        }
        override fun runExecutorTask(
            keys: ExecutorMetricKeys, ownerName: String?, context: JankHunterContext?, executor: Executor,
            queued: Int, waitMs: Long, command: Runnable, clock: RuntimeLongSource, scheduled: Boolean,
        ) {
            recordExecutorStarted(keys, executor, queued, waitMs, scheduled)
            telemetryCalls++
            command.run()
        }
        override fun <T> callExecutorTask(
            keys: ExecutorMetricKeys, ownerName: String?, context: JankHunterContext?, executor: Executor,
            queued: Int, waitMs: Long, callable: Callable<T>, clock: RuntimeLongSource, scheduled: Boolean,
        ): T {
            recordExecutorStarted(keys, executor, queued, waitMs, scheduled)
            telemetryCalls++
            return callable.call()
        }
        fun resetObservations() { clockReads = 0; telemetryCalls = 0 }
    }

    private class CapturingScheduler : ScheduledThreadPoolExecutor(1) {
        var command: Any? = null
        var future: ScheduledFuture<*>? = null
        var delay = 0L
        var period = 0L
        var unit: TimeUnit? = null
        override fun schedule(command: Runnable, delay: Long, unit: TimeUnit): ScheduledFuture<*> {
            this.command = command
            this.delay = delay
            this.unit = unit
            return ManualFuture(Executors.callable(command, null)).also { future = it }
        }
        override fun <V> schedule(callable: Callable<V>, delay: Long, unit: TimeUnit): ScheduledFuture<V> {
            command = callable
            this.delay = delay
            this.unit = unit
            return ManualFuture(callable).also { future = it }
        }
        override fun scheduleAtFixedRate(command: Runnable, initialDelay: Long, period: Long, unit: TimeUnit): ScheduledFuture<*> {
            this.period = period
            return schedule(command, initialDelay, unit)
        }
        override fun scheduleWithFixedDelay(command: Runnable, initialDelay: Long, delay: Long, unit: TimeUnit): ScheduledFuture<*> {
            period = delay
            return schedule(command, initialDelay, unit)
        }
    }

    private class ManualFuture<V>(callable: Callable<V>) : FutureTask<V>(callable), ScheduledFuture<V> {
        override fun getDelay(unit: TimeUnit): Long = 0L
        override fun compareTo(other: Delayed): Int = 0
    }
}
