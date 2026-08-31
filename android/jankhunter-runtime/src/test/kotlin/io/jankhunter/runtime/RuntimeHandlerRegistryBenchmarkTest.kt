package io.jankhunter.runtime

import org.junit.Test

class RuntimeHandlerRegistryBenchmarkTest {
    @Test
    fun missingRemovalHasNoSteadyStateAllocation() {
        RuntimeBenchmarkHarness.assumeEnabled()
        val iterations = RuntimeBenchmarkHarness.iterations(MIN_ITERATIONS, DEFAULT_ITERATIONS)
        val registry = HandlerWrapperRegistry(
            droppedCounter = {},
            exactAdmission = RuntimeBooleanSource { true },
        )
        val handler = Any()
        val runnable = Runnable {}

        val result = RuntimeBenchmarkHarness.measure(iterations, WARMUP_ITERATIONS) {
            registry.unregister(handler, runnable, null)
            1L
        }

        RuntimeBenchmarkHarness.report("handler registry missing removal", result)
        RuntimeBenchmarkHarness.assertBudget(
            "handler registry missing removal",
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
