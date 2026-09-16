package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.MetricAggregator
import java.nio.file.Files
import org.junit.Test

class RuntimeMetricBenchmarkTest {
    @Test
    fun executorStartBatchHasBoundedSteadyStateCost() {
        RuntimeBenchmarkHarness.assumeEnabled()
        val iterations = RuntimeBenchmarkHarness.iterations(MIN_ITERATIONS, DEFAULT_ITERATIONS)
        val directory = Files.createTempDirectory("jankhunter-executor-metrics-benchmark").toFile()
        val config = JankHunterConfig.builder()
            .metricAggregationEnabled(true)
            .maxMetricAggregationKeys(32)
            .build()
        val writer = AsyncLogWriterFactory().open(directory, config, "main")
        val service = RuntimeMetricsService(
            defaultMaxKeys = 32,
            nowMs = { 1L },
            writer = { writer },
            config = { config },
            ensureContextRecorded = {},
            executeMaintenance = { true },
            executeDelayedMaintenance = { _, _ -> true },
        )
        val keys = ExecutorMetricKeys("benchmark", "owner")
        service.configure(32, exactAdmission = true)

        try {
            var completed = 0L
            val result = RuntimeBenchmarkHarness.measure(iterations, WARMUP_ITERATIONS) {
                completed++
                service.recordExecutorStarted(keys, 1L, 0, 1, 4, completed)
                completed
            }

            RuntimeBenchmarkHarness.report("executor start metric batch", result)
            RuntimeBenchmarkHarness.assertBudget(
                "executor start metric batch",
                result,
                latencyBudgetNs = EXECUTOR_BATCH_LATENCY_BUDGET_NS,
                allocationBudgetBytes = ALLOCATION_BUDGET_BYTES,
            )
        } finally {
            service.flushBlocking(1_000L)
            writer.close()
            directory.deleteRecursively()
        }
    }

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

    @Test
    fun bestEffortEvictionDoesNotAllocateCandidateCollections() {
        RuntimeBenchmarkHarness.assumeEnabled()
        val iterations = RuntimeBenchmarkHarness.iterations(MIN_ITERATIONS, DEFAULT_ITERATIONS)
        val aggregator = MetricAggregator(maxKeys = EVICTION_CAPACITY)
        val names = Array(EVICTION_CAPACITY + 1) { index -> "benchmark.eviction.$index" }
        repeat(EVICTION_CAPACITY) { index -> aggregator.counter(names[index], 1L) }
        var index = EVICTION_CAPACITY

        val result = RuntimeBenchmarkHarness.measure(iterations, WARMUP_ITERATIONS) {
            aggregator.counter(names[index], 1L)
            index++
            if (index == names.size) index = 0
            index.toLong()
        }

        RuntimeBenchmarkHarness.report("best effort metric eviction", result)
        RuntimeBenchmarkHarness.assertBudget(
            "best effort metric eviction",
            result,
            latencyBudgetNs = EVICTION_LATENCY_BUDGET_NS,
            allocationBudgetBytes = EVICTION_ALLOCATION_BUDGET_BYTES,
        )
    }

    private companion object {
        const val COUNTER = "benchmark.counter"
        const val GAUGE = "benchmark.gauge"
        const val MIN_ITERATIONS = 100_000
        const val DEFAULT_ITERATIONS = 500_000
        const val WARMUP_ITERATIONS = 20_000
        const val EVICTION_CAPACITY = 64
        const val LATENCY_BUDGET_NS = 2_000.0
        const val EXECUTOR_BATCH_LATENCY_BUDGET_NS = 10_000.0
        const val EVICTION_LATENCY_BUDGET_NS = 20_000.0
        const val ALLOCATION_BUDGET_BYTES = 0.5
        const val EVICTION_ALLOCATION_BUDGET_BYTES = 256.0
    }
}
