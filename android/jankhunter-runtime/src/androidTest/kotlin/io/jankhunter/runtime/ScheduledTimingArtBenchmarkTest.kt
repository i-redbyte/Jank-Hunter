package io.jankhunter.runtime

import android.os.Debug
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.io.MetricAggregator
import java.io.File
import java.util.concurrent.Callable
import java.util.concurrent.Delayed
import java.util.concurrent.ScheduledFuture
import java.util.concurrent.Executors
import java.util.concurrent.ScheduledExecutorService
import java.util.concurrent.TimeUnit
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test

/** Isolates wrapper timing/context cost; no writer or real scheduler runs in measured windows. */
class ScheduledTimingArtBenchmarkTest {
    @Test
    fun measureSchedulingAndPeriodicExecution() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val args = InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("jankhunterScheduledBenchmark") == "true")
        val label = requireNotNull(args.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        val file = File(instrumentation.context.filesDir, "scheduled-timing-$label.jsonl")
        file.writeText("")
        for (main in booleanArrayOf(true, false)) {
            val work = Runnable {
                val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1L })
                val config = JankHunterConfig.builder().autoStartCollectors(false).build()
                graph.state.config = config
                graph.state.featureGate.activate(config)
                val delegate = ManualScheduler()
                val executor = JankHunterScheduledExecutorService(delegate, "timing", null, { 1L }, graph.asyncTelemetry)
                var runs = 0
                val command = Runnable { runs++ }
                try {
                    val metrics = MetricAggregator(16, exactAdmission = true)
                    repeat(WARMUP) { metrics.gaugeClassified("benchmark.duration_ms", 1L) }
                    measure(file, label, main, "metric_gauge") { metrics.gaugeClassified("benchmark.duration_ms", 1L) }
                    repeat(WARMUP) { executor.schedule(command, 1L, TimeUnit.SECONDS) }
                    measure(file, label, main, "schedule_once") { executor.schedule(command, 1L, TimeUnit.SECONDS) }
                    val rate = executor.scheduleAtFixedRate(command, 0L, 1L, TimeUnit.MILLISECONDS)
                    val rateTask = checkNotNull(delegate.command)
                    repeat(WARMUP) { rateTask.run() }
                    measure(file, label, main, "fixed_rate_run") { rateTask.run() }
                    rate.cancel(false)
                    val delay = executor.scheduleWithFixedDelay(command, 0L, 1L, TimeUnit.MILLISECONDS)
                    val delayTask = checkNotNull(delegate.command)
                    repeat(WARMUP) { delayTask.run() }
                    measure(file, label, main, "fixed_delay_run") { delayTask.run() }
                    graph.state.featureGate.deactivate()
                    repeat(WARMUP) { delayTask.run() }
                    measure(file, label, main, "inactive_fixed_delay_run") { delayTask.run() }
                    delay.cancel(false)
                    assertEquals(3 * (WARMUP + EVENTS), runs)
                } finally {
                    delegate.shutdownNow()
                }
            }
            if (main) instrumentation.runOnMainSync(work) else work.run()
        }
    }

    private inline fun measure(file: File, label: String, main: Boolean, phase: String, action: () -> Unit) {
        val samples = LongArray(EVENTS)
        val allocation = allocated()
        val cpu = Debug.threadCpuTimeNanos()
        val wall = System.nanoTime()
        repeat(EVENTS) { index ->
            val start = System.nanoTime()
            action()
            samples[index] = System.nanoTime() - start
        }
        val wallNs = System.nanoTime() - wall
        val cpuNs = Debug.threadCpuTimeNanos() - cpu
        val bytes = allocated() - allocation
        samples.sort()
        file.appendText(JSONObject().put("label", label).put("main", main).put("phase", phase)
            .put("events", EVENTS).put("wall_ns", wallNs).put("thread_cpu_ns", cpuNs)
            .put("process_allocated_bytes", bytes).put("p50_ns", samples[EVENTS / 2])
            .put("p95_ns", samples[EVENTS * 95 / 100]).put("p99_ns", samples[EVENTS * 99 / 100]).toString() + "\n")
        if (phase != "schedule_once") assertTrue("periodic execution allocated $bytes bytes", bytes <= 65_536L)
    }

    private class ManualScheduler : ScheduledExecutorService by Executors.newSingleThreadScheduledExecutor() {
        var command: Runnable? = null
        private val future = ManualFuture()
        override fun schedule(command: Runnable, delay: Long, unit: TimeUnit): ScheduledFuture<*> {
            this.command = command
            return future
        }
        override fun scheduleAtFixedRate(command: Runnable, initialDelay: Long, period: Long, unit: TimeUnit): ScheduledFuture<*> =
            schedule(command, initialDelay, unit)
        override fun scheduleWithFixedDelay(command: Runnable, initialDelay: Long, delay: Long, unit: TimeUnit): ScheduledFuture<*> =
            schedule(command, initialDelay, unit)
        override fun <V> schedule(callable: Callable<V>, delay: Long, unit: TimeUnit): ScheduledFuture<V> =
            throw UnsupportedOperationException("not measured")
    }

    private class ManualFuture : ScheduledFuture<Any?> {
        override fun getDelay(unit: TimeUnit): Long = 0L
        override fun compareTo(other: Delayed): Int = 0
        override fun cancel(mayInterruptIfRunning: Boolean): Boolean = true
        override fun isCancelled(): Boolean = false
        override fun isDone(): Boolean = false
        override fun get(): Any? = null
        override fun get(timeout: Long, unit: TimeUnit): Any? = null
    }

    private companion object {
        const val WARMUP = 100_000
        const val EVENTS = 50_000
        fun allocated(): Long = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong()
    }
}
