package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.BinaryLogWriter
import io.jankhunter.runtime.internal.io.MetricAggregator
import io.jankhunter.runtime.internal.io.MetricAggregationMode
import java.io.File
import java.util.Locale
import java.util.concurrent.Executor
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
    fun logSpamAggregationHasNoSteadyStateAllocation() {
        RuntimeBenchmarkHarness.assumeEnabled()
        val accumulator = LogSpamAccumulator(16)
        val result = RuntimeBenchmarkHarness.measure(
            iterations = RuntimeBenchmarkHarness.iterations(100_000, 1_000_000),
            warmupIterations = 50_000,
        ) {
            accumulator.add("screen", "owner", "source", 3, 7L)
            accumulator.logicalEventCount()
        }
        RuntimeBenchmarkHarness.report("log spam aggregation", result)
        RuntimeBenchmarkHarness.assertBudget(
            name = "log spam aggregation",
            result = result,
            latencyBudgetNs = 500.0,
            allocationBudgetBytes = 1.0,
        )
    }

    @Test
    fun methodCounterAggregationHasNoSteadyStateAllocation() {
        RuntimeBenchmarkHarness.assumeEnabled()
        val accumulator = MethodCounterAccumulator(16)
        val result = RuntimeBenchmarkHarness.measure(
            iterations = RuntimeBenchmarkHarness.iterations(100_000, 1_000_000),
            warmupIterations = 50_000,
        ) {
            accumulator.add(7L, "owner.method")
            accumulator.logicalEventCount()
        }
        RuntimeBenchmarkHarness.report("method counter aggregation", result)
        RuntimeBenchmarkHarness.assertBudget(
            name = "method counter aggregation",
            result = result,
            latencyBudgetNs = 300.0,
            allocationBudgetBytes = 1.0,
        )
    }

    @Test
    fun operationApiDisabledHotPath() {
        assumeBenchmarksEnabled()
        val count = iterations(FAST_PATH_MIN_ITERATIONS)
        val elapsedNs = medianElapsedNs {
            var checksum = 0L
            repeat(count) {
                val operation = JankHunterTelemetry.startOperation("benchmark.operation")
                operation.success()
                checksum = checksum xor operation.id
            }
            benchmarkLongSink = checksum
        }
        printBenchmark("operation API disabled", count, elapsedNs)
    }

    @Test
    fun operationStartFinishHotPath() {
        assumeBenchmarksEnabled()
        val count = iterations()
        val acceptedRecords = longArrayOf(0L)
        val sink = object : OperationEventSink {
            override fun operation(
                name: String,
                operationId: Long,
                parentId: Long,
                phase: Long,
                kind: Long,
                outcome: Long,
                durationUs: Long,
                budgetUs: Long,
                screen: String?,
                owner: String?,
                attributes: JankHunterOperationAttributes,
            ): Boolean {
                acceptedRecords[0]++
                return true
            }
        }
        val telemetry = RuntimeOperationTelemetry(ContextTracker(), { sink }, { 1L }, {})
        val elapsedNs = medianElapsedNs {
            var checksum = 0L
            repeat(count) {
                val operation = telemetry.start(
                    "benchmark.operation",
                    JankHunterOperationKind.USER,
                    500L,
                    JankHunterOperationAttributes.EMPTY,
                )
                operation.success()
                checksum = checksum xor operation.id
            }
            benchmarkLongSink = checksum xor acceptedRecords[0]
        }
        printBenchmark("operation start/finish", count, elapsedNs)
    }

    @Test
    fun logSpamCounterHotPath() {
        assumeBenchmarksEnabled()
        val count = iterations(FAST_PATH_MIN_ITERATIONS)
        val elapsedNs = medianElapsedNs {
            var checksum = 0L
            repeat(count) {
                JankHunterTelemetry.recordLog("BenchmarkOwner", "android.util.Log.d", 3)
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
                    JankHunterHooks.wrapRunnable(runnable, "BenchmarkOwner")
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
        val wrapped = JankHunterHooks.wrapRunnable(
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
        val wrapped = JankHunterHooks.wrapCoroutineBlock(block, "BenchmarkOwner") as Function2<Any?, Any?, Any?>
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
    fun executorTaskTrackingHotPath() {
        assumeBenchmarksEnabled()
        val count = iterations()
        val executionState = longArrayOf(1L)
        val executor = JankHunterExecutor(
            delegate = Executor { command -> command.run() },
            name = "benchmark executor",
            ownerName = "BenchmarkOwner",
            callbacks = JankHunter.asyncTelemetry(),
        )
        val command = Runnable {
            executionState[0] = nextBenchmarkState(executionState[0])
        }
        val elapsedNs = medianElapsedNs {
            repeat(count) {
                executor.execute(command)
            }
            benchmarkLongSink = executionState[0]
        }
        printBenchmark("executor task tracking", count, elapsedNs)
    }

    @Test
    fun asmMethodHookGuardHotPathWithoutWriter() {
        assumeBenchmarksEnabled()
        val count = iterations(METHOD_GUARD_MIN_ITERATIONS)
        val elapsedNs = medianElapsedNs {
            var tokenChecksum = 0L
            repeat(count) {
                val parentToken = JankHunterHooks.enterMethod(1L, "benchmark.Parent.call")
                val childToken = JankHunterHooks.enterMethod(2L, "benchmark.Child.call")
                JankHunterHooks.exitMethod(childToken, 2L)
                JankHunterHooks.exitMethod(parentToken, 1L)
                tokenChecksum = tokenChecksum xor parentToken xor childToken
            }
            benchmarkLongSink = tokenChecksum
        }
        printBenchmark("ASM method hook no-writer guard", count * 4, elapsedNs)
    }

    @Test
    fun advancedTelemetryDisabledGuardsHotPath() {
        assumeBenchmarksEnabled()
        val count = iterations(FAST_PATH_MIN_ITERATIONS)
        val elapsedNs = medianElapsedNs {
            var checksum = 0L
            repeat(count) {
                JankHunterTelemetry.recordIO(JankHunterIOOperation.FILE_READ, 1L, -1L, null, JankHunterIOOutcome.SUCCESS)
                checksum = checksum xor JankHunterWorkerRuntime.started(1L, "BenchmarkWorker", 0, 0)
                if (JankHunterWorkerRuntime.isActive()) checksum++
            }
            benchmarkLongSink = checksum
        }
        printBenchmark("advanced telemetry disabled guards", count * 3, elapsedNs)
    }

    @Test
    fun httpHandoffWithoutWriterHotPath() {
        assumeBenchmarksEnabled()
        val count = iterations(METHOD_GUARD_MIN_ITERATIONS)
        val elapsedNs = medianElapsedNs {
            repeat(count) {
                JankHunterNetworkRuntime.recordHttp(BENCHMARK_HTTP_EVENT)
            }
            benchmarkObjectSink = BENCHMARK_HTTP_EVENT
        }
        printBenchmark("HTTP handoff no-writer guard", count, elapsedNs)
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

        val BENCHMARK_HTTP_EVENT = JankHunterHttpEvent(
            JankHunterContextSnapshot("BenchmarkScreen", "BenchmarkOwner"),
            "GET /benchmark",
            null,
            1L,
            0L,
            0L,
            0L,
            0L,
            0L,
            1L,
            0L,
            200,
            JankHunterHttpEvent.FAILURE_PHASE_UNKNOWN,
            JankHunterHttpEvent.FAILURE_KIND_UNKNOWN,
            JankHunterHttpEvent.PROTOCOL_HTTP_2,
            0L,
            0L,
            1,
            0,
            0,
            0,
            0,
            0,
            0,
            0L,
        )

        @Volatile
        var benchmarkObjectSink: Any? = null

        @Volatile
        var benchmarkLongSink: Long = 0L
    }
}
