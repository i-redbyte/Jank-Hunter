package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Test

class PendingRuntimeCallsEventPoolTest {
    @Test
    fun recycledEventClearsBatchAndProducerContextBeforeReuse() {
        val batchPool = RuntimeCallBatchPool(capacity = 1, batchCapacity = 1)
        val eventPool = PendingRuntimeCallsEventPool(capacity = 1)
        val batch = batchPool.acquire().apply {
            add("screen", 1L, "caller", 2L, 3L, "callee", 4L, 5L, 6L)
        }
        val event = eventPool.acquire(LogEventContext("screen", "owner", 2L), batch)

        event.recycle()

        assertNull(field(PendingRuntimeCallsEvent::class.java, "batch").get(event))
        assertNull(field(PendingLogEvent::class.java, "producerContext").get(event))
        assertSame(event, eventPool.acquire(null, batchPool.acquire()))
    }

    @Test
    fun preAdmissionRetryReusesWrapperWithoutRecyclingCallerOwnedBatch() {
        val batchPool = RuntimeCallBatchPool(capacity = 1, batchCapacity = 1)
        val eventPool = PendingRuntimeCallsEventPool(capacity = 1)
        val batch = batchPool.acquire()
        val event = eventPool.acquire(null, batch)

        event.rejectBeforeAdmission()

        assertSame(event, eventPool.acquire(null, batch))
        batch.add("screen", 1L, "caller", 2L, 3L, "callee", 4L, 5L, 6L)
    }

    private fun field(type: Class<*>, name: String) =
        type.getDeclaredField(name).apply { isAccessible = true }
}
