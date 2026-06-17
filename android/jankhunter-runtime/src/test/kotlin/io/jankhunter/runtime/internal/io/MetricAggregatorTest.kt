package io.jankhunter.runtime.internal.io

import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
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
        assertEquals(130L, sink.gauges["memory.pss"])
        assertEquals(160L, sink.gauges["memory.pss.max"])

        sink.clear()
        aggregator.flush(sink)
        assertEquals(emptyMap<String, Long>(), sink.counters)
        assertEquals(emptyMap<String, Long>(), sink.gauges)
    }

    @Test
    fun reportsDroppedKeysWhenHotMetricCardinalityExceedsLimit() {
        val aggregator = MetricAggregator(maxKeys = 1)
        val sink = RecordingSink()

        aggregator.counter("first", 1)
        aggregator.counter("second", 1)
        aggregator.gauge("third", 10)
        aggregator.flush(sink)

        assertEquals(1L, sink.counters["first"])
        assertEquals(2L, sink.counters["jankhunter.metric_aggregation.dropped.count"])
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

    private class RecordingSink : MetricAggregator.Sink {
        val counters = linkedMapOf<String, Long>()
        val gauges = linkedMapOf<String, Long>()

        override fun counter(name: String, value: Long) {
            counters[name] = value
        }

        override fun gauge(name: String, value: Long) {
            gauges[name] = value
        }

        fun clear() {
            counters.clear()
            gauges.clear()
        }
    }
}
