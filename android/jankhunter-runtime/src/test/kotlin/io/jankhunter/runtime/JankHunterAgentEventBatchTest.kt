package io.jankhunter.runtime

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterAgentEventBatchTest {
    @Test
    fun packsReusesAndBoundsSemanticRecords() {
        val batch = JankHunterAgentEventBatch(1)
        assertTrue(
            batch.tryAppend(
                type = JankHunterAgentEventType.GC_INTERVAL,
                schemaVersion = 1,
                flags = -1,
                producerSequence = 9L,
                monotonicNs = 10L,
                producerId = 11L,
                threadToken = 12L,
                contextToken = 13L,
                payload0 = 14L,
                payload1 = 15L,
                payload2 = 16L,
                payload3 = Long.MIN_VALUE,
            ),
        )
        assertFalse(batch.tryAppend(1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0))
        assertEquals(JankHunterAgentEventType.GC_INTERVAL, batch.type(0))
        assertEquals(1, batch.schemaVersion(0))
        assertEquals(-1, batch.flags(0))
        assertEquals(9L, batch.producerSequence(0))
        assertEquals(Long.MIN_VALUE, batch.payload3(0))

        batch.clear()
        assertEquals(0, batch.size)
        assertFalse(batch.tryAppend(1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0))
        assertFalse(batch.tryAppend(1, 1, 0, 0, -1, 0, 0, 0, 0, 0, 0, 0))
    }

    @Test
    fun inMemorySinkCopiesBeforeProducerReusesBatch() {
        val batch = JankHunterAgentEventBatch(2)
        val sink = CopyingInMemorySink()
        assertTrue(batch.tryAppend(JankHunterAgentEventType.GC_INTERVAL, 1, 0, 7, 100, 1, 2, 3, 4, 5, 6, 7))
        assertTrue(sink.tryPublish(batch))
        batch.clear()
        assertTrue(batch.tryAppend(JankHunterAgentEventType.THREAD_END, 1, 0, 8, 200, 1, 2, 3, 0, 0, 0, 0))

        assertEquals(1, sink.batches.size)
        assertEquals(7L, sink.batches.single()[JankHunterAgentEventBatch.PRODUCER_SEQUENCE])
        assertEquals(100L, sink.batches.single()[JankHunterAgentEventBatch.MONOTONIC_NS])
    }

    private class CopyingInMemorySink : JankHunterAgentEventSink {
        val batches = mutableListOf<LongArray>()

        override fun tryPublish(batch: JankHunterAgentEventBatch): Boolean {
            batches += batch.copyPackedWords()
            return true
        }

        override fun tryPublishContext(
            contextToken: Long,
            screen: String?,
            owner: String?,
            flow: String?,
            step: String?,
        ): Boolean = true

        override fun tryPublishMethodDefinition(methodId: Long, symbol: String): Boolean = true
    }
}
