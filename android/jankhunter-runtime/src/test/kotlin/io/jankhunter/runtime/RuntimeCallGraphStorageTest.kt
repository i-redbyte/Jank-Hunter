package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.RuntimeCallBatch
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeCallGraphStorageTest {
    @Test
    fun tombstonesAreCompactedWithoutGrowingSparseTable() {
        val table = RuntimeGraphEdgeTable(contextAware = true)
        val buffer = RuntimeGraphEdgeBuffer(Thread.currentThread())
        repeat(11) { index ->
            setEdge(buffer, index.toLong())
            assertTrue(table.add(buffer, 0, 1_000))
        }

        val nearlyFullBatch = RuntimeCallBatch(RUNTIME_GRAPH_MAX_FLUSH_RECORDS)
        repeat(RUNTIME_GRAPH_MAX_FLUSH_RECORDS - 1) {
            nearlyFullBatch.add(null, 0L, null, null, 0L, 0L, 0L, 0L)
        }
        table.drainInto(nearlyFullBatch)
        assertEquals(10, table.size)

        setEdge(buffer, 100L)
        assertTrue(table.add(buffer, 0, 1_000))
        assertEquals(16, table.capacityForTest())
    }

    @Test
    fun nonLifoPopDiscardsInnerFramesWithoutPublishingAnEdge() {
        val stack = RuntimeCallStack()
        assertTrue(stack.push(1L, "outer", 1L, null, null, null))
        assertTrue(stack.push(2L, "inner", 2L, null, null, null))

        assertFalse(stack.pop(1L))
        assertEquals(0, stack.depth)
    }

    private fun setEdge(buffer: RuntimeGraphEdgeBuffer, id: Long) {
        buffer.callers[0] = id
        buffer.callees[0] = id + 1L
        buffer.screens[0] = "screen"
        buffer.flows[0] = "flow"
        buffer.steps[0] = "step"
        buffer.durationsMs[0] = 1L
    }
}
