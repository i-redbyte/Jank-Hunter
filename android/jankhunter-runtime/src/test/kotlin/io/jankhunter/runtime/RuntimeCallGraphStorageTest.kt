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

        assertTrue(page.add(1L, "caller", 2L, "callee", "screen", 41L, 3L))
        assertTrue(page.add(1L, "caller", 2L, "callee", "screen", 41L, 7L))
        assertTrue(page.add(1L, "caller", 2L, "callee", "screen", 41L, 5L))

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
            assertTrue(page.add(index.toLong(), "caller-$index", index + 1L, "callee-$index", null, 0L, 1L))
        }

        assertFalse(page.add(10_000L, "caller-full", 10_001L, "callee-full", null, 0L, 1L))
        assertTrue(page.add(0L, "caller-0", 1L, "callee-0", null, 0L, 4L))
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
            nearlyFullBatch.add(null, 0L, "caller", 0L, 0L, "callee", 0L, 0L, 0L)
        }
        table.drainInto(nearlyFullBatch)
        assertEquals(10, table.size)

        page.clear()
        val source = setEdge(page, 100L)
        assertTrue(table.add(page, source, 1_000))
        assertEquals(16, table.capacityForTest())
    }

    @Test
    fun edgeTablePreservesNamesAcrossMergedPages() {
        val table = RuntimeGraphEdgeTable()
        val first = RuntimeGraphAggregatePage()
        val second = RuntimeGraphAggregatePage()
        assertTrue(first.add(1L, "caller", 2L, "callee", "screen", 41L, 3L))
        assertTrue(second.add(1L, "caller", 2L, "callee", "screen", 41L, 7L))

        assertTrue(table.add(first, first.nextOccupiedIndex(0), 1_000))
        assertTrue(table.add(second, second.nextOccupiedIndex(0), 1_000))
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
        stack.push(1L, "outer", 1L, null, 0L)
        stack.push(2L, "inner", 2L, null, 0L)

        assertFalse(stack.pop(1L))
        assertEquals(0, stack.depth)
    }

    @Test
    fun stackGrowsWithoutDroppingDeepApplicationCalls() {
        val stack = RuntimeCallStack()
        repeat(1_024) { depth ->
            stack.push(depth.toLong(), "method-$depth", depth.toLong(), null, 0L)
        }
        assertEquals(1_024, stack.depth)
        repeat(1_024) { offset ->
            assertTrue(stack.pop((1_023 - offset).toLong()))
        }
        assertEquals(0, stack.depth)
    }

    private fun setEdge(page: RuntimeGraphAggregatePage, id: Long): Int {
        assertTrue(page.add(id, "caller-$id", id + 1L, "callee-${id + 1L}", "screen", 41L, 1L))
        return page.nextOccupiedIndex(0)
    }
}
