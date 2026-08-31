package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.MetricAggregator
import org.junit.Test

class RuntimeMetricBenchmarkTest {
    @Test
    fun existingMetricUpdatesHaveNoSteadyStateAllocation() {
        RuntimeBenchmarkHarness.assumeEnabled()
        val iterations = RuntimeBenchmarkHarness.iterations(MIN_ITERATIONS, DEFAULT_ITERATIONS)
        val aggregator = MetricAggregator(maxKeys = 8, exactAdmission = true)
        aggregator.counter(COUNTER, 1L)
        aggregator.gauge(GAUGE, 1L)
        var value = 1L

        val result = RuntimeBenchmarkHarness.measure(iterations, WARMUP_ITERATIONS) {
            value++
            aggregator.counter(COUNTER, 1L)
            aggregator.gauge(GAUGE, value)
            value
        }

        RuntimeBenchmarkHarness.report("existing metric aggregation", result)
        RuntimeBenchmarkHarness.assertBudget(
            "existing metric aggregation",
            result,
            latencyBudgetNs = LATENCY_BUDGET_NS,
            allocationBudgetBytes = ALLOCATION_BUDGET_BYTES,
        )
    }

    private companion object {
        const val COUNTER = "benchmark.counter"
        const val GAUGE = "benchmark.gauge"
        const val MIN_ITERATIONS = 100_000
        const val DEFAULT_ITERATIONS = 500_000
        const val WARMUP_ITERATIONS = 20_000
        const val LATENCY_BUDGET_NS = 2_000.0
        const val ALLOCATION_BUDGET_BYTES = 0.5
    }
}
