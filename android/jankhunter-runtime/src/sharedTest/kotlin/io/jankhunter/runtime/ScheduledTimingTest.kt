package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.MetricAggregationMode
import io.jankhunter.runtime.internal.io.MetricAggregator
import java.nio.file.Files
import java.util.concurrent.Callable
import java.util.concurrent.Delayed
import java.util.concurrent.Executors
import java.util.concurrent.FutureTask
import java.util.concurrent.ScheduledFuture
import java.util.concurrent.ScheduledThreadPoolExecutor
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Test

class ScheduledTimingTest {
    @Test
    fun oneShotDelayIsNotQueueWaitAndZeroLatenessIsPreserved() = Fixture().use { test ->
        var executions = 0
        test.executor.schedule({ executions++ }, 1_000L, TimeUnit.MILLISECONDS)
        test.now = 1_100L
        test.run()
        assertEquals(1, executions)
        val metrics = test.snapshot()
        assertEquals(1_000L, metrics[DELAY])
        assertEquals(0L, metrics[LATENESS])
        assertFalse("planned delay was recorded as contention", metrics.containsKey(WAIT))
    }

    @Test
    fun scheduledCallablePreservesResultAndSeparatesActualLateness() = Fixture().use { test ->
        val result = Any()
        test.executor.schedule(Callable { result }, 1L, TimeUnit.SECONDS)
        test.now = 1_107L
        assertSame(result, (test.delegate.command as Callable<*>).call())
        val metrics = test.snapshot()
        assertEquals(1_000L, metrics[DELAY])
        assertEquals(7L, metrics[LATENESS])
        assertFalse(metrics.containsKey(WAIT))
    }

    @Test
    fun fixedRateUsesTheOriginalScheduleForEveryInvocation() = Fixture().use { test ->
        val future = test.executor.scheduleAtFixedRate({ test.now += 50L }, 1_000L, 200L, TimeUnit.MILLISECONDS)
        try {
            for ((at, late) in listOf(1_105L to 5L, 1_370L to 70L, 1_500L to 0L)) {
                test.now = at
                test.run()
                val metrics = test.snapshot()
                assertEquals("fixed-rate due drifted after execution at $at", late, metrics[LATENESS])
                assertFalse(metrics.containsKey(WAIT))
            }
        } finally { future.cancel(false) }
    }

    @Test
    fun fixedDelayUsesThePreviousCompletionForEveryInvocation() = Fixture().use { test ->
        val future = test.executor.scheduleWithFixedDelay({ test.now += 50L }, 1_000L, 200L, TimeUnit.MILLISECONDS)
        try {
            for ((at, late) in listOf(1_105L to 5L, 1_362L to 7L, 1_612L to 0L)) {
                test.now = at
                test.run()
                val metrics = test.snapshot()
                assertEquals("fixed-delay ignored preceding completion at $at", late, metrics[LATENESS])
                assertFalse(metrics.containsKey(WAIT))
            }
        } finally { future.cancel(false) }
    }

    @Test
    fun configuredDelayUsesMillisecondsWithoutPrematureNanosecondSaturation() = Fixture().use { test ->
        val future = test.executor.schedule({}, Long.MAX_VALUE, TimeUnit.MILLISECONDS)
        try { assertEquals(Long.MAX_VALUE, test.snapshot()[DELAY]) } finally { future.cancel(false) }
    }

    @Test
    fun twoHugeConfiguredDelaysDoNotBecomeAHalvedAverage() = Fixture().use { test ->
        val first = test.executor.schedule({}, Long.MAX_VALUE, TimeUnit.MILLISECONDS)
        val second = test.executor.schedule({}, Long.MAX_VALUE, TimeUnit.MILLISECONDS)
        try { assertEquals(Long.MAX_VALUE, test.snapshot()[DELAY]) } finally {
            first.cancel(false)
            second.cancel(false)
        }
    }

    @Test
    fun subMillisecondFixedRateDoesNotAccumulateRoundingDrift() = Fixture().use { test ->
        val future = test.executor.scheduleAtFixedRate({}, 0L, 500L, TimeUnit.MICROSECONDS)
        try {
            repeat(2_001) { index -> test.nowNs = 100_000_000L + index * 500_000L; test.run() }
            assertEquals(0L, test.snapshot()[LATENESS])
        } finally { future.cancel(false) }
    }

    @Test
    fun fixedDelayReadsOnlyItsCompletionClockWhileDisabledAndResumesExactLateness() = Fixture().use { test ->
        val future = test.executor.scheduleWithFixedDelay({ test.now += 50L }, 0L, 100L, TimeUnit.MILLISECONDS)
        try {
            test.run()
            test.snapshot()
            test.setActive(false)
            test.now = 250L
            val scheduledBefore = test.scheduledReads
            val serviceBefore = test.serviceReads
            test.run()
            assertEquals(1, test.scheduledReads - scheduledBefore)
            assertEquals(0, test.serviceReads - serviceBefore)
            assertEquals(emptyMap<String, Long>(), test.snapshot())
            test.setActive(true)
            test.now = 401L
            test.run()
            assertEquals(1L, test.snapshot()[LATENESS])
        } finally { future.cancel(false) }
    }

