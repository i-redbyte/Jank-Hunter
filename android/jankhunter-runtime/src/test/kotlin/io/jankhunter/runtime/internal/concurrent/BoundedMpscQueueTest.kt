package io.jankhunter.runtime.internal.concurrent

import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class BoundedMpscQueueTest {
    @Test
    fun fullQueueRejectsWithoutWaitingAndReusesReleasedSlots() {
        val queue = BoundedMpscQueue<Int>(3)
        repeat(3) { value ->
            assertEquals(BoundedMpscQueue.OfferResult.OFFERED, queue.tryOffer(value))
        }
        assertFalse(queue.hasCapacity())
        assertEquals(BoundedMpscQueue.OfferResult.FULL, queue.tryOffer(3))
        assertEquals(0, queue.poll())
        assertTrue(queue.hasCapacity())
        assertEquals(BoundedMpscQueue.OfferResult.OFFERED, queue.tryOffer(3))
        assertEquals(1, queue.poll())
        assertEquals(2, queue.poll())
        assertEquals(3, queue.poll())
    }

    @Test
    fun multipleProducersPublishEachClaimedValueExactlyOnce() {
        val producers = 8
        val perProducer = 10_000
        val queue = BoundedMpscQueue<Int>(1024)
        val start = CountDownLatch(1)
        val done = CountDownLatch(producers)
        val next = AtomicInteger()
        val seen = ConcurrentHashMap.newKeySet<Int>()
        repeat(producers) {
            Thread {
                start.await()
                repeat(perProducer) {
                    val value = next.getAndIncrement()
                    while (true) {
                        when (queue.tryOffer(value)) {
                            BoundedMpscQueue.OfferResult.OFFERED -> break
                            BoundedMpscQueue.OfferResult.FULL,
                            BoundedMpscQueue.OfferResult.CONTENDED,
                            -> Thread.yield()
                        }
                    }
                }
                done.countDown()
            }.start()
        }
        start.countDown()
        while (done.count > 0L || !queue.isEmpty()) {
            queue.poll()?.let { value -> assertTrue(seen.add(value)) }
        }
        assertTrue(done.await(10, TimeUnit.SECONDS))
        assertEquals(producers * perProducer, seen.size)
    }
}
