package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotSame
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Test

class RuntimeCallBatchPoolTest {
    @Test
    fun recycledBatchClearsReferencesAndIsReused() {
        val pool = RuntimeCallBatchPool(capacity = 1, batchCapacity = 4)
        val batch = pool.acquire()
        batch.add(
            screen = "screen",
            callerId = 1L,
            callerName = "caller",
            operationId = 2L,
            calleeId = 3L,
            calleeName = "callee",
            count = 4L,
            totalMs = 5L,
            maxMs = 6L,
        )

        batch.recycle()

        assertEquals(0, batch.size)
        assertNull(referenceArray(batch, "screens")[0])
        assertNull(referenceArray(batch, "callerNames")[0])
        assertNull(referenceArray(batch, "calleeNames")[0])
        assertSame(batch, pool.acquire())
    }

    @Test
    fun duplicateRecycleDoesNotPublishTheSameBatchTwice() {
        val pool = RuntimeCallBatchPool(capacity = 2, batchCapacity = 1)
        val batch = pool.acquire()

        batch.recycle()
        batch.recycle()

        assertSame(batch, pool.acquire())
        val second = pool.acquire()
        assertNotSame(batch, second)
    }

    private fun referenceArray(batch: RuntimeCallBatch, name: String): Array<*> {
        val field = RuntimeCallBatch::class.java.getDeclaredField(name).apply { isAccessible = true }
        return field.get(batch) as Array<*>
    }
}
