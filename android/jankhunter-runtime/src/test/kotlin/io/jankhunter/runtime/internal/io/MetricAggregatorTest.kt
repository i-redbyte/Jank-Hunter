package io.jankhunter.runtime.internal.io

import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class MetricAggregatorTest {
    @Test
    fun foldsCountersAndGaugesIntoSingleFlush() {
        val aggregator = MetricAggregator(maxKeys = 8)
        val sink = RecordingSink()

        aggregator.counter("runtime.call", 2)
        aggregator.counter("runtime.call", 3)
        aggregator.gauge("memory.pss", 100)
        aggregator.gauge("memory.pss", 160)

        aggregator.flush(sink)

        assertEquals(mapOf("runtime.call" to 5L), sink.counters)
        assertEquals(
            RecordingGauge(
                value = 130L,
                count = 2L,
                sum = 260L,
                max = 160L,
                mode = MetricAggregationMode.AVERAGE,
            ),
            sink.gauges["memory.pss"],
        )

        sink.clear()
        aggregator.flush(sink)
        assertEquals(emptyMap<String, Long>(), sink.counters)
        assertEquals(emptyMap<String, Long>(), sink.gauges)
    }

    @Test
    fun evictsLeastRecentlyUsedKeysWhenHotMetricCardinalityExceedsLimit() {
        val aggregator = MetricAggregator(maxKeys = 2)
        val sink = RecordingSink()

        aggregator.counter("first", 1)
        aggregator.counter("second", 1)
        aggregator.counter("first", 1)
        aggregator.counter("third", 1)
        aggregator.flush(sink)

        assertEquals(2L, sink.counters["first"])
        assertEquals(1L, sink.counters["third"])
        assertEquals(null, sink.counters["second"])
        assertEquals(1L, sink.counters["jankhunter.metric_aggregation.dropped.count"])
    }

    @Test
    fun dropsNegativeMetricValuesWithInvalidCounter() {
        val aggregator = MetricAggregator(maxKeys = 8)
        val sink = RecordingSink()

        aggregator.counter("bad.counter", -1)
        aggregator.gauge("bad.gauge", -10)
        aggregator.flush(sink)

        assertEquals(2L, sink.counters["jankhunter.metric.invalid_negative.count"])
        assertEquals(null, sink.counters["bad.counter"])
        assertEquals(null, sink.gauges["bad.gauge"])
    }

    @Test
    fun preservesStateAndBooleanGaugeSemantics() {
        val aggregator = MetricAggregator(maxKeys = 8)
        val sink = RecordingSink()

        aggregator.gauge("battery.status", 2, MetricAggregationMode.STATE)
        aggregator.gauge("battery.status", 5, MetricAggregationMode.STATE)
        aggregator.gauge("battery.charging", 1, MetricAggregationMode.BOOLEAN_RATE)
        aggregator.gauge("battery.charging", 0, MetricAggregationMode.BOOLEAN_RATE)
        aggregator.flush(sink)

        assertEquals(
            RecordingGauge(
                value = 5L,
                count = 2L,
                sum = 5L,
                max = 5L,
                mode = MetricAggregationMode.STATE,
            ),
            sink.gauges["battery.status"],
        )
        assertEquals(
            RecordingGauge(
                value = 50L,
                count = 2L,
                sum = 1L,
                max = 1L,
                mode = MetricAggregationMode.BOOLEAN_RATE,
            ),
            sink.gauges["battery.charging"],
        )
    }

    @Test
    fun saturatesLongAggregatesInsteadOfWrappingNegative() {
        val aggregator = MetricAggregator(maxKeys = 8)
        val sink = RecordingSink()

        aggregator.counter("large.counter", Long.MAX_VALUE)
        aggregator.counter("large.counter", 1L)
        aggregator.gauge("large.gauge", Long.MAX_VALUE)
        aggregator.gauge("large.gauge", 1L)
        aggregator.flush(sink)

        assertEquals(Long.MAX_VALUE, sink.counters["large.counter"])
        assertEquals(Long.MAX_VALUE, sink.gauges["large.gauge"]?.sum)
        assertTrue((sink.gauges["large.gauge"]?.value ?: -1L) >= 0L)
    }

    @Test
    fun valuesRecordedDuringEmissionStayInTheNextBatch() {
        val aggregator = MetricAggregator(maxKeys = 8)
        val firstSink = RecordingSink()
        aggregator.counter("first", 1L)

        aggregator.flush(object : MetricAggregator.Sink by firstSink {
            override fun counter(name: String, value: Long) {
                firstSink.counter(name, value)
                aggregator.counter("second", 2L)
            }
        })

        assertEquals(mapOf("first" to 1L), firstSink.counters)
        val secondSink = RecordingSink()
        aggregator.flush(secondSink)
        assertEquals(mapOf("second" to 2L), secondSink.counters)
    }

    @Test
    fun enforcesMaxKeysUnderConcurrentUniqueMetrics() {
        val aggregator = MetricAggregator(maxKeys = 8)
        val ready = CountDownLatch(32)
        val start = CountDownLatch(1)
        val executor = Executors.newFixedThreadPool(32)
        try {
            repeat(32) { index ->
                executor.execute {
                    ready.countDown()
                    assertTrue(start.await(2, TimeUnit.SECONDS))
                    aggregator.counter("metric.$index", 1)
                }
            }
            assertTrue(ready.await(2, TimeUnit.SECONDS))
            start.countDown()
        } finally {
            executor.shutdown()
            assertTrue(executor.awaitTermination(2, TimeUnit.SECONDS))
        }

        val sink = RecordingSink()
        aggregator.flush(sink)

        val accepted = sink.counters.keys.count { it.startsWith("metric.") }
        assertTrue("accepted=$accepted counters=${sink.counters}", accepted <= 8)
        assertEquals(24L, sink.counters["jankhunter.metric_aggregation.dropped.count"])
    }

    @Test
    fun concurrentUpdatesRemainLosslessWhileFlushSwapsBatches() {
        val aggregator = MetricAggregator(maxKeys = 8)
        val workerCount = 8
        val running = AtomicBoolean(true)
        val workersStarted = CountDownLatch(workerCount)
        val firstFlushEntered = CountDownLatch(1)
        val releaseFirstFlush = CountDownLatch(1)
        val updateDuringFirstFlush = CountDownLatch(1)
        val blockFirstDelivery = AtomicBoolean(true)
        val flushDeliveryBlocked = AtomicBoolean()
        val produced = AtomicLong()
        val flushCount = AtomicInteger()
        val recorded = AtomicLong()
        val sink = object : MetricAggregator.Sink {
            override fun counter(name: String, value: Long) {
                if (name != "shared.counter") return
                if (blockFirstDelivery.compareAndSet(true, false)) {
                    flushDeliveryBlocked.set(true)
                    firstFlushEntered.countDown()
                    assertTrue(releaseFirstFlush.await(5, TimeUnit.SECONDS))
                    flushDeliveryBlocked.set(false)
                }
                recorded.addAndGet(value)
            }

            override fun gauge(
                name: String,
                value: Long,
                count: Long,
                sum: Long,
                max: Long,
                mode: MetricAggregationMode,
            ) = Unit
        }
        val producers = Executors.newFixedThreadPool(workerCount)
        val flusher = Executors.newSingleThreadExecutor()
        try {
            val workerFutures = List(workerCount) {
                producers.submit {
                    aggregator.counter("shared.counter", 1L)
                    produced.incrementAndGet()
                    workersStarted.countDown()
                    while (running.get()) {
                        aggregator.counter("shared.counter", 1L)
                        produced.incrementAndGet()
                        if (flushDeliveryBlocked.get()) {
                            updateDuringFirstFlush.countDown()
                        }
                    }
                }
            }

            assertTrue(workersStarted.await(5, TimeUnit.SECONDS))
            val flusherFuture = flusher.submit {
                repeat(500) {
                    aggregator.flush(sink)
                    flushCount.incrementAndGet()
                }
            }
            assertTrue(firstFlushEntered.await(5, TimeUnit.SECONDS))
            assertTrue(updateDuringFirstFlush.await(5, TimeUnit.SECONDS))
            releaseFirstFlush.countDown()
            flusherFuture.get(10, TimeUnit.SECONDS)
            running.set(false)
            workerFutures.forEach { it.get(10, TimeUnit.SECONDS) }
        } finally {
            releaseFirstFlush.countDown()
            running.set(false)
            producers.shutdownNow()
            flusher.shutdownNow()
            assertTrue(producers.awaitTermination(10, TimeUnit.SECONDS))
            assertTrue(flusher.awaitTermination(10, TimeUnit.SECONDS))
        }

        aggregator.flush(sink)
        assertTrue(flushCount.get() > 0)
        assertEquals(produced.get(), recorded.get())
    }

    @Test
    fun concurrentEvictionAccountsForEverySampleWithoutWaitingForWriters() {
        val aggregator = MetricAggregator(maxKeys = 4)
        val workerCount = 8
        val updatesPerWorker = 2_000
        val start = CountDownLatch(1)
        val executor = Executors.newFixedThreadPool(workerCount)
        try {
            val futures = List(workerCount) { worker ->
                executor.submit {
                    assertTrue(start.await(2, TimeUnit.SECONDS))
                    repeat(updatesPerWorker) { update ->
                        aggregator.counter("metric.${(worker + update) and 15}", 1L)
                    }
                }
            }
            start.countDown()
            futures.forEach { it.get(10, TimeUnit.SECONDS) }
        } finally {
            executor.shutdownNow()
            assertTrue(executor.awaitTermination(10, TimeUnit.SECONDS))
        }

        val sink = RecordingSink()
        aggregator.flush(sink)
        val retained = sink.counters
            .filterKeys { it.startsWith("metric.") }
            .values
            .sum()
        val dropped = sink.counters[MetricAggregator.DROPPED_METRIC_NAME] ?: 0L

        assertEquals(workerCount.toLong() * updatesPerWorker, retained + dropped)
    }

    @Test
    fun concurrentGaugeKeepsLastValueAndModeFromTheSameSample() {
        val aggregator = MetricAggregator(maxKeys = 8)
        val start = CountDownLatch(1)
        val executor = Executors.newFixedThreadPool(4)
        try {
            val futures = listOf(
                101L to MetricAggregationMode.STATE,
                202L to MetricAggregationMode.LAST,
                101L to MetricAggregationMode.STATE,
                202L to MetricAggregationMode.LAST,
            ).map { (value, mode) ->
                executor.submit {
                    assertTrue(start.await(2, TimeUnit.SECONDS))
                    repeat(5_000) {
                        aggregator.gauge("mixed.gauge", value, mode)
                    }
                }
            }
            start.countDown()
            futures.forEach { it.get(10, TimeUnit.SECONDS) }
        } finally {
            executor.shutdownNow()
            assertTrue(executor.awaitTermination(10, TimeUnit.SECONDS))
        }

        val sink = RecordingSink()
        aggregator.flush(sink)
        val gauge = requireNotNull(sink.gauges["mixed.gauge"])
        when (gauge.mode) {
            MetricAggregationMode.STATE -> assertEquals(101L, gauge.value)
            MetricAggregationMode.LAST -> assertEquals(202L, gauge.value)
            else -> throw AssertionError("unexpected mode ${gauge.mode}")
        }
        assertEquals(20_000L, gauge.count)
    }

    private class RecordingSink : MetricAggregator.Sink {
        val counters = linkedMapOf<String, Long>()
        val gauges = linkedMapOf<String, RecordingGauge>()

        override fun counter(name: String, value: Long) {
            counters[name] = value
        }

        override fun gauge(
            name: String,
            value: Long,
            count: Long,
            sum: Long,
            max: Long,
            mode: MetricAggregationMode,
        ) {
            gauges[name] = RecordingGauge(value, count, sum, max, mode)
        }

        fun clear() {
            counters.clear()
            gauges.clear()
        }
    }

    private data class RecordingGauge(
        val value: Long,
        val count: Long,
        val sum: Long,
        val max: Long,
        val mode: MetricAggregationMode,
    )
}
