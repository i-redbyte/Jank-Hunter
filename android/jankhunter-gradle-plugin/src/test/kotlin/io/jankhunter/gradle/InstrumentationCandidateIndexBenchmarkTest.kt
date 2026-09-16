package io.jankhunter.gradle

import com.sun.management.ThreadMXBean
import java.lang.management.ManagementFactory
import java.util.Locale
import kotlin.system.measureNanoTime
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test

class InstrumentationCandidateIndexBenchmarkTest {
    @Test
    fun candidateLookupHasNoSteadyStateAllocation() {
        assumeTrue(System.getProperty("jankhunter.benchmark") == "true")
        val iterations = System.getProperty("jankhunter.benchmark.iterations")
            ?.toIntOrNull()
            ?.coerceAtLeast(MIN_ITERATIONS)
            ?: DEFAULT_ITERATIONS
        var checksum = 0
        repeat(WARMUP_ITERATIONS) {
            checksum = lookupPair(checksum)
        }

        val threadId = Thread.currentThread().threadId()
        val allocatedBefore = allocatedBytes(threadId)
        val elapsedNs = measureNanoTime {
            repeat(iterations) {
                checksum = lookupPair(checksum)
            }
        }
        benchmarkSink = checksum
        val allocatedAfter = allocatedBytes(threadId)
        val operations = iterations.toLong() * LOOKUPS_PER_ITERATION
        val nanosPerLookup = elapsedNs.toDouble() / operations
        val bytesPerLookup = if (allocatedBefore >= 0L && allocatedAfter >= allocatedBefore) {
            (allocatedAfter - allocatedBefore).toDouble() / operations
        } else {
            Double.NaN
        }
        println(
            "JankHunter benchmark: instrumentation candidate index, lookups=$operations, " +
                "ns_per_lookup=${format(nanosPerLookup)}, bytes_per_lookup=${format(bytesPerLookup)}",
        )
        assertTrue(nanosPerLookup <= LATENCY_BUDGET_NS)
        if (!bytesPerLookup.isNaN()) assertTrue(bytesPerLookup <= ALLOCATION_BUDGET_BYTES)
    }

    private fun lookupPair(checksum: Int): Int {
        return checksum xor
            HookIntentResolver.candidateMask("length", "()I") xor
            HookIntentResolver.candidateMask("post", "(Ljava/lang/Runnable;)Z")
    }

    private fun allocatedBytes(threadId: Long): Long {
        val bean = ManagementFactory.getThreadMXBean() as? ThreadMXBean ?: return -1L
        if (!bean.isThreadAllocatedMemorySupported) return -1L
        if (!bean.isThreadAllocatedMemoryEnabled) bean.isThreadAllocatedMemoryEnabled = true
        return bean.getThreadAllocatedBytes(threadId)
    }

    private fun format(value: Double): String = String.format(Locale.US, "%.1f", value)

    private companion object {
        const val MIN_ITERATIONS = 100_000
        const val DEFAULT_ITERATIONS = 500_000
        const val WARMUP_ITERATIONS = 20_000
        const val LOOKUPS_PER_ITERATION = 2L
        const val LATENCY_BUDGET_NS = 250.0
        const val ALLOCATION_BUDGET_BYTES = 0.5

        @Volatile
        var benchmarkSink: Int = 0
    }
}
