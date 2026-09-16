package io.jankhunter.okhttp3

import android.os.Debug
import android.os.Process
import android.os.StrictMode
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterConfig
import io.jankhunter.runtime.JankHunterRuntimeFeature
import java.io.BufferedInputStream
import java.io.File
import java.net.InetAddress
import java.net.ServerSocket
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import okhttp3.Cache
import okhttp3.OkHttpClient
import okhttp3.Request
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test

/** Real reused HTTP/1 sockets, actual SDK and writer. Identical source runs before/after A-M4. */
class HttpTransportArtBenchmarkTest {
    @Test
    fun measureReusedHttpTransportWithRealWriter() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val args = InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("jankhunterHttpTransportBenchmark") == "true")
        val label = requireNotNull(args.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        instrumentation.runOnMainSync { JankHunter.shutdown() }
        for (cacheOnly in booleanArrayOf(false, true)) {
            val prefix = if (cacheOnly) "http-cache" else "http-transport"
            val output = File(instrumentation.context.filesDir, "$prefix-$label.jsonl")
            output.writeText("")
            for (main in booleanArrayOf(true, false)) {
                for (active in booleanArrayOf(true, false)) {
                    val root = File(instrumentation.context.cacheDir, "$prefix-bench-$main-$active")
                    root.deleteRecursively()
                    val config = JankHunterConfig.builder().logDirectory(root).autoStartCollectors(false)
                        .runtimeCallGraphEnabled(false).metricAggregationEnabled(false).sessionLogSizeLimitEnabled(false)
                        .runtimeFeatureEnabled(JankHunterRuntimeFeature.HTTP, active).build()
                    instrumentation.runOnMainSync { JankHunter.init(instrumentation.targetContext, config) }
                    val server = ServerSocket(0, 1, InetAddress.getByName("127.0.0.1")).apply { soTimeout = 10_000 }
                    val worker = Executors.newSingleThreadExecutor()
                    val served = worker.submit<Int> {
                        server.accept().use { socket ->
                            socket.soTimeout = 10_000
                            socket.tcpNoDelay = true
                            val input = BufferedInputStream(socket.getInputStream())
                            val bytes = ("HTTP/1.1 200 OK\r\nContent-Length: 1024\r\nCache-Control: max-age=3600\r\n\r\n" + "x".repeat(1024))
                                .toByteArray(Charsets.US_ASCII)
                            repeat(if (cacheOnly) 1 else WARMUP + EVENTS) {
                                var tail = 0
                                do {
                                    val next = input.read()
                                    check(next >= 0)
                                    tail = (tail shl 8) or next
                                } while (tail != 0x0d0a0d0a)
                                socket.getOutputStream().write(bytes)
                            }
                            if (cacheOnly) 1 else WARMUP + EVENTS
                        }
                    }
                    val cacheDirectory = File(instrumentation.context.cacheDir, "$prefix-native-cache-$main-$active")
                    cacheDirectory.deleteRecursively()
                    val cache = if (cacheOnly) Cache(cacheDirectory, 1024L * 1024L) else null
                    val client = OkHttpClient.Builder().cache(cache).eventListenerFactory(JankHunterEventListenerFactory())
                        .readTimeout(10L, TimeUnit.SECONDS).build()
                    val request = Request.Builder().url("http://127.0.0.1:${server.localPort}/transport-benchmark").build()
                    val samples = LongArray(EVENTS)
                    val task = Runnable {
                        val policy = StrictMode.getThreadPolicy()
                        StrictMode.setThreadPolicy(StrictMode.ThreadPolicy.Builder(policy).permitNetwork().build())
                        try {
                            fun exchange() {
                                client.newCall(request).execute().use { response ->
                                    check(response.code() == 200)
                                    checkNotNull(response.body()).source().skip(1024L)
                                }
                            }
                            repeat(WARMUP) { index -> exchange(); if ((index + 1) % BATCH == 0) JankHunter.flush() }
                            JankHunter.flush()
                            if (cacheOnly) check(cache!!.hitCount() == WARMUP - 1)
                            val allocatedBefore = allocated()
                            val processBefore = Process.getElapsedCpuTime()
                            val wallBefore = System.nanoTime()
                            var threadCPU = 0L
                            var index = 0
                            while (index < EVENTS) {
                                val end = minOf(index + BATCH, EVENTS)
                                val cpu = Debug.threadCpuTimeNanos()
                                while (index < end) {
                                    val at = System.nanoTime()
                                    exchange()
                                    samples[index++] = System.nanoTime() - at
                                }
                                threadCPU += Debug.threadCpuTimeNanos() - cpu
                                JankHunter.flush()
                            }
                            val wall = System.nanoTime() - wallBefore
                            val processCPU = Process.getElapsedCpuTime() - processBefore
                            val bytes = allocated() - allocatedBefore
                            if (cacheOnly) check(cache!!.hitCount() == WARMUP + EVENTS - 1)
                            samples.sort()
                            output.appendText(JSONObject().put("label", label).put("main", main).put("active", active)
                                .put("operations", EVENTS).put("thread_cpu_ns", threadCPU).put("process_cpu_ms", processCPU)
                                .put("process_allocated_bytes", bytes).put("complete_wall_ns", wall)
                                .put("p50_ns", samples[EVENTS / 2]).put("p95_ns", samples[EVENTS * 95 / 100])
                                .put("p99_ns", samples[EVENTS * 99 / 100]).put("pss_kb_after", Debug.getPss()).toString() + "\n")
                        } finally { StrictMode.setThreadPolicy(policy) }
                    }
                    try {
                        if (main) instrumentation.runOnMainSync(task) else task.run()
                        assertEquals(if (cacheOnly) 1 else WARMUP + EVENTS, served.get(10L, TimeUnit.SECONDS))
                    } finally {
                        client.dispatcher().cancelAll()
                        client.connectionPool().evictAll()
                        client.dispatcher().executorService().shutdown()
                        server.close()
                        worker.shutdownNow()
                        assertTrue(worker.awaitTermination(10L, TimeUnit.SECONDS))
                        cache?.close()
                        cacheDirectory.deleteRecursively()
                        instrumentation.runOnMainSync { JankHunter.shutdown() }
                    }
                    val logs = root.walkTopDown().filter { it.isFile && it.extension == "jhlog" }.toList()
                    check(logs.size == 1)
                    logs.single().copyTo(File(instrumentation.context.filesDir,
                        "$prefix-$label-$main-$active.jhlog"), overwrite = true)
                    root.deleteRecursively()
                }
            }
        }
    }

    private companion object {
        const val WARMUP = 100
        const val EVENTS = 500
        const val BATCH = 32
        fun allocated(): Long = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong()
    }
}
