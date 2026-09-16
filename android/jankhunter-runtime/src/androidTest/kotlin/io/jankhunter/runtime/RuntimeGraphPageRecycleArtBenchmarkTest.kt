package io.jankhunter.runtime

import android.os.Debug
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test

/** Measures the allocation/CPU tradeoff of recycling detached pages with identical payload work. */
class RuntimeGraphPageRecycleArtBenchmarkTest {
    @Test
    fun measureDetachedPageReuseAgainstAllocation() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val args = InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("jankhunterGraphRecycleBenchmark") == "true")
        val label = requireNotNull(args.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        val output = File(instrumentation.context.filesDir, "graph-recycle-$label.jsonl")
        output.writeText("")
        val freshAllocatedBytes = LongArray(2)
        for (main in booleanArrayOf(true, false)) for (recycle in booleanArrayOf(false, true)) {
            val budget = RuntimeGraphStorageBudget()
            val work = Runnable {
                fun page() {
                    val value = if (recycle) checkNotNull(budget.acquirePage()) else RuntimeGraphAggregatePage()
                    check(value.add(1L, "caller", 2L, "callee", "Screen", 1L, 3L))
                    check(value.logicalEventCount() == 1L)
                    value.clear()
                    if (recycle) budget.recyclePage(value)
                }
                repeat(1_000) { page() }
                val samples = LongArray(EVENTS)
                val before = allocated()
                val cpuBefore = Debug.threadCpuTimeNanos()
                repeat(EVENTS) { index ->
                    val start = System.nanoTime()
                    page()
                    samples[index] = System.nanoTime() - start
                }
                val cpu = Debug.threadCpuTimeNanos() - cpuBefore
                val bytes = allocated() - before
                samples.sort()
                output.appendText(JSONObject().put("label", label).put("main", main).put("recycle", recycle)
                    .put("events", EVENTS).put("p50_ns", samples[EVENTS / 2]).put("p95_ns", samples[EVENTS * 95 / 100])
                    .put("p99_ns", samples[EVENTS * 99 / 100]).put("thread_cpu_ns", cpu)
                    .put("process_allocated_bytes", bytes).put("retained_quota_bytes", budget.usedBytes()).toString() + "\n")
                val slot = if (main) 0 else 1
                if (recycle) {
                    // ART reports a process-wide, TLAB-granular counter, not this thread's exact
                    // allocations. Compare matched payload work instead of claiming exact zero.
                    assertTrue("recycling=$bytes, fresh=${freshAllocatedBytes[slot]}",
                        bytes >= 0L && bytes * 100L < freshAllocatedBytes[slot])
                    assertEquals(RuntimeGraphStorageBudget.PAGE_BYTES, budget.usedBytes())
                } else freshAllocatedBytes[slot] = bytes
            }
            if (main) instrumentation.runOnMainSync(work) else work.run()
            budget.close()
            assertEquals(0L, budget.usedBytes())
        }
    }

    private companion object {
        const val EVENTS = 10_000
        fun allocated(): Long = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong()
    }
}
