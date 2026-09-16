package io.jankhunter.runtime.internal.io

import java.math.BigInteger
import java.util.Random
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Test

class MetricSumTest {
    @Test
    fun concurrentGaugeUpdatesPublishCountAndBothSumWordsTogether() {
        val aggregator = MetricAggregator(4, exactAdmission = true)
        val pool = Executors.newFixedThreadPool(4)
        val start = CountDownLatch(1)
        try {
            val jobs = List(4) {
                pool.submit {
                    check(start.await(2L, TimeUnit.SECONDS))
                    repeat(4_000) { aggregator.gauge("large", Long.MAX_VALUE) }
                }
            }
            start.countDown()
            jobs.forEach { it.get(5L, TimeUnit.SECONDS) }
            var emitted = 0
            aggregator.flush(object : MetricAggregator.Sink {
                override fun counter(name: String, value: Long) { throw AssertionError("unexpected counter $name=$value") }
                override fun gauge(name: String, value: Long, count: Long, sum: Long, max: Long, mode: MetricAggregationMode, sumHigh: Long) {
                    emitted++
                    assertEquals(16_000L, count)
                    assertEquals(-16_000L, sum)
                    assertEquals(7_999L, sumHigh)
                    assertEquals(Long.MAX_VALUE, value)
                    assertEquals(Long.MAX_VALUE, max)
                }
            })
            assertEquals(1, emitted)
        } finally {
            start.countDown()
            pool.shutdownNow()
        }
    }

    @Test
    fun roundedWideMeanMatchesExactArithmeticIncludingSignedLowWords() {
        val random = Random(13L)
        val mask = BigInteger.ONE.shiftLeft(64).subtract(BigInteger.ONE)
        repeat(2_048) {
            val count = (random.nextLong() and Long.MAX_VALUE).coerceAtLeast(1L)
            val quotient = (random.nextLong() and Long.MAX_VALUE).coerceAtMost(Long.MAX_VALUE - 1L)
            val remainder = (random.nextLong() and Long.MAX_VALUE) % count
            val sum = BigInteger.valueOf(quotient).multiply(BigInteger.valueOf(count)).add(BigInteger.valueOf(remainder))
            val expected = quotient + if (remainder >= count / 2L + count % 2L) 1L else 0L
            assertEquals(expected, roundedMetricAverage(sum.and(mask).toLong(), sum.shiftRight(64).toLong(), count))
        }
        assertEquals(Long.MAX_VALUE, roundedMetricAverage(-2L, 0L, 2L))
        assertEquals(1L shl 62, roundedMetricAverage(0L, 1L, 4L))
    }
}
