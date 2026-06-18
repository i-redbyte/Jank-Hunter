package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.BinaryLogWriter
import io.jankhunter.runtime.internal.io.MetricAggregator
import io.jankhunter.runtime.internal.io.MetricAggregationMode
import java.io.File
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
        val elapsedNs = measureNanoTime {
            repeat(count) {
                val token = JankHunter.startFlow("benchmark.open")
                JankHunter.markFlowStep("render_list")
                JankHunter.endFlow(token)
            }
        }
        printBenchmark("flow start/step/end", count, elapsedNs)
    }

    @Test
    fun logSpamCounterHotPath() {
        assumeBenchmarksEnabled()
        val count = iterations()
        val elapsedNs = measureNanoTime {
            repeat(count) {
                JankHunter.recordLogSpam("BenchmarkOwner", "android.util.Log.d", 3)
            }
        }
        printBenchmark("log spam counter", count, elapsedNs)
    }

    @Test
    fun wrapperCreationHotPath() {
        assumeBenchmarksEnabled()
        val count = iterations()
        val runnable = Runnable { }
        val elapsedNs = measureNanoTime {
            repeat(count) {
                JankHunter.wrapRunnable(runnable, "BenchmarkOwner")
            }
        }
        printBenchmark("runnable wrapper creation", count, elapsedNs)
    }

    @Test
    fun wrapperExecutionHotPath() {
        assumeBenchmarksEnabled()
        val count = iterations()
        val wrapped = JankHunter.wrapRunnable(Runnable { }, "BenchmarkOwner")!!
        val elapsedNs = measureNanoTime {
            repeat(count) {
                wrapped.run()
            }
        }
        printBenchmark("runnable wrapper execution", count, elapsedNs)
    }

    @Test
    fun coroutinePropagationHotPath() {
        assumeBenchmarksEnabled()
        val count = iterations()
        val block: Function2<Any?, Any?, Any?> = { _: Any?, _: Any? -> Unit }
        @Suppress("UNCHECKED_CAST")
        val wrapped = JankHunter.wrapCoroutineBlock(block, "BenchmarkOwner") as Function2<Any?, Any?, Any?>
        val continuation = object : Continuation<Any?> {
            override val context: CoroutineContext = EmptyCoroutineContext

            override fun resumeWith(result: Result<Any?>) = Unit
        }
        val elapsedNs = measureNanoTime {
            repeat(count) {
                wrapped.invoke(Unit, continuation)
            }
        }
        printBenchmark("coroutine propagation wrapper", count, elapsedNs)
    }

    @Test
    fun asmMethodHookGuardHotPathWithoutWriter() {
        assumeBenchmarksEnabled()
        val count = iterations()
        val elapsedNs = measureNanoTime {
            repeat(count) {
                val parentToken = JankHunter.enterMethod("BenchmarkOwner.parent#1")
                val childToken = JankHunter.enterMethod("BenchmarkOwner.child#2")
                JankHunter.exitMethod(childToken, "BenchmarkOwner.child#2")
                JankHunter.exitMethod(parentToken, "BenchmarkOwner.parent#1")
            }
        }
        printBenchmark("ASM method hook no-writer guard", count * 4, elapsedNs)
    }

    @Test
    fun metricAggregationHotPath() {
        assumeBenchmarksEnabled()
        val count = iterations()
        val aggregator = MetricAggregator(maxKeys = 64)
        val elapsedNs = measureNanoTime {
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
        val file = File.createTempFile("jankhunter-benchmark-", ".jhlog")
        val elapsedNs = try {
            measureNanoTime {
                BinaryLogWriter(file, compressionEnabled = false).use { writer ->
                    repeat(count) {
                        writer.counter("benchmark.counter", 1)
                        writer.gauge("benchmark.gauge", it.toLong())
                    }
                    writer.flush()
                }
            }
        } finally {
            file.delete()
        }
        printBenchmark("binary log writer counter/gauge", count * 2, elapsedNs)
    }

    private fun assumeBenchmarksEnabled() {
        assumeTrue(
            "Benchmarks are opt-in. Run with -Djankhunter.benchmark=true",
            System.getProperty("jankhunter.benchmark") == "true",
        )
    }

    private fun iterations(): Int {
        return System.getProperty("jankhunter.benchmark.iterations")
            ?.toIntOrNull()
            ?.coerceAtLeast(1)
            ?: 100_000
    }

    private fun printBenchmark(name: String, count: Int, elapsedNs: Long) {
        val perOpNs = elapsedNs.toDouble() / count.toDouble()
        println("JankHunter benchmark: $name, iterations=$count, total_ns=$elapsedNs, ns_per_op=${"%.1f".format(perOpNs)}")
    }
}
