package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLong
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeCallGraphTest {
    @Test
    fun exitPopsPrimitiveStackWithoutWriter() {
        val graph = graph()
        val parent = graph.enter(0L, enabled = true)
        val child = graph.enter(-1L, enabled = true)

        graph.exit(child, -1L, writer = null)
        assertEquals(1, graph.currentThreadDepthForTest())

        graph.exit(parent, 0L, writer = null)
        assertEquals(0, graph.currentThreadDepthForTest())
    }

    @Test
    fun epochInvalidatesStaleTokenAndStack() {
        val graph = graph()
        val stale = graph.enter(42L, enabled = true)

        graph.resetFlushState()
        graph.exit(stale, 42L, writer = null)

        assertEquals(0, graph.currentThreadDepthForTest())
        assertNotEquals(stale, graph.enter(42L, enabled = true))
    }

    @Test
    fun zeroAndNegativeStableIdsAreAdmitted() = withWriter { writer ->
        val graph = graph()
        val parent = graph.enter(0L, enabled = true)
        val child = graph.enter(Long.MIN_VALUE, enabled = true)

        graph.exit(child, Long.MIN_VALUE, writer)
        graph.exit(parent, 0L, writer)

        assertEquals(1, graph.entryCountForTest())
    }

    @Test
    fun nonLifoExitDoesNotRecordFalseEdge() = withWriter { writer ->
        val graph = graph()
        val parent = graph.enter(1L, enabled = true)
        graph.enter(2L, enabled = true)

        graph.exit(parent, 1L, writer)

        assertEquals(0, graph.entryCountForTest())
        assertEquals(0, graph.currentThreadDepthForTest())
    }

    @Test
    fun maxKeysIsPreservedUnderConcurrentUniqueEdges() = withWriter { writer ->
        val graph = graph(maxKeys = 4)
        val pool = Executors.newFixedThreadPool(8)
        val start = CountDownLatch(1)
        val done = CountDownLatch(64)
        try {
            repeat(64) { index ->
                pool.execute {
                    start.await()
                    try {
                        val parent = graph.enter(1L, enabled = true)
                        val child = graph.enter(index.toLong() + 2L, enabled = true)
                        graph.exit(child, index.toLong() + 2L, writer)
                        graph.exit(parent, 1L, writer)
                    } finally {
                        done.countDown()
                    }
                }
            }

            start.countDown()
            assertTrue(done.await(2, TimeUnit.SECONDS))
            assertTrue("entry count exceeded cap: ${graph.entryCountForTest()}", graph.entryCountForTest() <= 4)
        } finally {
            pool.shutdownNow()
        }
    }

    @Test
    fun oneFlushPassRemovesAtMostOneBoundedBatch() = withWriter { writer ->
        val graph = graph(maxKeys = 512)
        repeat(300) { index ->
            val parentId = index.toLong() * 2L
            val childId = parentId + 1L
            val parent = graph.enter(parentId, enabled = true)
            val child = graph.enter(childId, enabled = true)
            graph.exit(child, childId, writer)
            graph.exit(parent, parentId, writer)
        }
        val before = graph.entryCountForTest()

        graph.flush(force = true, writer)

        val removed = before - graph.entryCountForTest()
        assertTrue("flush was not bounded: removed=$removed", removed in 1..128)
    }

    private fun graph(maxKeys: Int = 32): RuntimeCallGraph {
        val now = AtomicLong(1L)
        return RuntimeCallGraph(
            nowMs = { now.getAndIncrement() },
            captureContext = {
                JankHunterContext(screen = "screen", owner = null, flow = "flow", step = "step")
            },
            maxKeys = { maxKeys },
        )
    }

    private fun withWriter(block: (AsyncLogWriter) -> Unit) {
        val directory = Files.createTempDirectory("jankhunter-runtime-call-graph").toFile()
        val writer = AsyncLogWriter.open(
            directory,
            JankHunterConfig.builder()
                .autoStartCollectors(false)
                .flushIntervalMs(60_000)
                .build(),
            "main",
        )
        try {
            block(writer)
        } finally {
            writer.close()
            directory.deleteRecursively()
        }
    }
}
