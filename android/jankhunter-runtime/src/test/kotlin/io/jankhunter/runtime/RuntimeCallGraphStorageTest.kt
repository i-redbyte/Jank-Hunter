package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.RuntimeCallBatch
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeCallGraphStorageTest {
    @Test
    fun producerPageAggregatesRepeatedEdgeExactly() {
        val page = RuntimeGraphAggregatePage()

        assertTrue(page.add(1L, null, 2L, null, "screen", "flow", "step", 3L))
        assertTrue(page.add(1L, "caller", 2L, "callee", "screen", "flow", "step", 7L))
        assertTrue(page.add(1L, "caller", 2L, "callee", "screen", "flow", "step", 5L))

        val index = page.nextOccupiedIndex(0)
        assertEquals(1, page.size)
        assertEquals(3L, page.logicalEventCount())
        assertEquals(3L, page.counts[index])
        assertEquals(15L, page.totalsMs[index])
        assertEquals(7L, page.maximaMs[index])
        assertEquals("caller", page.callerNames[index])
        assertEquals("callee", page.calleeNames[index])
    }

    @Test
    fun producerPageStopsAtKeyLimitWithoutCorruptingExistingAggregates() {
        val page = RuntimeGraphAggregatePage()
        repeat(RUNTIME_GRAPH_PAGE_MAX_KEYS) { index ->
            assertTrue(page.add(index.toLong(), null, index + 1L, null, null, null, null, 1L))
        }

        assertFalse(page.add(10_000L, null, 10_001L, null, null, null, null, 1L))
        assertTrue(page.add(0L, null, 1L, null, null, null, null, 4L))
        assertEquals(RUNTIME_GRAPH_PAGE_MAX_KEYS, page.size)
        assertEquals(RUNTIME_GRAPH_PAGE_MAX_KEYS.toLong() + 1L, page.logicalEventCount())
    }

    @Test
    fun tombstonesAreCompactedWithoutGrowingSparseTable() {
        val table = RuntimeGraphEdgeTable()
        val page = RuntimeGraphAggregatePage()
        repeat(11) { index ->
            page.clear()
            val source = setEdge(page, index.toLong())
            assertTrue(table.add(page, source, 1_000))
        }

        val nearlyFullBatch = RuntimeCallBatch(RUNTIME_GRAPH_MAX_FLUSH_RECORDS)
        repeat(RUNTIME_GRAPH_MAX_FLUSH_RECORDS - 1) {
            nearlyFullBatch.add(null, 0L, null, null, 0L, 0L, 0L, 0L)
        }
        table.drainInto(nearlyFullBatch)
        assertEquals(10, table.size)

        page.clear()
        val source = setEdge(page, 100L)
        assertTrue(table.add(page, source, 1_000))
        assertEquals(16, table.capacityForTest())
    }

    @Test
    fun edgeTablePreservesNamesThatArriveAfterTheFirstPage() {
        val table = RuntimeGraphEdgeTable()
        val unnamed = RuntimeGraphAggregatePage()
        val named = RuntimeGraphAggregatePage()
        assertTrue(unnamed.add(1L, null, 2L, null, "screen", "flow", "step", 3L))
        assertTrue(named.add(1L, "caller", 2L, "callee", "screen", "flow", "step", 7L))

        assertTrue(table.add(unnamed, unnamed.nextOccupiedIndex(0), 1_000))
        assertTrue(table.add(named, named.nextOccupiedIndex(0), 1_000))
        val batch = RuntimeCallBatch(RUNTIME_GRAPH_MAX_FLUSH_RECORDS)
        table.drainInto(batch)

        assertEquals(1, batch.size)
        assertEquals("caller", batch.callerName(0))
        assertEquals("callee", batch.calleeName(0))
        assertEquals(2L, batch.count(0))
        assertEquals(10L, batch.totalMs(0))
        assertEquals(7L, batch.maxMs(0))
    }

    @Test
    fun nonLifoPopDiscardsInnerFramesWithoutPublishingAnEdge() {
        val stack = RuntimeCallStack()
        stack.push(1L, "outer", 1L, null, null, null)
        stack.push(2L, "inner", 2L, null, null, null)

        assertFalse(stack.pop(1L))
        assertEquals(0, stack.depth)
    }

    @Test
    fun stackGrowsWithoutDroppingDeepApplicationCalls() {
        val stack = RuntimeCallStack()
        repeat(1_024) { depth ->
            stack.push(depth.toLong(), "method-$depth", depth.toLong(), null, null, null)
        }
        assertEquals(1_024, stack.depth)
        repeat(1_024) { offset ->
            assertTrue(stack.pop((1_023 - offset).toLong()))
        }
        assertEquals(0, stack.depth)
    }

    private fun setEdge(page: RuntimeGraphAggregatePage, id: Long): Int {
        assertTrue(page.add(id, null, id + 1L, null, "screen", "flow", "step", 1L))
        return page.nextOccupiedIndex(0)
    }
}
