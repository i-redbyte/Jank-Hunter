package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import java.lang.ref.ReferenceQueue
import java.lang.ref.WeakReference
import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeHookLeakTest {
    @Test
    fun deadProducerThreadsAndBuffersAreReclaimed() {
        val directory = Files.createTempDirectory("jankhunter-thread-churn").toFile()
        val writer = writer(directory)
        val graph = graph()
        graph.resetFlushState(writer)
        val queue = ReferenceQueue<Thread>()
        val references = createProducerThreads(graph, queue, 64)
        assertTrue(graph.flushBlocking(5_000L))

        var dequeued = 0
        val collected = awaitGc(references.size) {
            dequeued += drainQueue(queue)
            dequeued
        }
        assertTrue(
            "only $collected/${references.size} producer Thread objects were collected",
            collected >= references.size - 1,
        )
        repeat(GC_RETRIES) {
            graph.flushBlocking(1_000L)
            if (graph.registeredProducerCountForTest() == 0) return@repeat
            forceGcPressure()
        }
        assertEquals(0, graph.registeredProducerCountForTest())

        val consumer = graph.consumerForTest()
        graph.flushForShutdown()
        assertFalse(consumer?.isAlive == true)
        graph.clear()
        writer.close()
        directory.deleteRecursively()
    }

    @Test
    fun repeatedStartStopDoesNotGrowRegistryOrLeaveConsumers() {
        val directory = Files.createTempDirectory("jankhunter-restart-churn").toFile()
        val writer = writer(directory)
        val graph = graph()
        repeat(32) {
            graph.resetFlushState(writer)
            graph.recordEdge(1L, 2L)
            val consumer = graph.consumerForTest()
            graph.flushForShutdown()
            assertFalse("consumer survived cycle $it", consumer?.isAlive == true)
            graph.clear()
            assertEquals(0, graph.registeredProducerCountForTest())
        }
        writer.close()
        directory.deleteRecursively()
    }

    @Test
    fun resetReleasesContextOwnerAndMethodSymbols() {
        val references = createAndReleaseDynamicSymbols()

        val collected = awaitGc(references.size) {
            references.count { it.get() == null }
        }

        assertEquals("retained dynamic symbols: ${references.map { it.get() }}", references.size, collected)
    }

    private fun createProducerThreads(
        graph: RuntimeCallGraph,
        queue: ReferenceQueue<Thread>,
        count: Int,
    ): List<WeakReference<Thread>> {
        return List(count) { index ->
            val thread = Thread({ graph.recordEdge(index.toLong(), index.toLong() + 1L) })
            val reference = WeakReference(thread, queue)
            thread.start()
            thread.join(2_000L)
            assertFalse(thread.isAlive)
            reference
        }
    }

    private fun createAndReleaseDynamicSymbols(): List<WeakReference<String>> {
        val directory = Files.createTempDirectory("jankhunter-symbol-release").toFile()
        val writer = writer(directory)
        var screen: String? = String("dynamic-screen-${System.nanoTime()}".toCharArray())
        val owner = String("dynamic-owner-${System.nanoTime()}".toCharArray())
        val method = String("dynamic-method-${System.nanoTime()}".toCharArray())
        val source = String("dynamic-source-${System.nanoTime()}".toCharArray())
        val references = listOf(screen, owner, method, source).map { WeakReference(requireNotNull(it)) }
        val graph = RuntimeCallGraph(
            nowMs = { 1L },
            captureScreen = { screen },
            captureFlow = { null },
            captureStep = { null },
            maxKeys = { 16 },
        )
        val events = RuntimeHookEventTransport({ 16 }, { 16 })
        graph.resetFlushState(writer)
        events.start(writer)
        val parent = graph.enter(1L, owner, enabled = true)
        val child = graph.enter(2L, method, enabled = true)
        graph.exit(child, 2L)
        graph.exit(parent, 1L)
        events.recordMethod(2L, method)
        events.recordLogSpam(screen, owner, null, null, source, 3)
        screen = null
        graph.flushForShutdown()
        events.stopAndFlush(5_000L)
        graph.clear()
        events.clear()
        writer.close()
        directory.deleteRecursively()
        return references
    }

    private fun awaitGc(expected: Int, observed: () -> Int): Int {
        var best = observed()
        repeat(GC_RETRIES) {
            if (best >= expected) return best
            forceGcPressure()
            best = maxOf(best, observed())
        }
        return best
    }

    private fun drainQueue(queue: ReferenceQueue<Thread>): Int {
        var count = 0
        while (queue.poll() != null) count++
        return count
    }

    private fun forceGcPressure() {
        gcPressureSink = Array(8) { ByteArray(256 * 1024) }
        System.gc()
        System.runFinalization()
        Thread.yield()
    }

    private fun graph(): RuntimeCallGraph {
        return RuntimeCallGraph(
            nowMs = { System.nanoTime() / 1_000_000L },
            captureScreen = { "screen" },
            captureFlow = { "flow" },
            captureStep = { "step" },
            maxKeys = { 128 },
        )
    }

    private fun writer(directory: java.io.File): AsyncLogWriter {
        return AsyncLogWriter.open(
            directory,
            JankHunterConfig.builder().autoStartCollectors(false).flushIntervalMs(60_000).build(),
            "main",
        )
    }

    private companion object {
        const val GC_RETRIES = 40

        @Volatile
        var gcPressureSink: Any? = null
    }
}
