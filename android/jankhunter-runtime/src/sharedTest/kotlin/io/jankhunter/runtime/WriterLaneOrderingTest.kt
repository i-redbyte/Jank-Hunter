package io.jankhunter.runtime

import io.jankhunter.runtime.internal.concurrent.BoundedMpscQueue.OfferResult
import io.jankhunter.runtime.internal.io.AsyncEventLanes
import io.jankhunter.runtime.internal.io.LogEventLane
import io.jankhunter.runtime.internal.io.PendingCounterEvent
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class WriterLaneOrderingTest {
    @Test
    fun concurrentPublicationAcrossBothLanesPreservesGlobalAdmissionOrder() {
        val lanes = AsyncEventLanes(bulkCapacity = 2, criticalCapacity = 2)
        val start = CountDownLatch(1)
        val stop = AtomicBoolean()
        val failure = AtomicReference<Throwable?>()
        val producer = Thread {
            try {
                check(start.await(5L, TimeUnit.SECONDS))
                for (sequence in 1L..EVENTS) {
                    val event = PendingCounterEvent(null, "lane-order", sequence).also { it.sequence = sequence }
                    val lane = if (sequence and 1L == 1L) LogEventLane.CRITICAL else LogEventLane.BULK
                    while (lanes.tryOffer(lane, event) != OfferResult.OFFERED) {
                        if (stop.get()) return@Thread
                        Thread.yield()
                    }
                }
            } catch (error: Throwable) {
                failure.set(error)
            }
        }
        producer.start()
        start.countDown()
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(30L)
        var consumed = 0L
        try {
            while (consumed < EVENTS && System.nanoTime() < deadline) {
                failure.get()?.let { throw it }
                val event = lanes.pollNext()
                if (event == null) {
                    Thread.yield()
                } else {
                    assertEquals("global writer order", ++consumed, event.sequence)
                }
            }
            assertEquals("consumer did not drain", EVENTS, consumed)
            assertTrue(lanes.hasCapacity(LogEventLane.CRITICAL))
            assertTrue(lanes.hasCapacity(LogEventLane.BULK))
            assertFalse(lanes.hasEvents())
        } finally {
            stop.set(true)
            producer.join(5_000L)
            if (producer.isAlive) producer.interrupt()
        }
        assertFalse("producer did not finish", producer.isAlive)
    }

    private companion object {
        const val EVENTS = 1_000_000L
    }
}
