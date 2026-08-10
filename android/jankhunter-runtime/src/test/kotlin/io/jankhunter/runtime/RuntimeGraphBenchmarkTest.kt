package io.jankhunter.runtime

import com.sun.management.ThreadMXBean
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import java.lang.management.ManagementFactory
import java.nio.file.Files
import java.util.Locale
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLongArray
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test

class RuntimeGraphBenchmarkTest {
    @Test
    fun activeGraphLatencyAndAllocationMatrix() {
        assumeTrue(System.getProperty("jankhunter.benchmark") == "true")
        assumeTrue(allocationBean?.isThreadAllocatedMemorySupported == true)
        val iterations = System.getProperty("jankhunter.benchmark.iterations")
            ?.toIntOrNull()?.coerceAtLeast(10_000) ?: 100_000

        withGraph { graph ->
            report("enter", measure(iterations) {
                val token = graph.enter(1L, enabled = true)
                graph.exit(token, 1L)
            })
            report("exit_publish", measure(iterations) {
                graph.recordEdge(1L, 2L)
            })
        }
        withGraph(consumerDelayNanos = 100_000_000L) { graph ->
            repeat(2_000) { graph.recordEdge(1L, 2L) }
            report("buffer_full_or_breaker", measure(iterations) {
                graph.recordEdge(1L, 2L)
            })
        }
        withMutableContextGraph { graph, context ->
            report("context_change", measure(iterations) { index ->
                context.set(index)
                graph.recordEdge(1L, 2L)
            })
        }
        listOf(1, 8, 32).forEach { producers ->
            withGraph { graph ->
                report("producers_$producers", measureConcurrent(graph, producers, iterations))
            }
        }
        benchmarkLifecycle(iterations.coerceAtMost(100))
    }

    private fun benchmarkLifecycle(iterations: Int) {
        val directory = Files.createTempDirectory("jankhunter-graph-lifecycle-benchmark").toFile()
        val writer = writer(directory)
        val samples = LongArray(iterations)
        repeat(iterations) { index ->
            val graph = graph()
            val started = System.nanoTime()
            graph.resetFlushState(writer)
            graph.flushForShutdown()
            samples[index] = System.nanoTime() - started
            graph.clear()
        }
        writer.close()
        directory.deleteRecursively()
        report("startup_shutdown", BenchmarkResult(samples, Double.NaN))
    }

    private inline fun measure(iterations: Int, crossinline operation: (Int) -> Unit): BenchmarkResult {
        repeat(WARMUP_ITERATIONS) { operation(it) }
        val samples = LongArray(iterations)
        val allocationReadOverhead = allocationReadOverhead(Thread.currentThread().id)
        val allocatedBefore = allocatedBytes(Thread.currentThread().id)
        repeat(iterations) { index ->
            val started = System.nanoTime()
            operation(index)
            samples[index] = System.nanoTime() - started
        }
        val allocatedAfter = allocatedBytes(Thread.currentThread().id)
        return BenchmarkResult(
            samples,
            allocationPerOp(allocatedBefore, allocatedAfter, allocationReadOverhead, iterations.toLong()),
        )
    }

    private fun measureConcurrent(
        graph: RuntimeCallGraph,
        producers: Int,
        totalIterations: Int,
    ): BenchmarkResult {
        val perProducer = (totalIterations / producers).coerceAtLeast(1)
        val allSamples = Array(producers) { LongArray(perProducer) }
        val allocations = AtomicLongArray(producers)
        val start = CountDownLatch(1)
        val done = CountDownLatch(producers)
        val threads = List(producers) { producer ->
            Thread({
                repeat(WARMUP_ITERATIONS / producers + 1) { graph.recordEdge(producer.toLong(), 2L) }
                start.await()
                val readOverhead = allocationReadOverhead(Thread.currentThread().id)
                val before = allocatedBytes(Thread.currentThread().id)
                repeat(perProducer) { index ->
                    val started = System.nanoTime()
                    graph.recordEdge(producer.toLong(), (index and 31).toLong())
                    allSamples[producer][index] = System.nanoTime() - started
                }
                allocations.set(producer, allocatedBytes(Thread.currentThread().id) - before - readOverhead)
                done.countDown()
            }, "JankHunterBenchmark-$producer")
        }
        threads.forEach(Thread::start)
        start.countDown()
        assertTrue(done.await(30, TimeUnit.SECONDS))
        threads.forEach { it.join(1_000L) }
        val samples = LongArray(perProducer * producers)
        var offset = 0
        allSamples.forEach { source ->
            source.copyInto(samples, offset)
            offset += source.size
        }
        var allocated = 0L
        repeat(producers) { allocated += allocations.get(it).coerceAtLeast(0L) }
        return BenchmarkResult(samples, allocated.toDouble() / samples.size.toDouble())
    }

