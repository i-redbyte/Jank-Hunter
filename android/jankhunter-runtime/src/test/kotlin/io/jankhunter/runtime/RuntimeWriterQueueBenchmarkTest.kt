package io.jankhunter.runtime

import io.jankhunter.runtime.internal.concurrent.BoundedMpscQueue
import io.jankhunter.runtime.internal.concurrent.BoundedSegmentedQueue
import org.junit.Test

class RuntimeWriterQueueBenchmarkTest {
    @Test
    fun segmentedQueueSteadyStateHasBoundedCost() {
        RuntimeBenchmarkHarness.assumeEnabled()
        val iterations = RuntimeBenchmarkHarness.iterations(MIN_ITERATIONS, DEFAULT_ITERATIONS)
        val queue = BoundedSegmentedQueue<QueueValue>(65_536)
        val value = QueueValue()

        val result = RuntimeBenchmarkHarness.measure(iterations, WARMUP_ITERATIONS) {
            check(queue.tryOffer(value) == BoundedMpscQueue.OfferResult.OFFERED)
            check(queue.poll() === value)
            1L
        }

        RuntimeBenchmarkHarness.report("segmented writer queue offer/poll", result)
        RuntimeBenchmarkHarness.assertBudget(
            "segmented writer queue offer/poll",
            result,
            latencyBudgetNs = LATENCY_BUDGET_NS,
            allocationBudgetBytes = ALLOCATION_BUDGET_BYTES,
        )
    }

    private class QueueValue

    private companion object {
        const val MIN_ITERATIONS = 100_000
        const val DEFAULT_ITERATIONS = 500_000
        const val WARMUP_ITERATIONS = 20_000
        const val LATENCY_BUDGET_NS = 1_000.0
        const val ALLOCATION_BUDGET_BYTES = 0.5
    }
}
