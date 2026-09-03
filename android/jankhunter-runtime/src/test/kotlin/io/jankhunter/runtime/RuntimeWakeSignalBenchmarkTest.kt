package io.jankhunter.runtime

import io.jankhunter.runtime.internal.concurrent.CoalescedWakeSignal
import org.junit.Test

class RuntimeWakeSignalBenchmarkTest {
    @Test
    fun coalescedProducerWakeIsAllocationFreeAfterTheFirstSignal() {
        RuntimeBenchmarkHarness.assumeEnabled()
        val signal = CoalescedWakeSignal()
        signal.tryRequest()
        val result = RuntimeBenchmarkHarness.measure(1_000_000, 100_000) {
            if (signal.tryRequest()) 1L else 0L
        }

        RuntimeBenchmarkHarness.report("coalesced producer wake", result)
        RuntimeBenchmarkHarness.assertBudget(
            "coalesced producer wake",
            result,
            latencyBudgetNs = 100.0,
            allocationBudgetBytes = 0.5,
        )
    }
}
