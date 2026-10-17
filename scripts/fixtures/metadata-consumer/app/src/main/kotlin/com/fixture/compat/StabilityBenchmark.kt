package com.fixture.compat

import android.content.Context
import android.view.FrameMetrics
import android.view.Window
import android.view.MotionEvent
import android.os.Debug
import android.os.Handler
import android.os.Looper
import android.os.Process
import android.os.SystemClock
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterManifestConfig
import java.io.File
import java.util.Arrays
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import org.json.JSONObject

/** Paced main-thread workload. Pair each direct hook duration with adjacent timer-only control. */
object StabilityBenchmark {
    private fun percentileUpperBound(histogram: LongArray, samples: Long, percentile: Long): Long {
        val target = (samples * percentile + 99L) / 100L
        var count = 0L
        for (index in histogram.indices) {
            count += histogram[index]
            if (count >= target) return if (index == histogram.lastIndex) Long.MAX_VALUE else (index + 1L) * 500L
        }
        error("missing full-window hook samples")
    }
    @JvmStatic fun run(context: Context, mode: String, seconds: Int): String {
        check(mode in setOf("off", "normal", "detailed", "full", "full_no_pipeline", "full_callback"))
        check(seconds in 10..3600)
        JankHunter.shutdown()
        val full = mode.startsWith("full")
        JankHunter.init(context, JankHunterManifestConfig.read(context).toBuilder()
            .runtimeEnabled(mode != "off").runtimeCallGraphEnabled(full)
            .concurrencyDetailedEnabled(mode == "detailed")
            .logDirectory(File(context.filesDir, "benchmark-$mode-${SystemClock.elapsedRealtimeNanos()}"))
            .build())
        val pipeline = HookFixture.create({}, {}, {})
        // Exercise a bounded synchronous supplier under the fixture monitor, without disk I/O.
        val hookSamples = LongArray(65_536)
        // All measured pairs, fixed 500ns bins through1ms plus an explicit overflow bucket.
        val hookHistogram = LongArray(2_002)
        val dispatchSamples = LongArray(65_536)
        var measured = 0L
        var worst = 0L
        val failure = AtomicReference<Throwable?>()
        val finished = CountDownLatch(1)
        val handler = Handler(Looper.getMainLooper())
        val activity = context as BenchmarkActivity
        val frameSamples = LongArray(20_000)
        var frameCount = 0L
        var frameDrops = 0L
        var inputCount = 0L
        var inputWorst = 0L
        val frameListener = Window.OnFrameMetricsAvailableListener { _, metrics, drops ->
            val duration = metrics.getMetric(FrameMetrics.TOTAL_DURATION)
            if (duration >= 0L) { frameSamples[(frameCount % frameSamples.size).toInt()] = duration; frameCount++ }
            frameDrops += drops.coerceAtLeast(0)
        }
        val frameInstalled = CountDownLatch(1)
        check(handler.post { activity.window.addOnFrameMetricsAvailableListener(frameListener, handler); frameInstalled.countDown() })
        check(frameInstalled.await(5L, TimeUnit.SECONDS))
        val warmupEnd = SystemClock.elapsedRealtimeNanos() + TimeUnit.SECONDS.toNanos(5L)
        val end = warmupEnd + TimeUnit.SECONDS.toNanos(seconds.toLong())
        var started = 0L
        var cpuStarted = 0L
        var runtimeStarted = emptyMap<String, String>()
        var requestedAt = SystemClock.elapsedRealtimeNanos()
        val workload = object : Runnable {
            override fun run() {
                try {
                    val now = SystemClock.elapsedRealtimeNanos()
                    if (now >= end) { finished.countDown(); return }
                    val measuring = now >= warmupEnd
                    if (measuring && started == 0L) {
                        started = now
                        cpuStarted = Process.getElapsedCpuTime()
                        runtimeStarted = Debug.getRuntimeStats()
                        frameCount = 0L; frameDrops = 0L
                    }
                    if (measuring) {
                        val input = MotionEvent.obtain(SystemClock.uptimeMillis(), SystemClock.uptimeMillis(), MotionEvent.ACTION_DOWN, 1f, 1f, 0)
                        val at = SystemClock.elapsedRealtimeNanos()
                        try { check(activity.dispatchTouchEvent(input)) } finally { input.recycle() }
                        inputCount++
                        inputWorst = maxOf(inputWorst, activity.lastInputAt - at)
                    }
                    repeat(32) {
                        val c0 = SystemClock.elapsedRealtimeNanos()
                        val c1 = SystemClock.elapsedRealtimeNanos()
                        val t0 = SystemClock.elapsedRealtimeNanos()
                        val token = HookFixture.enter(90001L, "fixture.direct")
                        HookFixture.exit(token, 90001L)
                        val duration = (SystemClock.elapsedRealtimeNanos() - t0 - (c1 - c0)).coerceAtLeast(0L)
                        if (mode == "full_no_pipeline") {
                            check(1 + 1 == 2)
                        } else if (mode == "full_callback") {
                            synchronized(this) {
                                val callbackToken = HookFixture.enter(90002L, "fixture.callback")
                                HookFixture.exit(callbackToken, 90002L)
                            }
                        } else pipeline.logDebug({ "fixture" }, "fixture")
                        if (measuring) {
                            hookSamples[(measured % hookSamples.size).toInt()] = duration
                            hookHistogram[(duration / 500L).coerceAtMost(2_001L).toInt()]++
                            dispatchSamples[(measured % dispatchSamples.size).toInt()] = (now - requestedAt).coerceAtLeast(0L)
                            measured++
                            if (duration > worst) worst = duration
                        }
                    }
                    requestedAt = SystemClock.elapsedRealtimeNanos() + TimeUnit.MILLISECONDS.toNanos(20L)
                    check(handler.postDelayed(this, 20L))
                } catch (error: Throwable) { failure.set(error); finished.countDown() }
            }
        }
        try {
            check(handler.post(workload))
            check(finished.await(seconds.toLong() + 30L, TimeUnit.SECONDS)) { "main benchmark deadline exceeded" }
            failure.get()?.let { throw it }
            val wall = SystemClock.elapsedRealtimeNanos() - started
            val cpu = Process.getElapsedCpuTime() - cpuStarted
            val retained = minOf(measured, hookSamples.size.toLong()).toInt()
            check(retained > 1000)
            val hooks = hookSamples.copyOf(retained).apply { Arrays.sort(this) }
            val dispatch = dispatchSamples.copyOf(retained).apply { Arrays.sort(this) }
            check(frameCount > 0L && inputCount > 0L)
            val frames = frameSamples.copyOf(minOf(frameCount, frameSamples.size.toLong()).toInt()).apply { Arrays.sort(this) }
            val result = JSONObject().put("mode", mode).put("seconds", seconds)
                .put("wall_ns", wall).put("cpu_ms", cpu).put("cpu_one_core_percent", cpu * 100_000_000.0 / wall)
                .put("samples", measured).put("retained_samples", retained)
                .put("direct_hook_p50_ns", percentileUpperBound(hookHistogram, measured, 50))
                .put("direct_hook_p95_ns", percentileUpperBound(hookHistogram, measured, 95))
                .put("direct_hook_tail_p95_ns", hooks[retained * 95 / 100])
                .put("direct_hook_histogram", org.json.JSONArray(hookHistogram.toList()))
                .put("direct_hook_worst_ns", worst)
                .put("main_dispatch_p95_ns", dispatch[retained * 95 / 100])
                .put("throughput_per_second", measured * 1_000_000_000.0 / wall)
                .put("runtime_stats_before", JSONObject().also { stats -> runtimeStarted.forEach { (name, value) -> stats.put(name, value) } }).put("runtime_stats_after",
                    JSONObject().also { stats -> Debug.getRuntimeStats().forEach { (name, value) -> stats.put(name, value) } })
                .put("frames", JSONObject().put("count", frameCount).put("dropped_callbacks", frameDrops)
                    .put("p50_ns", frames[frames.size / 2]).put("p95_ns", frames[frames.size * 95 / 100]))
                .put("input_samples", inputCount).put("synthetic_input_dispatch_worst_ns", inputWorst)
                .put("input_methodology", "synthetic MotionEvent dispatch duration plus scheduled-main marker latency; not end-to-end physical input latency")
                .put("methodology", "adjacent timer-only subtraction for direct enter/exit; full-window500ns histogram upper bound, overflow explicit; paced 32 calls/20ms on main; dispatch marker is not input latency")
            val output = checkNotNull(context.getExternalFilesDir(null))
            File(output, "stability-benchmark.json").writeText(result.toString() + "\n")
            if (mode != "off") {
                // A terminal drain establishes an exact accepted frontier for the benchmark.
                // Active snapshot concurrency is tested separately and remains partial/unknown.
                check(JankHunter.setRuntimeEnabled(false, "benchmark-terminal-frontier"))
                val archives = checkNotNull(JankHunter.captureSessionArchives(File(output, "closed-$mode-${SystemClock.elapsedRealtimeNanos()}"), false))
                check(archives.size == 1)
                val destination = File(output, "stability-benchmark.zip")
                check(!destination.exists())
                check(File(archives.single()).renameTo(destination))
            }
            return "benchmark-ok"
        } finally {
            handler.removeCallbacks(workload)
            val removed = CountDownLatch(1)
            handler.post { activity.window.removeOnFrameMetricsAvailableListener(frameListener); removed.countDown() }
            check(removed.await(5L, TimeUnit.SECONDS))
            JankHunter.shutdown()
        }
    }
}
