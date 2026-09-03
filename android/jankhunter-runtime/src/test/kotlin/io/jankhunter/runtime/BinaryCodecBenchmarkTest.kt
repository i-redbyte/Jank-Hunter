package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.BinaryPayload
import io.jankhunter.runtime.internal.io.ColumnarMicroPage
import io.jankhunter.runtime.internal.io.Jhlog
import io.jankhunter.runtime.internal.io.PendingRuntimeCallsEventPool
import io.jankhunter.runtime.internal.io.PendingStableCountersEventPool
import io.jankhunter.runtime.internal.io.RuntimeNumericColumnCodec
import io.jankhunter.runtime.internal.io.RuntimeCallBatchPool
import io.jankhunter.runtime.internal.io.StableCounterBatchPool
import org.junit.Test

class BinaryCodecBenchmarkTest {
    @Test
    fun runtimeNumericColumnsAllocateNothingAtSteadyState() {
        RuntimeBenchmarkHarness.assumeEnabled()
        val values = LongArray(Jhlog.MAX_RUNTIME_CALL_BLOCK_ROWS) { 10_000L + it }
        val codec = RuntimeNumericColumnCodec()
        val output = BinaryPayload(256)
        val result = RuntimeBenchmarkHarness.measure(NUMERIC_ITERATIONS, NUMERIC_WARMUP) {
            output.clear()
            codec.encode(values, values.size, output)
            output.size.toLong()
        }

        RuntimeBenchmarkHarness.report("runtime numeric column encoding", result)
        RuntimeBenchmarkHarness.assertBudget(
            "runtime numeric column encoding",
            result,
            latencyBudgetNs = NUMERIC_LATENCY_BUDGET_NS,
            allocationBudgetBytes = ALLOCATION_BUDGET_BYTES,
        )
    }

    @Test
    fun ransMicroPageEncodingAllocatesNothingAtSteadyState() {
        RuntimeBenchmarkHarness.assumeEnabled()
        val page = ColumnarMicroPage()
        val row = BinaryPayload().bytes(ByteArray(64) { if (it and 3 == 0) 1 else 0 })
        repeat(Jhlog.MAX_MICRO_PAGE_ROWS) {
            page.append(Jhlog.TYPE_COUNTER, 0L, row, null, null, 1L)
        }
        val output = BinaryPayload(16 * 1024)
        page.encodeTo(output, entropyEnabled = true)
        val result = RuntimeBenchmarkHarness.measure(RANS_ITERATIONS, RANS_WARMUP) {
            page.encodeTo(output, entropyEnabled = true)
            output.size.toLong()
        }

        RuntimeBenchmarkHarness.report("rANS micro-page encoding", result)
        RuntimeBenchmarkHarness.assertBudget(
            "rANS micro-page encoding",
            result,
            latencyBudgetNs = RANS_LATENCY_BUDGET_NS,
            allocationBudgetBytes = ALLOCATION_BUDGET_BYTES,
        )
    }

    @Test
    fun runtimeBatchReuseAllocatesNothingAtSteadyState() {
        RuntimeBenchmarkHarness.assumeEnabled()
        val pool = RuntimeCallBatchPool(capacity = 4, batchCapacity = 8)
        val result = RuntimeBenchmarkHarness.measure(NUMERIC_ITERATIONS, NUMERIC_WARMUP) {
            val batch = pool.acquire()
            batch.add("screen", 1L, "caller", 2L, 3L, "callee", 4L, 5L, 6L)
            batch.recycle()
            batch.size.toLong()
        }

        RuntimeBenchmarkHarness.report("runtime batch reuse", result)
        RuntimeBenchmarkHarness.assertBudget(
            "runtime batch reuse",
            result,
            latencyBudgetNs = BATCH_REUSE_LATENCY_BUDGET_NS,
            allocationBudgetBytes = ALLOCATION_BUDGET_BYTES,
        )
    }

    @Test
    fun runtimeBatchQueueEnvelopeAllocatesNothingAtSteadyState() {
        RuntimeBenchmarkHarness.assumeEnabled()
        val batchPool = RuntimeCallBatchPool(capacity = 4, batchCapacity = 8)
        val eventPool = PendingRuntimeCallsEventPool(capacity = 4)
        val result = RuntimeBenchmarkHarness.measure(NUMERIC_ITERATIONS, NUMERIC_WARMUP) {
            val batch = batchPool.acquire()
            batch.add("screen", 1L, "caller", 2L, 3L, "callee", 4L, 5L, 6L)
            eventPool.acquire(null, batch).recycle()
            batch.size.toLong()
        }

        RuntimeBenchmarkHarness.report("runtime batch queue-envelope reuse", result)
        RuntimeBenchmarkHarness.assertBudget(
            "runtime batch queue-envelope reuse",
            result,
            latencyBudgetNs = BATCH_REUSE_LATENCY_BUDGET_NS,
            allocationBudgetBytes = ALLOCATION_BUDGET_BYTES,
        )
    }

    @Test
    fun stableCounterBatchAndEnvelopeAllocateNothingAtSteadyState() {
        RuntimeBenchmarkHarness.assumeEnabled()
        val batchPool = StableCounterBatchPool(capacity = 4, batchCapacity = 8)
        val eventPool = PendingStableCountersEventPool(capacity = 4)
        val result = RuntimeBenchmarkHarness.measure(NUMERIC_ITERATIONS, NUMERIC_WARMUP) {
            val batch = batchPool.acquire()
            batch.add(1L, "method", 2L)
            eventPool.acquire(null, batch).recycle()
            batch.size.toLong()
        }

        RuntimeBenchmarkHarness.report("stable counter batch queue-envelope reuse", result)
        RuntimeBenchmarkHarness.assertBudget(
            "stable counter batch queue-envelope reuse",
            result,
            latencyBudgetNs = BATCH_REUSE_LATENCY_BUDGET_NS,
            allocationBudgetBytes = ALLOCATION_BUDGET_BYTES,
        )
    }

    @Test
    fun longUtf8TruncationAvoidsPerCodePointGarbage() {
        RuntimeBenchmarkHarness.assumeEnabled()
        val value = "😀".repeat(4_096)
        val payload = BinaryPayload()
        val result = RuntimeBenchmarkHarness.measure(RANS_ITERATIONS, RANS_WARMUP) {
            payload.clear().boundedString(value, 1_024)
            payload.size.toLong()
        }

        RuntimeBenchmarkHarness.report("long UTF-8 truncation", result)
        RuntimeBenchmarkHarness.assertBudget(
            "long UTF-8 truncation",
            result,
            latencyBudgetNs = UTF8_TRUNCATION_LATENCY_BUDGET_NS,
            allocationBudgetBytes = UTF8_TRUNCATION_ALLOCATION_BUDGET_BYTES,
        )
    }

    private companion object {
        const val NUMERIC_ITERATIONS = 100_000
        const val NUMERIC_WARMUP = 10_000
        const val RANS_ITERATIONS = 2_000
        const val RANS_WARMUP = 200
        const val NUMERIC_LATENCY_BUDGET_NS = 20_000.0
        const val RANS_LATENCY_BUDGET_NS = 2_000_000.0
        const val BATCH_REUSE_LATENCY_BUDGET_NS = 2_000.0
        const val UTF8_TRUNCATION_LATENCY_BUDGET_NS = 100_000.0
        const val UTF8_TRUNCATION_ALLOCATION_BUDGET_BYTES = 0.5
        const val ALLOCATION_BUDGET_BYTES = 0.5
    }
}
