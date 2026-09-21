package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.LogQualityCounters
import io.jankhunter.runtime.internal.io.RuntimeHookFailureQualitySynchronizer
import org.junit.Test

class RuntimeHookFailureBenchmarkTest {
    @Test
    fun unchangedFailureCountersSynchronizeWithoutAllocation() {
        RuntimeBenchmarkHarness.assumeEnabled()
        val synchronizer = RuntimeHookFailureQualitySynchronizer(LogQualityCounters())
        val measurement = RuntimeBenchmarkHarness.measure(
            iterations = RuntimeBenchmarkHarness.iterations(MIN_ITERATIONS, DEFAULT_ITERATIONS),
            warmupIterations = WARMUP_ITERATIONS,
        ) {
            synchronizer.sync()
            0L
        }

        RuntimeBenchmarkHarness.report("runtime hook failure sync", measurement)
        RuntimeBenchmarkHarness.assertBudget(
            "runtime hook failure sync",
            measurement,
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
