package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.RuntimeCallBatch
import java.nio.file.Files
import java.util.Random
import java.util.concurrent.atomic.AtomicLong
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeCallGraphModelTest {
    @Test
    fun bufferedGraphMatchesSeededSingleThreadModel() {
        SEEDS.forEach(::verifySeed)
    }

    private fun verifySeed(seed: Long) {
        val directory = Files.createTempDirectory("jankhunter-graph-model").toFile()
        val writer = AsyncLogWriterFactory().open(
            directory,
            JankHunterConfig.builder().autoStartCollectors(false).flushIntervalMs(60_000).build(),
            "main",
        )
        val clock = AtomicLong()
        var screen = "screen-0"
        var operationId = 1L
        val actual = linkedMapOf<EdgeKey, Aggregate>()
        val graph = RuntimeCallGraph(
            nowMs = clock::get,
            captureScreen = { screen },
            captureOperationId = { operationId },
            maxKeys = { 4_096 },
            batchObserver = { batch -> aggregateBatch(actual, batch) },
        )
        graph.resetFlushState(writer)
        val expected = linkedMapOf<EdgeKey, Aggregate>()
        val stack = mutableListOf<Frame>()
        val random = Random(seed)
        try {
            repeat(5_000) { operation ->
                when {
                    stack.isEmpty() || (stack.size < 12 && random.nextInt(100) < 58) -> {
                        clock.addAndGet(1L + random.nextInt(3))
                        val id = random.nextInt(16).toLong()
                        val token = graph.enter(id, "method-$id", enabled = true)
                        stack += Frame(id, token, clock.get(), screen, operationId)
                    }
                    random.nextInt(100) < 82 -> {
                        clock.addAndGet(random.nextInt(8).toLong())
                        exitTop(graph, stack, expected, clock.get())
                    }
                    else -> {
                        val generation = random.nextInt(5)
                        screen = "screen-$generation"
                        operationId = random.nextInt(12).toLong() + 1L
                    }
                }
                if (operation % 100 == 99) {
                    assertTrue("periodic flush failed, seed=$seed", graph.flushBlocking(5_000L))
                }
            }
            while (stack.isNotEmpty()) {
                clock.incrementAndGet()
                exitTop(graph, stack, expected, clock.get())
            }
            assertTrue("flush failed, seed=$seed", graph.flushBlocking(5_000L))
            assertEquals("edge model mismatch, seed=$seed", expected, actual)
        } finally {
            graph.flushForShutdown()
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    private fun exitTop(
        graph: RuntimeCallGraph,
        stack: MutableList<Frame>,
        expected: MutableMap<EdgeKey, Aggregate>,
        now: Long,
    ) {
        val frame = stack.removeAt(stack.lastIndex)
        graph.exit(frame.token, frame.id)
        val parent = stack.lastOrNull() ?: return
        expected.getOrPut(
            EdgeKey(frame.screen, parent.id, frame.id, frame.operationId),
            ::Aggregate,
        ).add((now - frame.startedAt).coerceAtLeast(0L))
    }

    private fun aggregateBatch(target: MutableMap<EdgeKey, Aggregate>, batch: RuntimeCallBatch) {
        repeat(batch.size) { index ->
            val aggregate = target.getOrPut(
                EdgeKey(
                    batch.screen(index), batch.callerId(index), batch.calleeId(index),
                    batch.operationId(index),
                ),
                ::Aggregate,
            )
            aggregate.count += batch.count(index)
            aggregate.totalMs += batch.totalMs(index)
            aggregate.maxMs = maxOf(aggregate.maxMs, batch.maxMs(index))
        }
    }

    private data class Frame(
        val id: Long,
        val token: Long,
        val startedAt: Long,
        val screen: String,
        val operationId: Long,
    )

    private data class EdgeKey(
        val screen: String?,
        val caller: Long,
        val callee: Long,
        val operationId: Long,
    )

    private data class Aggregate(var count: Long = 0L, var totalMs: Long = 0L, var maxMs: Long = 0L) {
        fun add(durationMs: Long) {
            count++
            totalMs += durationMs
            maxMs = maxOf(maxMs, durationMs)
        }
    }

    private companion object {
        val SEEDS = listOf(0x32524L, 0xC0FFEE, 0x5EEDL)
    }
}
