package io.jankhunter.runtime

import com.sun.management.ThreadMXBean
import java.lang.management.ManagementFactory
import java.util.Locale
import kotlin.system.measureNanoTime
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue

internal object RuntimeBenchmarkHarness {
    fun measure(
        iterations: Int,
        warmupIterations: Int,
        operation: BenchmarkOperation,
    ): Result {
        var checksum = 0L
        repeat(warmupIterations) { checksum = checksum xor operation.run() }
        val threadId = Thread.currentThread().id
        val allocationOverhead = allocationReadOverhead(threadId)
        val allocatedBefore = allocatedBytes(threadId)
        val elapsedNs = measureNanoTime {
            repeat(iterations) { checksum = checksum xor operation.run() }
        }
        val allocatedAfter = allocatedBytes(threadId)
        sink = checksum
        val allocationBytesPerOperation = if (allocatedBefore >= 0L && allocatedAfter >= allocatedBefore) {
            (allocatedAfter - allocatedBefore - allocationOverhead).coerceAtLeast(0L).toDouble() / iterations
        } else {
            Double.NaN
        }
        return Result(iterations, elapsedNs, allocationBytesPerOperation)
    }

    fun report(name: String, result: Result) {
        println(
            "JankHunter benchmark: $name, iterations=${result.iterations}, total_ns=${result.elapsedNs}, " +
                "ns_per_op=${format(result.nanosPerOperation)}, " +
                "bytes_per_op=${format(result.allocationBytesPerOperation)}",
        )
    }

    fun assertBudget(
        name: String,
        result: Result,
        latencyBudgetNs: Double,
        allocationBudgetBytes: Double,
    ) {
        assertTrue(
            "$name latency budget exceeded: ${result.nanosPerOperation} ns/op",
            result.nanosPerOperation <= latencyBudgetNs,
        )
        if (!result.allocationBytesPerOperation.isNaN()) {
            assertTrue(
                "$name allocation budget exceeded: ${result.allocationBytesPerOperation} B/op",
                result.allocationBytesPerOperation <= allocationBudgetBytes,
            )
        }
    }

    fun iterations(minimum: Int, defaultIterations: Int): Int {
        return System.getProperty("jankhunter.benchmark.iterations")
            ?.toIntOrNull()
            ?.coerceAtLeast(minimum)
            ?: defaultIterations.coerceAtLeast(minimum)
    }

    fun assumeEnabled() {
        assumeTrue(System.getProperty("jankhunter.benchmark") == "true")
    }

    private fun allocatedBytes(threadId: Long): Long {
        val bean = allocationBean ?: return -1L
        if (!bean.isThreadAllocatedMemorySupported) return -1L
        if (!bean.isThreadAllocatedMemoryEnabled) bean.isThreadAllocatedMemoryEnabled = true
        return bean.getThreadAllocatedBytes(threadId)
    }

    private fun allocationReadOverhead(threadId: Long): Long {
        val before = allocatedBytes(threadId)
        val after = allocatedBytes(threadId)
        return if (before < 0L || after < before) 0L else after - before
    }

    private fun format(value: Double): String = String.format(Locale.US, "%.1f", value)

    fun interface BenchmarkOperation {
        fun run(): Long
    }

    data class Result(
        val iterations: Int,
        val elapsedNs: Long,
        val allocationBytesPerOperation: Double,
    ) {
        val nanosPerOperation: Double
            get() = elapsedNs.toDouble() / iterations
    }

    private val allocationBean = ManagementFactory.getThreadMXBean() as? ThreadMXBean

    @Volatile
    private var sink = 0L
}
