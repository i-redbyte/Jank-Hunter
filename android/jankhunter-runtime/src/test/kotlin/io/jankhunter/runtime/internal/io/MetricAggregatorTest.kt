package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertEquals
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