    @Test
    fun executeOnScheduledExecutorKeepsOrdinaryQueueWait() = Fixture().use { test ->
        test.executor.execute {}
        test.now = 110L
        test.run()
        val metrics = test.snapshot()
        assertEquals(10L, metrics[WAIT])
        assertFalse(metrics.containsKey(LATENESS))
        assertFalse(metrics.containsKey(DELAY))
    }

    @Test
    fun deadlineSubtractionPreservesLatenessAcrossSignedNanoTimeWrap() = Fixture(Long.MAX_VALUE - 20_000_000L).use { test ->
        test.executor.schedule({}, 30L, TimeUnit.MILLISECONDS)
        test.nowNs += 37_000_000L
        test.run()
        assertEquals(7L, test.snapshot()[LATENESS])
    }

    @Test
    fun negativeInitialDelayIsImmediate() = Fixture().use { test ->
        test.executor.schedule({}, -100L, TimeUnit.MILLISECONDS)
        test.now += 5L
        test.run()
        val metrics = test.snapshot()
        assertEquals(0L, metrics[DELAY])
        assertEquals(5L, metrics[LATENESS])
    }

    @Test
    fun failedTimingClockDoesNotPreventApplicationExecutionOrInventLateness() = Fixture().use { test ->
        var executed = false
        test.failScheduledClock = true
        test.executor.schedule({ executed = true }, 0L, TimeUnit.MILLISECONDS)
        test.failScheduledClock = false
        test.run()
        assertEquals(true, executed)
        assertFalse(test.snapshot().containsKey(LATENESS))
    }

    private class Fixture(initialNs: Long = 100_000_000L) : AutoCloseable {
        var nowNs = initialNs
        var now: Long
            get() = nowNs / 1_000_000L
            set(value) { nowNs = value * 1_000_000L }
        var scheduledReads = 0
        var serviceReads = 0
        var failScheduledClock = false
        val delegate = ManualScheduler()
        private val directory = Files.createTempDirectory("scheduled-timing").toFile()
        private val config = JankHunterConfig.builder().autoStartCollectors(false).metricAggregationEnabled(true).build()
        private val writer = AsyncLogWriterFactory().open(directory, config, "scheduled-timing")
        private val graph = RuntimeComponentGraph(nowMs = { now }, nowUs = { now * 1_000L })
        val executor = JankHunterScheduledExecutorService(delegate, "timing", null, { serviceReads++; now }, graph.asyncTelemetry, scheduledClock = {
            scheduledReads++
            check(!failScheduledClock)
            nowNs
        })

        init {
            graph.state.writer = writer
            graph.state.config = config
            graph.metrics.configure(128, exactAdmission = true)
            graph.coordinator.markStarted(config)
        }

        fun run() = (delegate.command as Runnable).run()

        fun setActive(active: Boolean) {
            if (active) graph.state.featureGate.activate(config) else graph.state.featureGate.deactivate()
        }

        fun snapshot(): Map<String, Long> {
            val aggregator = RuntimeMetricsService::class.java.getDeclaredField("aggregator").run {
                isAccessible = true
                get(graph.metrics) as MetricAggregator
            }
            val values = mutableMapOf<String, Long>()
            aggregator.flush(object : MetricAggregator.Sink {
                override fun counter(name: String, value: Long) { values[name] = value }
                override fun gauge(name: String, value: Long, count: Long, sum: Long, max: Long, mode: MetricAggregationMode, sumHigh: Long) {
                    values[name] = value
                }
            })
            return values
        }

        override fun close() {
            delegate.shutdownNow()
            graph.session.stop(clearInit = true)
            writer.close()
            directory.deleteRecursively()
        }
    }

    private class ManualScheduler : ScheduledThreadPoolExecutor(1) {
        var command: Any? = null
        override fun schedule(command: Runnable, delay: Long, unit: TimeUnit): ScheduledFuture<*> {
            this.command = command
            return ManualFuture(Executors.callable(command, null))
        }
        override fun <V> schedule(callable: Callable<V>, delay: Long, unit: TimeUnit): ScheduledFuture<V> {
            command = callable
            return ManualFuture(callable)
        }
        override fun scheduleAtFixedRate(command: Runnable, initialDelay: Long, period: Long, unit: TimeUnit): ScheduledFuture<*> =
            schedule(command, initialDelay, unit)
        override fun scheduleWithFixedDelay(command: Runnable, initialDelay: Long, delay: Long, unit: TimeUnit): ScheduledFuture<*> =
            schedule(command, initialDelay, unit)
    }

    private class ManualFuture<V>(callable: Callable<V>) : FutureTask<V>(callable), ScheduledFuture<V> {
        override fun getDelay(unit: TimeUnit): Long = 0L
        override fun compareTo(other: Delayed): Int = 0
    }

    private companion object {
        const val DELAY = "executor.timing.scheduled_delay_ms"
        const val LATENESS = "executor.timing.scheduled_lateness_ms"
        const val WAIT = "executor.timing.wait_ms"
    }
}
