package io.jankhunter.runtime

import org.junit.Test

class RuntimeContextBenchmarkTest {
    @Test
    fun unchangedContextCaptureDoesNotAllocateAtSteadyState() {
        RuntimeBenchmarkHarness.assumeEnabled()
        val iterations = RuntimeBenchmarkHarness.iterations(MIN_ITERATIONS, DEFAULT_ITERATIONS)
        val tracker = ContextTracker("Home")
        val expected = tracker.capture(ownerOverride = "Feed")

        val result = RuntimeBenchmarkHarness.measure(iterations, WARMUP_ITERATIONS) {
            check(tracker.capture(ownerOverride = "Feed") === expected)
            1L
        }

        RuntimeBenchmarkHarness.report("unchanged context capture", result)
        RuntimeBenchmarkHarness.assertBudget(
            "unchanged context capture",
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
    }
}
