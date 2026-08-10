package io.jankhunter.runtime.internal.concurrent

import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicIntegerArray
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SpscSlotSequencerTest {
    @Test
    fun fullRingRejectsWithoutChangingProducerPosition() {
        val ring = SpscSlotSequencer(2)
        repeat(2) { expected ->
            val position = ring.tryClaimProducer()
            assertEquals(expected.toLong(), position)
            ring.publish(position)
        }

        assertEquals(SpscSlotSequencer.NO_POSITION, ring.tryClaimProducer())
        assertEquals(2L, ring.pendingCount())
    }

    @Test
    fun unpublishedPayloadIsInvisibleToConsumer() {
        val ring = SpscSlotSequencer(2)
        val position = ring.tryClaimProducer()

        assertEquals(SpscSlotSequencer.NO_POSITION, ring.tryClaimConsumer())

        ring.publish(position)
        assertEquals(position, ring.tryClaimConsumer())
    }

    @Test
    fun releaseAllowsDeterministicWraparound() {
        val ring = SpscSlotSequencer(4)
        repeat(1_000) { expected ->
            val produced = ring.tryClaimProducer()
            assertEquals(expected.toLong(), produced)
            ring.publish(produced)
            val consumed = ring.tryClaimConsumer()
            assertEquals(produced, consumed)
            ring.release(consumed)
            assertEquals(0L, ring.pendingCount())
        }
        assertTrue(ring.isEmpty())
    }

    @Test
    fun acquireObservesPayloadBeforePublication() {
        val eventCount = 100_000
        val ring = SpscSlotSequencer(64)
        val payload = AtomicIntegerArray(ring.capacity)
        val start = CountDownLatch(1)
        val done = CountDownLatch(2)
        val failed = AtomicBoolean(false)
        val producer = Thread {
            start.await()
            var value = 1
            while (value <= eventCount) {
                val position = ring.tryClaimProducer()
                if (position != SpscSlotSequencer.NO_POSITION) {
                    payload.set(ring.slotIndex(position), value)
                    ring.publish(position)
                    value++
                }
            }
            done.countDown()
        }
        val consumer = Thread {
            start.await()
            var expected = 1
            while (expected <= eventCount) {
                val position = ring.tryClaimConsumer()
                if (position != SpscSlotSequencer.NO_POSITION) {
                    if (payload.get(ring.slotIndex(position)) != expected) failed.set(true)
                    ring.release(position)
                    expected++
                }
            }
            done.countDown()
        }
        producer.start()
        consumer.start()
        start.countDown()

        assertTrue(done.await(10, TimeUnit.SECONDS))
        assertFalse(failed.get())
    }
}
