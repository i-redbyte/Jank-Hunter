package io.jankhunter.runtime

import android.os.Debug
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test

class RuntimeGraphStorageArtBenchmarkTest {
    @Test
    fun measureFirstEdgeStorageAndRepeatedAggregation() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val args = InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("jankhunterGraphStorageBenchmark") == "true")
        val label = requireNotNull(args.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        val file = File(instrumentation.context.filesDir, "graph-storage-$label.jsonl")
        file.writeText("")
        for (main in booleanArrayOf(true, false)) {
            val work = Runnable {
                repeat(100) { add(RuntimeGraphAggregateBuffer(Thread.currentThread())) }
                val buffers = arrayOfNulls<RuntimeGraphAggregateBuffer>(COLD_EVENTS)
                measure(file, label, main, "first_edge", COLD_EVENTS) { index ->
                    val buffer = RuntimeGraphAggregateBuffer(Thread.currentThread())
                    add(buffer)
                    buffers[index] = buffer
                }
                assertTrue(buffers.all { it?.bufferedLogicalEventCount() == 1L })
                val hot = checkNotNull(buffers[0])
                buffers.fill(null)
                repeat(WARMUP) { add(hot) }
                measure(file, label, main, "repeated_edge", HOT_EVENTS) { add(hot) }
                assertEquals(1L + WARMUP + HOT_EVENTS, hot.bufferedLogicalEventCount())
            }
            if (main) instrumentation.runOnMainSync(work) else work.run()
        }
    }

    private inline fun measure(
        file: File, label: String, main: Boolean, phase: String, events: Int, action: (Int) -> Unit,
    ) {
        val samples = LongArray(events)
        val bytesBefore = allocated()
        val cpuBefore = Debug.threadCpuTimeNanos()
        val started = System.nanoTime()
        repeat(events) { index ->
            val before = System.nanoTime()
            action(index)
            samples[index] = System.nanoTime() - before
        }
        val elapsed = System.nanoTime() - started
        val cpu = Debug.threadCpuTimeNanos() - cpuBefore
        val bytes = allocated() - bytesBefore
        samples.sort()
        file.appendText(JSONObject().put("label", label).put("main", main).put("phase", phase)
            .put("events", events).put("wall_ns", elapsed).put("thread_cpu_ns", cpu)
            .put("process_allocated_bytes", bytes).put("p50_ns", samples[events / 2])
            .put("p95_ns", samples[events * 95 / 100]).put("p99_ns", samples[events * 99 / 100])
            .toString() + "\n")
        if (InstrumentationRegistry.getArguments().getString("jankhunterAssertLazyGraphStorage") == "true") {
            val budget = if (phase == "first_edge") events * 10_000L + 65_536L else 65_536L
            assertTrue("$phase allocated $bytes bytes; budget=$budget", bytes <= budget)
        }
    }

    private fun add(buffer: RuntimeGraphAggregateBuffer) {
        check(buffer.tryAdd(1L, "caller", 2L, "callee", null, 0L, 1L) == RUNTIME_GRAPH_ADD_AGGREGATED)
    }

    private companion object {
        const val COLD_EVENTS = 500
        const val HOT_EVENTS = 50_000
        const val WARMUP = 500_000
        fun allocated(): Long = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong()
    }
}
