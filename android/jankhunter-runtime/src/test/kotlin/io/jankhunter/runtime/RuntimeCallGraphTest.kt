package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLong
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeCallGraphTest {
    @Test
    fun exitPopsPrimitiveStack() = withGraph { graph ->
        val parent = graph.enter(0L, enabled = true)
        val child = graph.enter(-1L, enabled = true)

        graph.exit(child, -1L)
        assertEquals(1, graph.currentThreadDepthForTest())

        graph.exit(parent, 0L)
        assertEquals(0, graph.currentThreadDepthForTest())
    }

    @Test
    fun stoppedEpochInvalidatesStaleTokenAndStack() = withGraph { graph ->
        val stale = graph.enter(42L, enabled = true)

        graph.flushForShutdown()
        graph.clear()

        assertEquals(0, graph.currentThreadDepthForTest())
        assertEquals(0L, graph.enter(42L, enabled = true))
        graph.exit(stale, 42L)
    }

    @Test
    fun zeroAndNegativeStableIdsArePublished() = withGraph { graph ->
        val parent = graph.enter(0L, enabled = true)
        val child = graph.enter(Long.MIN_VALUE, enabled = true)

        graph.exit(child, Long.MIN_VALUE)
        graph.exit(parent, 0L)

        assertEquals(1L, graph.acceptedForTest())
        assertTrue(graph.flushBlocking(2_000L))
        assertEquals(1L, graph.emittedForTest() + graph.acceptedEventLossForTest())
    }

    @Test
    fun nonLifoExitDoesNotPublishFalseEdge() = withGraph { graph ->
        val parent = graph.enter(1L, enabled = true)
        graph.enter(2L, enabled = true)

        graph.exit(parent, 1L)

        assertEquals(0L, graph.acceptedForTest())
        assertEquals(0, graph.currentThreadDepthForTest())
    }

    @Test
    fun contextIsCapturedAtCalleeEntry() = withGraph(
        initialScreen = "screen-a",
    ) { graph, screen ->
        val parent = graph.enter(1L, enabled = true)
        val childA = graph.enter(2L, enabled = true)
        screen.set("screen-b")
        graph.exit(childA, 2L)
        val childB = graph.enter(2L, enabled = true)
        graph.exit(childB, 2L)
        graph.exit(parent, 1L)

        assertEquals(2L, graph.acceptedForTest())
        assertTrue(graph.flushBlocking(2_000L))
        assertEquals(2L, graph.aggregatedEdgeKeysForTest())
        assertEquals(2L, graph.emittedForTest() + graph.acceptedEventLossForTest())
    }

    @Test
    fun legacyModePreservesFirstContextEdgeIdentity() = withGraph(
        initialScreen = "screen-a",
        mode = JankHunterRuntimeGraphMode.LEGACY,
    ) { graph, screen ->
        graph.recordEdge(1L, 2L)
        screen.set("screen-b")
        graph.recordEdge(1L, 2L)

        assertTrue(graph.flushBlocking(2_000L))
        assertEquals(1L, graph.aggregatedEdgeKeysForTest())
    }

    @Test
    fun shadowModeProjectsBufferedContextsToEquivalentLegacyEdge() = withGraph(
        initialScreen = "screen-a",
        mode = JankHunterRuntimeGraphMode.SHADOW,
    ) { graph, screen ->
        graph.recordEdge(1L, 2L)
        screen.set("screen-b")
        graph.recordEdge(1L, 2L)

        assertTrue(graph.flushBlocking(2_000L))
        val comparison = graph.shadowComparisonForTest()
        assertEquals(0L, comparison.missingEdges)
        assertEquals(0L, comparison.extraEdges)
        assertEquals(0L, comparison.countDifferences)
        assertEquals(0L, comparison.durationDifferences)
        assertEquals(1L, comparison.contextSplits)
        assertEquals(2L, graph.aggregatedEdgeKeysForTest())
    }

    @Test
    fun producersMakeProgressWithoutWaitingForConsumerOrWriterMonitor() = withGraph { graph ->
        val producerCount = 32
        val eventsPerProducer = 2_000
        val pool = Executors.newFixedThreadPool(producerCount)
        val start = CountDownLatch(1)
        val done = CountDownLatch(producerCount)
        try {
            repeat(producerCount) { producer ->
                pool.execute {
                    start.await()
                    repeat(eventsPerProducer) { event ->
                        val parentId = producer.toLong() shl 32
                        val childId = event.toLong() and 7L
                        val parent = graph.enter(parentId, enabled = true)
                        val child = graph.enter(childId, enabled = true)
                        graph.exit(child, childId)
                        graph.exit(parent, parentId)
                    }
                    done.countDown()
                }
            }
            start.countDown()
            assertTrue("producer progress timed out", done.await(10, TimeUnit.SECONDS))
        } finally {
            pool.shutdownNow()
        }
    }

    @Test
    fun repeatedCallsHaveExactLogicalCount() = withGraph { graph ->
        repeat(10_000) {
            val parent = graph.enter(1L, enabled = true)
            val child = graph.enter(2L, enabled = true)
            graph.exit(child, 2L)
            graph.exit(parent, 1L)
        }
        val accepted = graph.acceptedForTest()
        assertTrue(accepted > 0L)

        assertTrue(graph.flushBlocking(5_000L))

        assertEquals(accepted, graph.emittedForTest() + graph.acceptedEventLossForTest())
    }

    @Test
    fun circuitBreakerStopsCollectionAfterSustainedLoss() = withGraph(maxKeys = 0) { graph, _ ->
        repeat(512) { graph.recordEdge(1L, it.toLong() + 2L) }
        assertTrue(graph.flushBlocking(5_000L))
        assertTrue(graph.circuitBreakerOpenForTest())

        val attemptsBefore = graph.attemptedForTest()
        graph.recordEdge(1L, 999L)

        assertEquals(attemptsBefore + 1L, graph.attemptedForTest())
        assertEquals(1L, graph.circuitBreakerDropsForTest())
    }

    @Test
    fun shutdownTerminatesDedicatedDaemonConsumer() {
        val directory = Files.createTempDirectory("jankhunter-runtime-call-graph").toFile()
        val writer = writer(directory)
        val graph = graph()
        graph.resetFlushState(writer)
        val consumer = graph.consumerForTest()
        assertEquals("JankHunterGraph", consumer?.name)
        assertTrue(consumer?.isDaemon == true)

        graph.flushForShutdown()

        assertFalse(consumer?.isAlive == true)
        writer.close()
        directory.deleteRecursively()
    }

    private fun withGraph(
        initialScreen: String = "screen",
        mode: JankHunterRuntimeGraphMode = JankHunterRuntimeGraphMode.BUFFERED,
        maxKeys: Int = 128,
        block: (RuntimeCallGraph, AtomicText) -> Unit,
    ) {
        val directory = Files.createTempDirectory("jankhunter-runtime-call-graph").toFile()
        val writer = writer(directory)
        val screen = AtomicText(initialScreen)
        val graph = graph(screen, maxKeys)
        graph.resetFlushState(writer, mode)
        try {
            block(graph, screen)
        } finally {
            graph.flushForShutdown()
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    private fun withGraph(block: (RuntimeCallGraph) -> Unit) {
        withGraph { graph, _ -> block(graph) }
    }

    private fun graph(screen: AtomicText = AtomicText("screen"), maxKeys: Int = 128): RuntimeCallGraph {
        val now = AtomicLong(1L)
        return RuntimeCallGraph(
            nowMs = { now.getAndIncrement() },
            captureScreen = screen::get,
            captureFlow = { "flow" },
            captureStep = { "step" },
            maxKeys = { maxKeys },
        )
    }

    private fun writer(directory: java.io.File): AsyncLogWriter {
        return AsyncLogWriter.open(
            directory,
            JankHunterConfig.builder()
                .autoStartCollectors(false)
                .flushIntervalMs(60_000)
                .build(),
            "main",
        )
    }

    private class AtomicText(initial: String) {
        @Volatile
        private var value = initial

        fun get(): String = value

        fun set(updated: String) {
            value = updated
        }
    }
}