    private fun report(name: String, result: BenchmarkResult) {
        result.samples.sort()
        val p50 = percentile(result.samples, 0.50)
        val p95 = percentile(result.samples, 0.95)
        val p99 = percentile(result.samples, 0.99)
        val p999 = percentile(result.samples, 0.999)
        println(
            "JankHunter active graph benchmark: name=$name ops=${result.samples.size} " +
                "p50_ns=$p50 p95_ns=$p95 p99_ns=$p99 p999_ns=$p999 " +
                "alloc_bytes_per_op=${String.format(Locale.US, "%.3f", result.allocationBytesPerOp)}",
        )
        if (name != "startup_shutdown") {
            assertTrue("$name p99.9 budget exceeded: $p999 ns", p999 <= HOT_PATH_P999_BUDGET_NS)
            if (!result.allocationBytesPerOp.isNaN()) {
                assertTrue(
                    "$name allocation budget exceeded: ${result.allocationBytesPerOp} bytes/op",
                    result.allocationBytesPerOp <= HOT_PATH_ALLOCATION_BUDGET_BYTES,
                )
            }
        }
    }

    private fun percentile(sorted: LongArray, quantile: Double): Long {
        val index = kotlin.math.ceil((sorted.size - 1) * quantile).toInt()
        return sorted[index.coerceIn(sorted.indices)]
    }

    private fun allocatedBytes(threadId: Long): Long {
        val bean = allocationBean ?: return -1L
        if (!bean.isThreadAllocatedMemorySupported) return -1L
        if (!bean.isThreadAllocatedMemoryEnabled) bean.isThreadAllocatedMemoryEnabled = true
        return bean.getThreadAllocatedBytes(threadId)
    }

    private fun allocationReadOverhead(threadId: Long): Long {
        val before = allocatedBytes(threadId)
        val after = allocatedBytes(threadId)
        return if (before < 0L || after < before) 0L else after - before
    }

    private fun allocationPerOp(before: Long, after: Long, overhead: Long, operations: Long): Double {
        if (before < 0L || after < before) return Double.NaN
        return (after - before - overhead).coerceAtLeast(0L).toDouble() / operations.toDouble()
    }

    private fun withGraph(
        consumerDelayNanos: Long = 0L,
        block: (RuntimeCallGraph) -> Unit,
    ) {
        val directory = Files.createTempDirectory("jankhunter-active-graph-benchmark").toFile()
        val writer = writer(directory)
        val graph = graph(consumerDelayNanos)
        graph.resetFlushState(writer)
        try {
            block(graph)
        } finally {
            graph.flushForShutdown()
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    private fun withMutableContextGraph(
        block: (RuntimeCallGraph, MutableContext) -> Unit,
    ) {
        val context = MutableContext()
        val directory = Files.createTempDirectory("jankhunter-context-graph-benchmark").toFile()
        val writer = writer(directory)
        val graph = RuntimeCallGraph(
            nowMs = { System.nanoTime() / 1_000_000L },
            captureScreen = { context.screen },
            captureFlow = { context.flow },
            captureStep = { context.step },
            maxKeys = { 4_096 },
        )
        graph.resetFlushState(writer)
        try {
            block(graph, context)
        } finally {
            graph.flushForShutdown()
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    private fun graph(consumerDelayNanos: Long = 0L): RuntimeCallGraph {
        return RuntimeCallGraph(
            nowMs = { System.nanoTime() / 1_000_000L },
            captureScreen = { "screen" },
            captureFlow = { "flow" },
            captureStep = { "step" },
            maxKeys = { 4_096 },
            consumerDelayNanos = consumerDelayNanos,
        )
    }

    private fun writer(directory: java.io.File): AsyncLogWriter {
        return AsyncLogWriter.open(
            directory,
            JankHunterConfig.builder().autoStartCollectors(false).flushIntervalMs(60_000).build(),
            "main",
        )
    }

    private class MutableContext {
        @Volatile var screen = "screen-0"
        @Volatile var flow = "flow-0"
        @Volatile var step = "step-0"

        fun set(index: Int) {
            screen = if (index and 1 == 0) "screen-0" else "screen-1"
            flow = if (index and 2 == 0) "flow-0" else "flow-1"
            step = if (index and 4 == 0) "step-0" else "step-1"
        }
    }

    private data class BenchmarkResult(val samples: LongArray, val allocationBytesPerOp: Double)

    private companion object {
        const val WARMUP_ITERATIONS = 10_000
        const val HOT_PATH_P999_BUDGET_NS = 1_000_000L
        const val HOT_PATH_ALLOCATION_BUDGET_BYTES = 8.0
        val allocationBean = ManagementFactory.getThreadMXBean() as? ThreadMXBean
    }
}
