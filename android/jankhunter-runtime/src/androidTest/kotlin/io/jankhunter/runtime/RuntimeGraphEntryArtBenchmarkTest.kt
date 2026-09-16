package io.jankhunter.runtime

import android.os.Debug
import android.os.Process
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.io.File
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test

/** Actual method stack, aggregate pages, consumer and writer; same workload before/after A-M14b. */
class RuntimeGraphEntryArtBenchmarkTest {
    @Test
    fun measureNestedMethodEntriesAndCompletedEdges() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val args = InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("jankhunterGraphEntryBenchmark") == "true")
        val label = requireNotNull(args.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        val output = File(instrumentation.context.filesDir, "graph-entry-$label.jsonl")
        output.writeText("")
        for (main in booleanArrayOf(true, false)) {
            for (active in booleanArrayOf(true, false)) {
                val root = File(instrumentation.context.cacheDir, "graph-entry-$main-$active").apply { deleteRecursively() }
                val config = JankHunterConfig.builder().autoStartCollectors(false).runtimeCallGraphEnabled(active).build()
                val writer = AsyncLogWriterFactory().open(root, config, "main")
                val graph = RuntimeCallGraph({ System.nanoTime() / 1_000_000L }, { "Screen" }, { 1L }, { 128 })
                graph.resetFlushState(writer)
                val samples = LongArray(EVENTS)
                val work = Runnable {
                    fun edge() {
                        val parent = graph.enter(1L, "app.Parent", active)
                        val child = graph.enter(2L, "app.Child", active)
                        graph.exit(child, 2L)
                        graph.exit(parent, 1L)
                    }
                    repeat(WARMUP) { edge() }
                    assertTrue(graph.flushBlocking(5_000L))
                    val bytesBefore = allocated()
                    val processBefore = Process.getElapsedCpuTime()
                    val cpuBefore = Debug.threadCpuTimeNanos()
                    repeat(EVENTS) { index ->
                        val start = System.nanoTime()
                        edge()
                        samples[index] = System.nanoTime() - start
                    }
                    val cpu = Debug.threadCpuTimeNanos() - cpuBefore
                    val process = Process.getElapsedCpuTime() - processBefore
                    val bytes = allocated() - bytesBefore
                    samples.sort()
                    output.appendText(JSONObject().put("label", label).put("main", main).put("active", active)
                        .put("events", EVENTS).put("p50_ns", samples[EVENTS / 2]).put("p95_ns", samples[EVENTS * 95 / 100])
                        .put("p99_ns", samples[EVENTS * 99 / 100]).put("thread_cpu_ns", cpu)
                        .put("process_cpu_ms", process).put("process_allocated_bytes", bytes).toString() + "\n")
                }
                try {
                    if (main) instrumentation.runOnMainSync(work) else work.run()
                    assertTrue(graph.flushBlocking(5_000L))
                    val expected = if (active) WARMUP.toLong() + EVENTS else 0L
                    assertEquals(expected, graph.attemptedForTest())
                    assertEquals(expected, graph.acceptedForTest())
                    assertEquals(expected, graph.emittedForTest())
                    assertEquals(0L, graph.acceptedEventLossForTest())
                } finally {
                    assertTrue(graph.flushForShutdown(5_000L))
                    graph.clear()
                    writer.close()
                }
                val logs = root.walkTopDown().filter { it.isFile && it.extension == "jhlog" }.toList()
                if (active) {
                    check(logs.size == 1)
                    logs.single().copyTo(File(instrumentation.context.filesDir, "graph-entry-$label-$main-$active.jhlog"), true)
                } else {
                    check(logs.isEmpty()) // The direct writer stays lazy when no graph event is admitted.
                }
                root.deleteRecursively()
            }
        }
    }

    private companion object {
        const val EVENTS = 50_000
        const val WARMUP = 500_000
        fun allocated(): Long = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong()
    }
}
