package io.jankhunter.runtime

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
