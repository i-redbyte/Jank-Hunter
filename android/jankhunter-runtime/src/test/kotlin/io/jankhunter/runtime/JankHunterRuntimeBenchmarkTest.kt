package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.BinaryLogWriter
import io.jankhunter.runtime.internal.io.MetricAggregator
import io.jankhunter.runtime.internal.io.MetricAggregationMode
import java.io.File
import java.util.Locale
import kotlin.coroutines.Continuation
import kotlin.coroutines.CoroutineContext
import kotlin.coroutines.EmptyCoroutineContext
import kotlin.system.measureNanoTime
import org.junit.After
import org.junit.Assume.assumeTrue
import org.junit.Test

class JankHunterRuntimeBenchmarkTest {
    @After
    fun tearDown() {
        JankHunter.shutdown()
    }

    @Test
    fun flowApiContextHotPath() {
        assumeBenchmarksEnabled()
        val count = iterations()
        val elapsedNs = medianElapsedNs {
            var lastToken: JankHunterFlow? = null
            repeat(count) {
                val token = JankHunter.startFlow("benchmark.open")
                JankHunter.markFlowStep("render_list")
                JankHunter.endFlow(token)
                lastToken = token
            }
            benchmarkObjectSink = lastToken
        }
        printBenchmark("flow start/step/end", count, elapsedNs)
    }

    @Test
    fun logSpamCounterHotPath() {
        assumeBenchmarksEnabled()
        val count = iterations(FAST_PATH_MIN_ITERATIONS)
        val elapsedNs = medianElapsedNs {
            var checksum = 0L
            repeat(count) {
                JankHunter.recordLogSpam("BenchmarkOwner", "android.util.Log.d", 3)
                checksum += it.toLong()
            }
            benchmarkLongSink = checksum
        }
        printBenchmark("log spam counter", count, elapsedNs)
    }

    @Test
    fun wrapperCreationHotPath() {
        assumeBenchmarksEnabled()
        val count = iterations(FAST_PATH_MIN_ITERATIONS)
        val runnable = Runnable { }
        val wrappedResults = arrayOfNulls<Runnable>(BENCHMARK_BLACKHOLE_SIZE)
        val elapsedNs = medianElapsedNs {
            repeat(count) {
                wrappedResults[it and BENCHMARK_BLACKHOLE_MASK] =
                    JankHunter.wrapRunnable(runnable, "BenchmarkOwner")
            }
            benchmarkObjectSink = wrappedResults
        }
        printBenchmark("runnable wrapper creation", count, elapsedNs)
    }

    @Test
    fun wrapperExecutionHotPath() {
        assumeBenchmarksEnabled()
        val count = iterations(FAST_PATH_MIN_ITERATIONS)
        val executionState = longArrayOf(1L)
        val wrapped = JankHunter.wrapRunnable(
            Runnable {
                executionState[0] = nextBenchmarkState(executionState[0])
            },
            "BenchmarkOwner",
        )!!
        val elapsedNs = medianElapsedNs {
            repeat(count) {
                wrapped.run()
            }
            benchmarkLongSink = executionState[0]
        }
        printBenchmark("runnable wrapper execution", count, elapsedNs)
    }

    @Test
    fun coroutinePropagationHotPath() {
        assumeBenchmarksEnabled()
        val count = iterations(FAST_PATH_MIN_ITERATIONS)
        val coroutineState = longArrayOf(1L)
        val block: Function2<Any?, Any?, Any?> = { _: Any?, _: Any? ->
            coroutineState[0] = nextBenchmarkState(coroutineState[0])
            Unit
        }
        @Suppress("UNCHECKED_CAST")
        val wrapped = JankHunter.wrapCoroutineBlock(block, "BenchmarkOwner") as Function2<Any?, Any?, Any?>
        val wrappedResults = arrayOfNulls<Any>(BENCHMARK_BLACKHOLE_SIZE)
        val continuation = object : Continuation<Any?> {
            override val context: CoroutineContext = EmptyCoroutineContext

            override fun resumeWith(result: Result<Any?>) = Unit
        }
        val elapsedNs = medianElapsedNs {
            repeat(count) {
                wrappedResults[it and BENCHMARK_BLACKHOLE_MASK] = wrapped.invoke(Unit, continuation)
            }
            benchmarkObjectSink = wrappedResults
            benchmarkLongSink = coroutineState[0]
        }
        printBenchmark("coroutine propagation wrapper", count, elapsedNs)
    }

    @Test
    fun asmMethodHookGuardHotPathWithoutWriter() {
        assumeBenchmarksEnabled()
        val count = iterations(METHOD_GUARD_MIN_ITERATIONS)
        val elapsedNs = medianElapsedNs {
            var tokenChecksum = 0L
            repeat(count) {
                val parentToken = JankHunter.enterMethod(1L)
                val childToken = JankHunter.enterMethod(2L)
                JankHunter.exitMethod(childToken, 2L)
                JankHunter.exitMethod(parentToken, 1L)
                tokenChecksum = tokenChecksum xor parentToken xor childToken
            }
            benchmarkLongSink = tokenChecksum
        }
        printBenchmark("ASM method hook no-writer guard", count * 4, elapsedNs)
    }

