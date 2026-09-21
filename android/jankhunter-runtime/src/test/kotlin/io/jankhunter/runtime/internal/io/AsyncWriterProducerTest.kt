package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterConfig
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class AsyncWriterProducerTest {
    @Test
    fun defaultWriterKeepsQueueSlotsLazy() {
        val producer = AsyncWriterProducer(JankHunterConfig.builder().build())

        assertTrue(producer.eventLanes.allocatedSlotCapacityForTest() <= 512)
    }

    @Test
    fun producerBurstCreatesOneWakePermit() {
        val producer = AsyncWriterProducer(JankHunterConfig.builder().build())

        assertTrue(producer.requestWorkerWake())
        repeat(10_000) { producer.requestWorkerWake() }

        assertEquals(1, producer.queuedEvents.availablePermits())
    }
}
