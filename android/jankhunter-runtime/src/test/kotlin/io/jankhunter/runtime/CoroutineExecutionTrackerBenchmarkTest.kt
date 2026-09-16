package io.jankhunter.runtime

import org.junit.Test

class CoroutineExecutionTrackerBenchmarkTest {
    @Test
    fun suspendResumeHasNoSteadyStateAllocation() {
        RuntimeBenchmarkHarness.assumeEnabled()
        val iterations = RuntimeBenchmarkHarness.iterations(MIN_ITERATIONS, DEFAULT_ITERATIONS)
        var nowMs = 0L
        val continuation = Any()
        val tracker = CoroutineExecutionTracker(
            capacity = 16,
            shardCount = 1,
            clock = { nowMs },
            threadId = { 1L },
            onComplete = CoroutineExecutionSink { _, _, _, _, _, _, _ -> },
        )
        var token = tracker.enter(continuation, OWNER, collectNew = true)

        val result = RuntimeBenchmarkHarness.measure(iterations, WARMUP_ITERATIONS) {
            nowMs++
            tracker.exit(token, continuation, suspended = true, CoroutineExecutionOutcome.SUCCESS)
            nowMs++
            token = tracker.enter(continuation, OWNER, collectNew = true)
            token
        }

        RuntimeBenchmarkHarness.report("coroutine suspend-resume", result)
        RuntimeBenchmarkHarness.assertBudget(
            "coroutine suspend-resume",
            result,
            latencyBudgetNs = LATENCY_BUDGET_NS,
            allocationBudgetBytes = ALLOCATION_BUDGET_BYTES,
        )
    }

    private companion object {
        const val OWNER = "benchmark.Owner.load"
        const val MIN_ITERATIONS = 100_000
        const val DEFAULT_ITERATIONS = 500_000
        const val WARMUP_ITERATIONS = 20_000
        const val LATENCY_BUDGET_NS = 5_000.0
        const val ALLOCATION_BUDGET_BYTES = 0.5
    }
}