    @Test
    fun metricAggregationHotPath() {
        assumeBenchmarksEnabled()
        val count = iterations(METRIC_MIN_ITERATIONS)
        val elapsedNs = medianElapsedNs(
            setup = { MetricAggregator(maxKeys = 64) },
            cleanup = {},
        ) { aggregator ->
            repeat(count) {
                aggregator.counter("benchmark.counter", 1)
                aggregator.gauge("benchmark.gauge", it.toLong())
            }
            aggregator.flush(object : MetricAggregator.Sink {
                override fun counter(name: String, value: Long) = Unit

                override fun gauge(
                    name: String,
                    value: Long,
                    count: Long,
                    sum: Long,
                    max: Long,
                    mode: MetricAggregationMode,
                ) = Unit
            })
        }
        printBenchmark("metric aggregation counter/gauge", count * 2, elapsedNs)
    }

    @Test
    fun binaryLogWriterHotPath() {
        assumeBenchmarksEnabled()
        val count = iterations()
        val elapsedNs = medianElapsedNs(
            setup = { File.createTempFile("jankhunter-benchmark-", ".jhlog") },
            cleanup = { file ->
                file.delete()
                Unit
            },
        ) { file ->
            BinaryLogWriter(file).use { writer ->
                repeat(count) {
                    writer.counter("benchmark.counter", 1)
                    writer.gauge("benchmark.gauge", it.toLong())
                }
                writer.flush()
            }
        }
        printBenchmark("binary log writer counter/gauge", count * 2, elapsedNs)
    }

    private inline fun medianElapsedNs(crossinline workload: () -> Unit): Long {
        return medianElapsedNs(
            setup = { Unit },
            cleanup = {},
            workload = { workload() },
        )
    }

    private inline fun <T> medianElapsedNs(
        crossinline setup: () -> T,
        crossinline cleanup: (T) -> Unit,
        crossinline workload: (T) -> Unit,
    ): Long {
        repeat(BENCHMARK_WARMUP_SAMPLE_COUNT) {
            val warmupState = setup()
            try {
                workload(warmupState)
            } finally {
                cleanup(warmupState)
            }
        }

        val samples = LongArray(BENCHMARK_SAMPLE_COUNT) {
            val sampleState = setup()
            try {
                measureNanoTime { workload(sampleState) }
            } finally {
                cleanup(sampleState)
            }
        }
        samples.sort()
        return samples[samples.size / 2]
    }

    private fun assumeBenchmarksEnabled() {
        assumeTrue(
            "Benchmarks are opt-in. Run with -Djankhunter.benchmark=true",
            System.getProperty("jankhunter.benchmark") == "true",
        )
    }

    private fun iterations(minimum: Int = 1): Int {
        return System.getProperty("jankhunter.benchmark.iterations")
            ?.toIntOrNull()
            ?.coerceAtLeast(minimum)
            ?: maxOf(DEFAULT_BENCHMARK_ITERATIONS, minimum)
    }

    private fun printBenchmark(name: String, count: Int, elapsedNs: Long) {
        val perOpNs = elapsedNs.toDouble() / count.toDouble()
        val formattedPerOp = String.format(Locale.US, "%.1f", perOpNs)
        println("JankHunter benchmark: $name, iterations=$count, total_ns=$elapsedNs, ns_per_op=$formattedPerOp")
    }

    private fun nextBenchmarkState(current: Long): Long {
        return current * BENCHMARK_STATE_MULTIPLIER + BENCHMARK_STATE_INCREMENT
    }

    private companion object {
        const val BENCHMARK_WARMUP_SAMPLE_COUNT = 3
        const val BENCHMARK_SAMPLE_COUNT = 7
        const val DEFAULT_BENCHMARK_ITERATIONS = 100_000
        const val FAST_PATH_MIN_ITERATIONS = 2_000_000
        const val METHOD_GUARD_MIN_ITERATIONS = 1_000_000
        const val METRIC_MIN_ITERATIONS = 500_000
        const val BENCHMARK_BLACKHOLE_SIZE = 64
        const val BENCHMARK_BLACKHOLE_MASK = BENCHMARK_BLACKHOLE_SIZE - 1
        const val BENCHMARK_STATE_MULTIPLIER = 6_364_136_223_846_793_005L
        const val BENCHMARK_STATE_INCREMENT = 1_442_695_040_888_963_407L

        @Volatile
        var benchmarkObjectSink: Any? = null

        @Volatile
        var benchmarkLongSink: Long = 0L
    }
}
