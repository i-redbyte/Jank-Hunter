package io.jankhunter.runtime

import android.os.Debug
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import java.util.concurrent.Executor
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test

class ExecutorContextArtBenchmarkTest {
    @Test
    fun measureExecutionOnAnUnattributedWorker() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val args = InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("jankhunterExecutorContextBenchmark") == "true")
        val label = requireNotNull(args.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        val file = File(instrumentation.context.filesDir, "executor-context-$label-bare.jsonl")
        file.writeText("")
        for (main in booleanArrayOf(true, false)) {
            val work = Runnable {
                val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1L })
                val config = JankHunterConfig.builder().autoStartCollectors(false).build()
                graph.state.config = config
                graph.state.featureGate.activate(config)
                val delegate = LastTask()
                val executor = JankHunterExecutor(delegate, "context", null, { 1L }, graph.asyncTelemetry)
                var observed: JankHunterContext? = null
                graph.contextTracker.callWithContext(SUBMITTER, null, {}) {
                    executor.execute { observed = graph.contextTracker.capture() }
                }
                val task = checkNotNull(delegate.last)
                repeat(WARMUP) { task.run() }
                measure(file, label, main, "execution") { task.run() }
                assertEquals(JankHunterContext(null, null), graph.contextTracker.capture())
                if (args.getString("jankhunterAssertEnqueueContext") == "true") assertEquals(SUBMITTER, observed)
            }
            if (main) instrumentation.runOnMainSync(work) else work.run()
        }
    }

    @Test
    fun measureEnqueueAndRepeatedExecution() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val args = InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("jankhunterExecutorContextBenchmark") == "true")
        val label = requireNotNull(args.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        val file = File(instrumentation.context.filesDir, "executor-context-$label.jsonl")
        file.writeText("")
        for (main in booleanArrayOf(true, false)) {
            val work = Runnable {
                val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1L })
                val config = JankHunterConfig.builder().autoStartCollectors(false).build()
                graph.state.config = config
                graph.state.featureGate.activate(config)
                val delegate = LastTask()
                val executor = JankHunterExecutor(delegate, "context", null, { 1L }, graph.asyncTelemetry)
                var runs = 0
                var observed: JankHunterContext? = null
                val command = Runnable { observed = graph.contextTracker.capture(); runs++ }
                graph.contextTracker.callWithContext(SUBMITTER, null, {}) {
                    repeat(WARMUP) { executor.execute(command) }
                    measure(file, label, main, "enqueue") { executor.execute(command) }
                }
                val task = checkNotNull(delegate.last)
                graph.contextTracker.callWithContext(WORKER, null, {}) {
                    repeat(WARMUP) { task.run() }
                    measure(file, label, main, "execution") { task.run() }
                    assertEquals(WORKER, graph.contextTracker.capture())
                }
                assertEquals(WARMUP + EVENTS, runs)
                if (args.getString("jankhunterAssertEnqueueContext") == "true") assertEquals(SUBMITTER, observed)
                file.appendText(JSONObject().put("label", label).put("main", main).put("phase", "result")
                    .put("runs", runs).put("screen", observed?.screen).put("owner", observed?.owner)
                    .put("operation_id", observed?.operationId).toString() + "\n")
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
            val started = System.nanoTime()
            action()
            samples[index] = System.nanoTime() - started
        }
        val elapsed = System.nanoTime() - wall
        val cpuNs = Debug.threadCpuTimeNanos() - cpu
        val bytes = allocated() - allocation
        samples.sort()
        file.appendText(JSONObject().put("label", label).put("main", main).put("phase", phase)
            .put("events", EVENTS).put("wall_ns", elapsed).put("thread_cpu_ns", cpuNs)
            .put("process_allocated_bytes", bytes).put("p50_ns", samples[EVENTS / 2])
            .put("p95_ns", samples[EVENTS * 95 / 100]).put("p99_ns", samples[EVENTS * 99 / 100])
            .toString() + "\n")
        if (InstrumentationRegistry.getArguments().getString("jankhunterAssertScopeAllocation") == "true") {
            val budget = if (phase == "enqueue") EVENTS * 32L + 65_536L else 65_536L
            assertTrue("$phase allocated $bytes bytes; budget=$budget", bytes <= budget)
        }
    }

    private class LastTask : Executor {
        @Volatile var last: Runnable? = null
        override fun execute(command: Runnable) { last = command }
    }

    private companion object {
        val SUBMITTER = JankHunterContext("submit-screen", "submit-owner", 42L)
        val WORKER = JankHunterContext("worker-screen", "worker-owner", 7L)
        const val WARMUP = 100_000
        const val EVENTS = 50_000
        fun allocated(): Long = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong()
    }
}
