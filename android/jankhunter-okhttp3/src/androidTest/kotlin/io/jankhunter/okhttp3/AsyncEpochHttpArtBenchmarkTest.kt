package io.jankhunter.okhttp3

import android.os.Debug
import android.os.Process
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterConfig
import io.jankhunter.runtime.JankHunterRuntimeFeature
import java.io.File
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import org.json.JSONObject
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test

/** Measures the real SDK EventListener and writer; no network or OkHttp Call allocation in samples. */
class AsyncEpochHttpArtBenchmarkTest {
    @Test
    fun measureHttpListenerEpochTracking() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val args = InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("jankhunterAsyncEpochBenchmark") == "true")
        val label = requireNotNull(args.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        instrumentation.runOnMainSync { JankHunter.shutdown() }
        val output = File(instrumentation.context.filesDir, "async-epoch-http-$label.jsonl")
        output.writeText("")
        val request = Request.Builder().url("http://127.0.0.1/benchmark").build()
        val client = OkHttpClient()
        val call = client.newCall(request)
        val response = Response.Builder().request(request).protocol(Protocol.HTTP_1_1).code(200).message("OK").build()
        val factory = JankHunterEventListenerFactory()
        for (main in booleanArrayOf(true, false)) {
            for (active in booleanArrayOf(true, false)) {
                val root = File(instrumentation.context.cacheDir, "http-epoch-bench-$main-$active")
                root.deleteRecursively()
                val config = JankHunterConfig.builder().logDirectory(root).autoStartCollectors(false)
                    .runtimeCallGraphEnabled(false).metricAggregationEnabled(false).sessionLogSizeLimitEnabled(false)
                    .runtimeFeatureEnabled(JankHunterRuntimeFeature.HTTP, active).build()
                instrumentation.runOnMainSync { JankHunter.init(instrumentation.targetContext, config) }
                val action = Runnable {
                    val listener = factory.create(call)
                    listener.callStart(call)
                    listener.requestHeadersStart(call)
                    listener.requestHeadersEnd(call, request)
                    listener.responseHeadersStart(call)
                    listener.responseHeadersEnd(call, response)
                    listener.responseBodyStart(call)
                    listener.responseBodyEnd(call, 0L)
                    listener.callEnd(call)
                }
                val samples = LongArray(EVENTS)
                val execute = { task: Runnable -> if (main) instrumentation.runOnMainSync(task) else task.run() }
                try {
                    execute(Runnable {
                        repeat(WARMUP) { index ->
                            action.run()
                            if ((index + 1) % BATCH == 0) JankHunter.flush()
                        }
                    })
                    JankHunter.flush()
                    val allocatedBefore = allocated()
                    val processBefore = Process.getElapsedCpuTime()
                    val wallBefore = System.nanoTime()
                    var threadCPU = 0L
                    execute(Runnable {
                        var index = 0
                        while (index < EVENTS) {
                            val end = minOf(index + BATCH, EVENTS)
                            val cpu = Debug.threadCpuTimeNanos()
                            while (index < end) {
                                val at = System.nanoTime()
                                action.run()
                                samples[index++] = System.nanoTime() - at
                            }
                            threadCPU += Debug.threadCpuTimeNanos() - cpu
                            // Bound the offered burst below the 256-event critical reserve. The
                            // real writer drains outside hook samples and measured thread CPU.
                            JankHunter.flush()
                        }
                    })
                    JankHunter.flush()
                    val cpu = Process.getElapsedCpuTime() - processBefore
                    val bytes = allocated() - allocatedBefore
                    val wall = System.nanoTime() - wallBefore
                    assertTrue(JankHunter.initDiagnostics().toString(), JankHunter.isStarted())
                    samples.sort()
                    output.appendText(JSONObject().put("label", label).put("main", main).put("active", active)
                        .put("operations", EVENTS).put("thread_cpu_ns", threadCPU).put("process_cpu_ms", cpu)
                        .put("process_allocated_bytes", bytes).put("complete_wall_ns", wall)
                        .put("p50_ns", samples[EVENTS / 2]).put("p95_ns", samples[EVENTS * 95 / 100])
                        .put("p99_ns", samples[EVENTS * 99 / 100]).toString() + "\n")
                } finally {
                    instrumentation.runOnMainSync { JankHunter.shutdown() }
                }
                // Keep one actual log per phase so the same CLI can verify delivery on both revisions.
                val logs = root.walkTopDown().filter { it.isFile && it.extension == "jhlog" }.toList()
                check(logs.size == 1)
                logs.single().copyTo(File(instrumentation.context.filesDir,
                    "async-epoch-http-$label-$main-$active.jhlog"), overwrite = true)
                root.deleteRecursively()
            }
        }
    }

    private companion object {
        const val BATCH = 64
        const val WARMUP = 2_000
        const val EVENTS = 5_000
        fun allocated(): Long = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong()
    }
}
