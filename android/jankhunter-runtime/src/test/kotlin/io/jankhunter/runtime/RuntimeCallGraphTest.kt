package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLong
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeCallGraphTest {
    @Test
    fun nonLifoExitDoesNotRecordFalseEdge() {
        val now = AtomicLong(1L)
        val graph = RuntimeCallGraph(
            nowMs = { now.getAndIncrement() },
            captureContext = { ownerOverride ->
                JankHunterContext(
                    screen = "screen",
                    owner = ownerOverride,
                    flow = "flow",
                    step = "step",
                )
            },
            maxKeys = { 8 },
        )
        val directory = Files.createTempDirectory("jankhunter-runtime-call-graph").toFile()
        val writer = AsyncLogWriter.open(
            directory,
            JankHunterConfig.builder()
                .autoStartCollectors(false)
                .logCompressionEnabled(false)
                .build(),
            "main",
        )
        try {
            val parent = graph.enter("parent", enabled = true)
            graph.enter("child", enabled = true)

            graph.exit(parent, "parent", writer)

            assertTrue("non-LIFO exit recorded an edge", counterSize(graph) == 0)
        } finally {
            writer.close()
            directory.deleteRecursively()
        }
    }

    @Test
    fun maxKeysIsPreservedUnderConcurrentUniqueEdges() {
        val now = AtomicLong(1L)
        val graph = RuntimeCallGraph(
            nowMs = { now.getAndIncrement() },
            captureContext = { ownerOverride ->
                JankHunterContext(
                    screen = "screen",
                    owner = ownerOverride,
                    flow = "flow",
                    step = "step",
                )
            },
            maxKeys = { 4 },
        )
        val directory = Files.createTempDirectory("jankhunter-runtime-call-graph").toFile()
        val writer = AsyncLogWriter.open(
            directory,
            JankHunterConfig.builder()
                .autoStartCollectors(false)
                .flushIntervalMs(60_000)
                .logCompressionEnabled(false)
                .build(),
            "main",
        )
        val pool = Executors.newFixedThreadPool(8)
        val start = CountDownLatch(1)
        val done = CountDownLatch(64)
        try {
            repeat(64) { index ->
                pool.execute {
                    start.await()
                    try {
                        val parent = graph.enter("caller", enabled = true)
                        val child = graph.enter("callee-$index", enabled = true)
                        graph.exit(child, "callee-$index", writer)
                        graph.exit(parent, "caller", writer)
                    } finally {
                        done.countDown()
                    }
                }
            }

            start.countDown()
            assertTrue(done.await(2, TimeUnit.SECONDS))
            assertTrue("counter size exceeded cap: ${counterSize(graph)}", counterSize(graph) <= 4)
        } finally {
            pool.shutdownNow()
            writer.close()
            directory.deleteRecursively()
        }
    }

    @Suppress("UNCHECKED_CAST")
    private fun counterSize(graph: RuntimeCallGraph): Int {
        val field = RuntimeCallGraph::class.java.getDeclaredField("counters").apply {
            isAccessible = true
        }
        return (field.get(graph) as Map<Any, Any>).size
    }
}
