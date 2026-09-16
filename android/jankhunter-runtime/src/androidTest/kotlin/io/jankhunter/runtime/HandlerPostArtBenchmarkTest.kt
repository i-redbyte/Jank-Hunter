package io.jankhunter.runtime

import android.os.Debug
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.io.File
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assume.assumeTrue
import org.junit.Test

class HandlerPostArtBenchmarkTest {
    @Test
    fun measurePostAndCancellationOnMainAndWorker() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val arguments = InstrumentationRegistry.getArguments()
        assumeTrue(arguments.getString("jankhunterHandlerBenchmark") == "true")
        val label = requireNotNull(arguments.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        val output = File(instrumentation.context.filesDir, "handler-post-$label.jsonl")
        output.writeText("")
        for (main in booleanArrayOf(true, false)) {
            val task = Runnable { measure(output, label, main) }
            if (main) instrumentation.runOnMainSync(task) else task.run()
        }
    }

    private fun measure(output: File, label: String, main: Boolean) {
        val directory = File(output.parentFile, "handler-benchmark-session")
        val config = JankHunterConfig.builder().autoStartCollectors(false).build()
        val writer = AsyncLogWriterFactory().open(directory, config, "handler-benchmark")
        val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1_000L })
        graph.state.writer = writer
        graph.state.config = config
        graph.coordinator.markStarted(config)
        val handler = Handler(Looper.getMainLooper())
        val original = Runnable { throw AssertionError("cancelled benchmark task executed") }
        val due = SystemClock.uptimeMillis() + FUTURE_DELAY_MS
        try {
            repeat(WARMUP) { cycle(graph, handler, original, due) }
            val samples = LongArray(EVENTS)
            val bytesBefore = allocated()
            val cpuBefore = Debug.threadCpuTimeNanos()
            val started = System.nanoTime()
            repeat(EVENTS) { index ->
                val before = System.nanoTime()
                cycle(graph, handler, original, due)
                samples[index] = System.nanoTime() - before
            }
            val wall = System.nanoTime() - started
            val cpu = Debug.threadCpuTimeNanos() - cpuBefore
            val bytes = allocated() - bytesBefore
            assertEquals(false, handler.hasCallbacks(original))
            samples.sort()
            output.appendText(JSONObject().put("label", label).put("main", main).put("cycles", EVENTS)
                .put("wall_ns", wall).put("thread_cpu_ns", cpu).put("process_allocated_bytes", bytes)
                .put("p50_ns", samples[EVENTS / 2]).put("p95_ns", samples[EVENTS * 95 / 100])
                .put("p99_ns", samples[EVENTS * 99 / 100]).toString() + "\n")
        } finally {
            handler.removeCallbacks(original)
            graph.session.stop(clearInit = true)
            writer.close()
            directory.deleteRecursively()
        }
    }

    private fun cycle(graph: RuntimeComponentGraph, handler: Handler, original: Runnable, due: Long) {
        val queued = graph.handlerHooks.wrap(handler, original, null, "BenchmarkOwner")
        val posted = handler.postAtTime(queued, due)
        graph.handlerHooks.onPostResult(original, queued, posted)
        check(posted)
        // Clean both implementations using the actual queued identity; platform correctness has separate tests.
        handler.removeCallbacks(queued)
        graph.handlerHooks.clear(handler, original, null)
    }

    private companion object {
        const val FUTURE_DELAY_MS = 3_600_000L
        const val WARMUP = 100_000
        const val EVENTS = 50_000
        fun allocated(): Long = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong()
    }
}
