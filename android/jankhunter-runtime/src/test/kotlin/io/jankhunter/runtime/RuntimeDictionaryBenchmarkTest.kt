package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.DictionaryIds
import io.jankhunter.runtime.internal.io.DictionaryLookupResult
import org.junit.Test

class RuntimeDictionaryBenchmarkTest {
    @Test
    fun existingSymbolLookupHasNoSteadyStateAllocation() {
        RuntimeBenchmarkHarness.assumeEnabled()
        val iterations = RuntimeBenchmarkHarness.iterations(MIN_ITERATIONS, DEFAULT_ITERATIONS)
        val dictionary = DictionaryIds(maxRegularEntries = 64, maxValueBytes = 1_024)
        val result = DictionaryLookupResult()
        dictionary.resolve(KIND, VALUE, result)

        val measurement = RuntimeBenchmarkHarness.measure(iterations, WARMUP_ITERATIONS) {
            dictionary.resolve(KIND, VALUE, result)
        }

        RuntimeBenchmarkHarness.report("existing dictionary lookup", measurement)
        RuntimeBenchmarkHarness.assertBudget(
            "existing dictionary lookup",
            measurement,
            latencyBudgetNs = LATENCY_BUDGET_NS,
            allocationBudgetBytes = ALLOCATION_BUDGET_BYTES,
        )
    }

    private companion object {
        const val KIND = 2
        const val VALUE = "benchmark.dictionary.value"
        const val MIN_ITERATIONS = 100_000
        const val DEFAULT_ITERATIONS = 500_000
        const val WARMUP_ITERATIONS = 20_000
        const val LATENCY_BUDGET_NS = 500.0
        const val ALLOCATION_BUDGET_BYTES = 0.5
    }
}
