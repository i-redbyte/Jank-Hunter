package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeGraphAdmissionTest {
    @Test
    fun zeroBudgetRotationRejectsEdgeAndUnwindsStack() = withBlockedConsumer(0L) { graph ->
        val parent = graph.enter(1L, "parent", true)
        val child = graph.enter(2L, "child", true)
        producer(graph).registry.single().buffer.rotationRequested = true
        graph.exit(child, 2L)
        assertEquals(1, graph.currentThreadDepthForTest())
        assertEquals(1L, graph.attemptedForTest())
        assertEquals(0L, graph.acceptedForTest())
        assertEquals(1L, graph.producerCapacityLossForTest())
        producer(graph).registry.single().buffer.rotationRequested = false
        graph.exit(parent, 1L)
        assertEquals(0, graph.currentThreadDepthForTest())
    }

    @Test
    fun interruptedSaturatedGraphPreservesInterruptAndAccountsLoss() = withBlockedConsumer(
        TimeUnit.SECONDS.toNanos(5L),
    ) { graph ->
        val capacity = RUNTIME_GRAPH_PAGE_QUEUE_CAPACITY * RUNTIME_GRAPH_PAGE_MAX_KEYS
        repeat(capacity) { record(graph, it.toLong() + 2L) }
        Thread.currentThread().interrupt()
        try {
            record(graph, capacity.toLong() + 2L)
            assertTrue(Thread.currentThread().isInterrupted)
            assertEquals(capacity.toLong(), graph.acceptedForTest())
            assertEquals(capacity.toLong() + 1L, graph.attemptedForTest())
            assertEquals(1L, graph.producerCapacityLossForTest())
            assertFalse(producer(graph).registry.single().buffer.producerWaiting)
        } finally {
            Thread.interrupted()
        }
    }

    private fun record(graph: RuntimeCallGraph, callee: Long) {
        graph.recordSemantic(1L, "caller", callee, "callee", 1L, true)
    }

    private fun withBlockedConsumer(waitNanos: Long, block: (RuntimeCallGraph) -> Unit) {
        val directory = Files.createTempDirectory("jankhunter-graph-admission").toFile()
        val release = CountDownLatch(1)
        val entered = CountDownLatch(1)
        val writer = AsyncLogWriterFactory().open(
            directory, JankHunterConfig.builder().autoStartCollectors(false).build(), "main",
        )
        val graph = RuntimeCallGraph(
            { 1L }, { null }, { 0L }, { 4096 },
            admissionWaitNanos = { waitNanos },
            consumerLoopObserver = {
                entered.countDown()
                check(release.await(5L, TimeUnit.SECONDS))
            },
        )
        val executor = Executors.newSingleThreadExecutor()
        graph.resetFlushState(writer)
        try {
            assertTrue(entered.await(5L, TimeUnit.SECONDS))
            executor.submit { block(graph) }.get(500L, TimeUnit.MILLISECONDS)
        } finally {
            producer(graph).registry.forEach { it.buffer.rotationRequested = false }
            release.countDown()
            executor.shutdown()
            assertTrue(executor.awaitTermination(5L, TimeUnit.SECONDS))
            graph.flushForShutdown()
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    // Hold the consumer-owned rotation handshake to reproduce descheduling inside rotation.
    private fun producer(graph: RuntimeCallGraph): RuntimeGraphProducer {
        val session = RuntimeCallGraph::class.java.getDeclaredField("primary").apply { isAccessible = true }.get(graph)
        val field = RuntimeCallGraphSession::class.java.getDeclaredField("producer")
        field.isAccessible = true
        return field.get(session) as RuntimeGraphProducer
    }
}
