package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeGraphStorageLifetimeTest {
    @Test
    fun unusedProducerHasNoAggregatePages() {
        val buffer = RuntimeGraphAggregateBuffer(Thread.currentThread())
        assertEquals("unused producer must not allocate edge arrays", 0, allocatedPages(buffer))
    }

    @Test
    fun oneEdgeAllocatesOnePageAndRingReuseStaysBounded() {
        val buffer = RuntimeGraphAggregateBuffer(Thread.currentThread())
        assertEquals(RUNTIME_GRAPH_ADD_AGGREGATED, buffer.tryAdd(1L, "caller", 2L, "callee", null, 0L, 1L))
        assertEquals("one edge needs only its active page", 1, allocatedPages(buffer))
        repeat(RUNTIME_GRAPH_PAGE_QUEUE_CAPACITY * 3) {
            assertTrue(buffer.publishActivePage())
            val position = buffer.tryClaimConsumer()
            assertEquals(1L, buffer.pageAt(position).logicalEventCount())
            buffer.release(position)
            assertEquals(RUNTIME_GRAPH_ADD_AGGREGATED, buffer.tryAdd(1L, "caller", 2L, "callee", null, 0L, 1L))
            assertTrue(allocatedPages(buffer) <= RUNTIME_GRAPH_PAGE_QUEUE_CAPACITY)
        }
    }

    @Test
    fun drainedBuffersRetireWhileCompletedThreadObjectsRemainReachable() {
        val directory = File.createTempFile("jh-graph-lifetime", "").apply {
            check(delete())
            check(mkdirs())
        }
        val writer = AsyncLogWriterFactory().open(
            directory, JankHunterConfig.builder().autoStartCollectors(false).build(), "main",
        )
        val graph = RuntimeCallGraph({ 1L }, { null }, { 0L }, { 16 })
        val threads = List(8) { index ->
            Thread({
                val parent = graph.enter(1L, "parent", true)
                val child = graph.enter(2L, "child", true)
                graph.exit(child, 2L)
                graph.exit(parent, 1L)
            }, "short-producer-$index")
        }
        try {
            graph.resetFlushState(writer)
            threads.forEach(Thread::start)
            threads.forEach { it.join(2_000L); assertFalse(it.isAlive) }
            assertTrue(graph.flushBlocking(2_000L))
            assertEquals(8L, graph.acceptedForTest())
            assertEquals(8L, graph.emittedForTest())
            assertEquals(0L, graph.acceptedEventLossForTest())
            assertEquals("completed Thread retention must not retain graph buffers", 0, graph.registeredProducerCountForTest())
            assertEquals(8, threads.count { it.state == Thread.State.TERMINATED })
        } finally {
            graph.flushForShutdown(2_000L)
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    private fun allocatedPages(buffer: RuntimeGraphAggregateBuffer): Int {
        val field = RuntimeGraphAggregateBuffer::class.java.getDeclaredField("pages").apply { isAccessible = true }
        return (field.get(buffer) as Array<*>).count { it != null }
    }
}
