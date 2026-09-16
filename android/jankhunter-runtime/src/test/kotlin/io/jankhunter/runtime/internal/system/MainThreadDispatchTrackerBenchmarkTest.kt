package io.jankhunter.runtime.internal.system

import io.jankhunter.runtime.RuntimeBenchmarkHarness
import org.junit.Test

class MainThreadDispatchTrackerBenchmarkTest {
    @Test
    fun shortDispatchDoesNotAllocateAtSteadyState() {
        RuntimeBenchmarkHarness.assumeEnabled()
        val iterations = RuntimeBenchmarkHarness.iterations(MIN_ITERATIONS, DEFAULT_ITERATIONS)
        var nowMs = 0L
        val tracker = MainThreadDispatchTracker({ nowMs }, minDurationMs = 10L)

        val result = RuntimeBenchmarkHarness.measure(iterations, WARMUP_ITERATIONS) {
            tracker.onMessage(START)
            nowMs++
            tracker.onMessage(END)
            1L
        }

        RuntimeBenchmarkHarness.report("main-thread short dispatch", result)
        RuntimeBenchmarkHarness.assertBudget(
            "main-thread short dispatch",
            result,
            latencyBudgetNs = LATENCY_BUDGET_NS,
            allocationBudgetBytes = ALLOCATION_BUDGET_BYTES,
        )
    }

    private companion object {
        const val MIN_ITERATIONS = 100_000
        const val DEFAULT_ITERATIONS = 500_000
        const val WARMUP_ITERATIONS = 20_000
        const val LATENCY_BUDGET_NS = 1_000.0
        const val ALLOCATION_BUDGET_BYTES = 0.5
        const val START = ">>>>> Dispatching to Handler (x) {abc} callback"
        const val END = "<<<<< Finished to Handler (x) {abc} callback"
    }
}
