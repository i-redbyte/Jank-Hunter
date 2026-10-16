package io.jankhunter.sample

import android.content.Intent
import android.os.Handler
import android.os.HandlerThread
import android.view.FrameMetrics
import android.view.Window
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.sample.concurrency.ConcurrencyBoundaryBenchmark
import io.jankhunter.runtime.JankHunterExecutionObservationMode
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import org.json.JSONObject
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class ConcurrencyBoundaryBenchmarkTest {
    @Test fun recordsMeasuredNormalAndDetailedOverhead() {
        val arguments = InstrumentationRegistry.getArguments()
        val seconds = arguments.getString("benchmark_seconds")?.toInt() ?: 30
        val observation = JankHunterExecutionObservationMode.valueOf(
            arguments.getString("benchmark_observation_mode") ?: "WRAPPERS")
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val context = instrumentation.targetContext
        if (arguments.getString("benchmark_frames") != "true") {
            assertTrue(ConcurrencyBoundaryBenchmark(context, observationMode = observation).run(seconds).length() > 0L)
            return
        }
        val activity = instrumentation.startActivitySync(Intent().setClassName(context,
            "io.jankhunter.sample.concurrency.ConcurrencyFrameProbeActivity").addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        val recorder = FrameRecorder()
        instrumentation.runOnMainSync { activity.window.addOnFrameMetricsAvailableListener(recorder, recorder.handler) }
        try {
            val artifact = ConcurrencyBoundaryBenchmark(context, recorder::reset, recorder::snapshot, observation).run(seconds)
            assertTrue(artifact.length() > 0L)
        } finally {
            instrumentation.runOnMainSync {
                activity.window.removeOnFrameMetricsAvailableListener(recorder)
                activity.finish()
            }
            recorder.close()
        }
    }

    private class FrameRecorder : Window.OnFrameMetricsAvailableListener {
        private val thread = HandlerThread("sample-frame-measurements").apply { start() }
        val handler = Handler(thread.looper)
        private val samples = LongArray(20_000)
        private var count = 0L
        private var dropped = 0L

        override fun onFrameMetricsAvailable(window: Window, metrics: FrameMetrics, dropCountSinceLastInvocation: Int) {
            val total = metrics.getMetric(FrameMetrics.TOTAL_DURATION)
            if (total >= 0L) {
                samples[(count % samples.size).toInt()] = total
                count++
            }
            dropped += dropCountSinceLastInvocation.coerceAtLeast(0)
        }

        fun reset() = onRecorderThread { count = 0L; dropped = 0L }

        fun snapshot(): JSONObject {
            var result = JSONObject()
            onRecorderThread {
                check(count > 0L) { "animated window produced no FrameMetrics" }
                val measured = samples.copyOf(minOf(count, samples.size.toLong()).toInt())
                java.util.Arrays.sort(measured)
                result = JSONObject().put("count", count).put("dropped_callbacks", dropped)
                    .put("total_duration_p50_ns", measured[measured.lastIndex / 2])
                    .put("total_duration_p95_ns", measured[measured.lastIndex * 95 / 100])
                    .put("samples", measured.size)
                    .put("scope", "Window FrameMetrics TOTAL_DURATION; last at most 20000 frames; not input latency")
            }
            return result
        }

        fun close() {
            thread.quitSafely()
            thread.join(5_000L)
            check(!thread.isAlive)
        }

        private fun onRecorderThread(action: () -> Unit) {
            val completed = CountDownLatch(1)
            var failure: Throwable? = null
            check(handler.post {
                try { action() } catch (error: Throwable) { failure = error } finally { completed.countDown() }
            })
            check(completed.await(5, TimeUnit.SECONDS))
            failure?.let { throw it }
        }
    }
}
