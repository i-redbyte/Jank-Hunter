package io.jankhunter.runtime

import android.os.Debug
import android.os.Process
import android.util.Log
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.metrics.performance.FrameData
import androidx.metrics.performance.JankStats
import io.jankhunter.runtime.integration.JankHunterJankStats
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.LogQualityCounters
import io.jankhunter.runtime.internal.io.QualityCounterId
import io.jankhunter.runtime.internal.system.RuntimeMaintenanceScheduler
import io.jankhunter.runtime.internal.io.Jhlog
import io.jankhunter.runtime.internal.system.FpsMonitor
import java.io.File
import java.lang.reflect.Proxy
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executor
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference
import java.util.concurrent.atomic.AtomicLong
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith

/** Opt-in, identical workloads for before/after ART runs. Allocation counters are process-wide. */
@RunWith(AndroidJUnit4::class)
class RuntimeHotPathArtBenchmarkTest {
    @Test
    fun measureWriterAndMetrics() {
        assumeTrue(arguments.getString("jankhunterWriterBenchmark") == "true")
        val label = requireNotNull(arguments.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        val output = File(instrumentation.context.filesDir, "jankhunter-writer-$label.jsonl")
        output.writeText("")
        for (onMain in booleanArrayOf(true, false)) {
            val directory = File(instrumentation.targetContext.cacheDir, "art-writer-$label-$onMain")
            val config = JankHunterConfig.builder().autoStartCollectors(false).flushIntervalMs(60_000L)
                .metricAggregationEnabled(true).metricAggregationWindowMs(60_000L).build()
            val writer = AsyncLogWriterFactory().open(directory, config, "instrumentation")
            val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1L })
            val scheduler = RuntimeMaintenanceScheduler()
            graph.state.config = config
            graph.state.writer = writer
            graph.state.maintenanceScheduler = scheduler
            graph.metrics.configure(16, true)
            try {
                var index = 0
                val task = Runnable {
                    record(output, label, measure("writer_main_$onMain", SINGLE_EVENTS, FRAME_WARMUP) {
                        writer.counter("app.writer.benchmark", 1L)
                        if (++index and 255 == 0) assertTrue(writer.flushBlocking(1_000L))
                    })
                    record(output, label, measure("metrics_main_$onMain", SINGLE_EVENTS, FRAME_WARMUP) {
                        graph.metrics.recordCounter("app.metrics.benchmark", 1L)
                    })
                    val before = Snapshot.capture()
                    assertTrue(graph.metrics.flushBlocking(1_000L))
                    record(output, label, summarize("metrics_flush_main_$onMain", emptyArray(), before, Snapshot.capture()))
                }
                if (onMain) instrumentation.runOnMainSync(task) else task.run()
                assertTrue(writer.flushBlocking(5_000L))
                val field = AsyncLogWriter::class.java.getDeclaredField("quality").apply { isAccessible = true }
                val quality = (field.get(writer) as LogQualityCounters).snapshot().associate { it.counterId to it.value }
                val expected = (FRAME_WARMUP + SINGLE_EVENTS + 1L)
                assertEquals(expected, quality[QualityCounterId.ACCEPTED_EVENT_TOTAL])
                assertEquals(expected, quality[QualityCounterId.WRITTEN_EVENT_TOTAL])
                record(output, label, JSONObject().put("workload", "writer_accounting_main_$onMain")
                    .put("accepted", quality[QualityCounterId.ACCEPTED_EVENT_TOTAL])
                    .put("written", quality[QualityCounterId.WRITTEN_EVENT_TOTAL]))
                val beforeIdle = Snapshot.capture()
                Thread.sleep(1_500L)
                record(output, label, summarize("writer_metrics_idle_main_$onMain", emptyArray(), beforeIdle, Snapshot.capture()))
            } finally {
                scheduler.shutdown(1_000L)
                graph.metrics.reset()
                writer.close(5_000L)
                directory.deleteRecursively()
            }
        }
    }

    @Test
    fun measureExecutorDisable() {
        assumeTrue(arguments.getString("jankhunterExecutorBenchmark") == "true")
        val label = requireNotNull(arguments.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        val output = File(instrumentation.context.filesDir, "jankhunter-executor-$label.jsonl")
        output.writeText("")
        val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1L })
        val config = JankHunterConfig.builder().build()
        val delegate = LastTaskExecutor()
        val executor = JankHunterExecutor(delegate, "lifecycle", null, { 1L }, graph.asyncTelemetry)
        var executions = 0
        val command = Runnable { executions++ }
        for (onMain in booleanArrayOf(true, false)) {
            for (active in booleanArrayOf(true, false, true)) {
                if (active) graph.state.featureGate.activate(config) else graph.state.featureGate.deactivate()
                val task = Runnable {
                    record(output, label, measure("executor_main_${onMain}_active_$active", SINGLE_EVENTS, FRAME_WARMUP) {
                        executor.execute(command)
                    })
                    val accepted = checkNotNull(delegate.last)
                    if (!active && arguments.getString("jankhunterAssertInactiveIdentity") == "true") assertTrue(accepted === command)
                    accepted.run()
                }
                if (onMain) instrumentation.runOnMainSync(task) else task.run()
            }
        }
        assertEquals(6, executions)
        val beforeIdle = Snapshot.capture()
        Thread.sleep(1_000L)
        record(output, label, summarize("executor_idle_1s", emptyArray(), beforeIdle, Snapshot.capture()))
    }

    @Test
    fun measureJankStatsListener() {
        assumeTrue(arguments.getString("jankhunterJankStatsBenchmark") == "true")
        val label = requireNotNull(arguments.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        val output = File(instrumentation.context.filesDir, "jankhunter-jankstats-$label.jsonl")
        output.writeText("")
        for (onMain in booleanArrayOf(true, false)) {
            var frames = 0L
            var nanos = 0L
            val listener = checkNotNull(JankHunterJankStats.createFrameListener { _, duration ->
                frames++
                nanos += duration
            }) as JankStats.OnFrameListener
            val frame = FrameData(1L, 16_000_000L, false, emptyList())
            val task = Runnable {
                record(output, label, measure("jankstats_listener_main_$onMain", SINGLE_EVENTS, FRAME_WARMUP) { listener.onFrame(frame) })
            }
            if (onMain) instrumentation.runOnMainSync(task) else task.run()
            assertEquals((FRAME_WARMUP + SINGLE_EVENTS).toLong(), frames)
            assertEquals(frames * 16_000_000L, nanos)
        }
        val beforeIdle = Snapshot.capture()
        Thread.sleep(1_000L)
        record(output, label, summarize("jankstats_idle_1s", emptyArray(), beforeIdle, Snapshot.capture()))
    }

    @Test
    fun measureFrameClockPaths() {
        assumeTrue(arguments.getString("jankhunterFrameBenchmark") == "true")
        val label = requireNotNull(arguments.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        val output = File(instrumentation.context.filesDir, "jankhunter-frames-$label.jsonl")
        output.writeText("")
        val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1L })
        instrumentation.runOnMainSync {
            val monitor = FpsMonitor(Long.MAX_VALUE, 16L, graph.collectorTelemetry, exactAdmission = true)
            monitor.start()
            try {
                monitor.setJankStatsActive(true)
                monitor.setWindowActive(true)
                record(output, label, measure("frames_jankstats", SINGLE_EVENTS, FRAME_WARMUP) {
                    monitor.onJankStatsFrame("art.frames", 16_000_000L, false)
                })
                val listener = checkNotNull(JankHunterJankStats.createFrameListener { jank, duration ->
                    monitor.onJankStatsFrame("art.frames", duration, jank)
                }) as JankStats.OnFrameListener
                val frame = FrameData(1L, 16_000_000L, false, emptyList())
                record(output, label, measure("frames_listener_to_main", SINGLE_EVENTS, FRAME_WARMUP) {
                    listener.onFrame(frame)
                })
            } finally {
                assertTrue(monitor.stop())
            }
        }
        record(output, label, measureQueuedFrames(graph, throughListener = false))
        record(output, label, measureQueuedFrames(graph, throughListener = true))
        val beforeIdle = Snapshot.capture()
        Thread.sleep(1_000L)
        record(output, label, summarize("frames_idle_1s", emptyArray(), beforeIdle, Snapshot.capture()))
    }

    private fun measureQueuedFrames(graph: RuntimeComponentGraph, throughListener: Boolean): JSONObject {
        val emittedFrames = AtomicLong()
        val proxy = Proxy.newProxyInstance(
            RuntimeCollectorCallbacks::class.java.classLoader, arrayOf(RuntimeCollectorCallbacks::class.java),
        ) { _, method, args ->
            if (method.name == "recordUiWindow") emittedFrames.addAndGet(checkNotNull(args)[2] as Long)
            method.invoke(graph.collectorTelemetry, *(args ?: emptyArray()))
        }
        lateinit var monitor: FpsMonitor
        instrumentation.runOnMainSync {
            monitor = FpsMonitor(
                Long.MAX_VALUE, 16L, checkNotNull(RuntimeCollectorCallbacks::class.java.cast(proxy)), exactAdmission = true,
            )
            monitor.start()
            monitor.setJankStatsActive(true)
            monitor.setWindowActive(true)
        }
        val listener = checkNotNull(JankHunterJankStats.createFrameListener { jank, duration ->
            monitor.onJankStatsFrame("art.worker", duration, jank)
        }) as JankStats.OnFrameListener
        val frame = FrameData(1L, 16_000_000L, false, emptyList())
        val publish = if (throughListener) {
            { listener.onFrame(frame) }
        } else {
            { monitor.onJankStatsFrame("art.worker", 16_000_000L, false) }
        }
        try {
            repeat(FRAME_WARMUP) { index ->
                publish()
                if (index and 255 == 255) instrumentation.runOnMainSync {}
            }
            val mainCpu = LongArray(2)
            instrumentation.runOnMainSync { mainCpu[0] = Debug.threadCpuTimeNanos() }
            val samples = LongArray(SINGLE_EVENTS)
            val before = Snapshot.capture()
            repeat(SINGLE_EVENTS) { index ->
                val started = System.nanoTime()
                publish()
                samples[index] = System.nanoTime() - started
                if (index and 255 == 255) instrumentation.runOnMainSync {}
            }
            instrumentation.runOnMainSync { mainCpu[1] = Debug.threadCpuTimeNanos() }
            val after = Snapshot.capture()
            instrumentation.runOnMainSync { assertTrue(monitor.stop()) }
            assertEquals((FRAME_WARMUP + SINGLE_EVENTS).toLong(), emittedFrames.get())
            val name = if (throughListener) "frames_listener_worker_to_main" else "frames_worker_to_main"
            return summarize(name, arrayOf(samples), before, after)
                .put("emitted_frames_including_warmup", emittedFrames.get())
                .put("consumer_main_cpu_ns", mainCpu[1] - mainCpu[0])
        } finally {
            instrumentation.runOnMainSync { monitor.stop() }
        }
    }

    @Test
    fun measureDatabaseTransactions() {
        assumeTrue(arguments.getString("jankhunterDatabaseBenchmark") == "true")
        val label = requireNotNull(arguments.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        val output = File(instrumentation.context.filesDir, "jankhunter-db-$label.jsonl")
        output.writeText("")
        for ((depth, rollback) in listOf(1 to false, 16 to false, 16 to true)) {
            var completions = 0L
            var rollbacks = 0L
            val database = Any()
            val tracker = DatabaseTransactionTracker(
                completionSink = DatabaseTransactionCompletionSink { _, _, _, _, _, outcome, _, _, _, _, _ ->
                    completions++
                    if (outcome == Jhlog.DATABASE_TRANSACTION_ROLLBACK) rollbacks++
                },
                nanoTime = System::nanoTime,
            )
            instrumentation.runOnMainSync {
                val result = measure("database_depth_${depth}_rollback_$rollback", SINGLE_EVENTS) {
                    repeat(depth) { tracker.begin(database, 1L, "art.database", Jhlog.DATABASE_TRANSACTION_EXCLUSIVE) }
                    repeat(depth) { index ->
                        if (!rollback || index > 0) tracker.markSuccessful(database)
                        tracker.finish(database, DatabaseFailureKind.OTHER, failed = false)
                    }
                }
                assertEquals((WARMUP + SINGLE_EVENTS).toLong() * depth, completions)
                record(output, label, result.put("completions_including_warmup", completions)
                    .put("rollbacks_including_warmup", rollbacks))
            }
        }
        val beforeIdle = Snapshot.capture()
        Thread.sleep(1_000L)
        record(output, label, summarize("database_idle_1s", emptyArray(), beforeIdle, Snapshot.capture()))
    }

    @Test
    fun measureHotPathsAndIdleConsumer() {
        assumeTrue(arguments.getString("jankhunterBenchmark") == "true")
        val label = requireNotNull(arguments.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        val output = File(instrumentation.context.filesDir, "jankhunter-art-$label.jsonl")
        output.writeText("")
        val delegate = LastTaskExecutor()
        val executor = JankHunterExecutor(delegate, "art", null, { 1L }, activeExecutorTestCallbacks())
        var executions = 0
        val command = Runnable { executions++ }
        instrumentation.runOnMainSync {
            record(output, label, measure("executor_main", SINGLE_EVENTS) { executor.execute(command) })
            delegate.last?.run()
            assertEquals(1, executions)
            val sql = "SELECT * FROM account WHERE token=\"private-value\" AND id=123"
            record(output, label, measure("sql_main", SINGLE_EVENTS) { sqlSink = RuntimeSqlNormalizer.normalize(sql) })
            assertTrue(requireNotNull(sqlSink).startsWith("SELECT * FROM account WHERE token="))
        }
        for (count in intArrayOf(1, 8, 32)) measureGraph(output, label, count)
        measureHooks(output, label)
    }

    private fun measureGraph(output: File, label: String, producers: Int) {
        val directory = File(instrumentation.targetContext.cacheDir, "art-graph-$label-$producers")
        val writer = AsyncLogWriterFactory().open(directory, config(), "instrumentation")
        val consumerTid = AtomicInteger()
        val graph = RuntimeCallGraph(
            nowMs = { System.nanoTime() / 1_000_000L }, captureScreen = { "ArtScreen" },
            captureOperationId = { 1L }, maxKeys = { 4_096 },
            consumerLoopObserver = { consumerTid.compareAndSet(0, Process.myTid()) },
        )
        graph.resetFlushState(writer)
        val warmed = CountDownLatch(producers)
        val start = CountDownLatch(1)
        val done = CountDownLatch(producers)
        val failure = AtomicReference<Throwable>()
        val samples = Array(producers) { LongArray(GRAPH_EVENTS) }
        val threads = List(producers) { producer ->
            Thread({
                try {
                    repeat(WARMUP) { edge(graph, producer, it) }
                    warmed.countDown()
                    check(start.await(30L, TimeUnit.SECONDS))
                    repeat(GRAPH_EVENTS) { event ->
                        val before = System.nanoTime()
                        edge(graph, producer, event)
                        samples[producer][event] = System.nanoTime() - before
                    }
                } catch (error: Throwable) {
                    failure.compareAndSet(null, error)
                    warmed.countDown()
                } finally {
                    done.countDown()
                }
            }, "JHArt-$producer")
        }
        try {
            threads.forEach(Thread::start)
            assertTrue(warmed.await(30L, TimeUnit.SECONDS))
            failure.get()?.let { throw AssertionError(it) }
            assertTrue(graph.flushBlocking(5_000L))
            val before = Snapshot.capture()
            start.countDown()
            assertTrue(done.await(30L, TimeUnit.SECONDS))
            val after = Snapshot.capture()
            threads.forEach { it.join(1_000L) }
            failure.get()?.let { throw AssertionError(it) }
            assertTrue(graph.flushBlocking(5_000L))
            val attempted = producers.toLong() * (WARMUP + GRAPH_EVENTS)
            assertEquals(attempted, graph.attemptedForTest())
            assertEquals(graph.acceptedForTest(), graph.emittedForTest() + graph.acceptedEventLossForTest())
            val result = summarize("graph_$producers", samples, before, after)
                .put("attempted_including_warmup", attempted)
                .put("emitted_including_warmup", graph.emittedForTest())
                .put("pre_admission_loss", graph.producerCapacityLossForTest())
                .put("accepted_loss", graph.acceptedEventLossForTest())
            record(output, label, result)
            if (producers == 1) {
                val switchesBefore = voluntarySwitches(consumerTid.get())
                val idleBefore = Snapshot.capture()
                Thread.sleep(1_000L)
                val idleAfter = Snapshot.capture()
                record(output, label, summarize("graph_idle_1s", emptyArray(), idleBefore, idleAfter)
                    .put("consumer_voluntary_switches", voluntarySwitches(consumerTid.get()) - switchesBefore))
            }
        } finally {
            start.countDown()
            threads.forEach { it.join(2_000L) }
            graph.flushForShutdown(5_000L)
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    private fun measureHooks(output: File, label: String) {
        val directory = File(instrumentation.targetContext.cacheDir, "art-hooks-$label")
        val writer = AsyncLogWriterFactory().open(directory, config(), "instrumentation")
        val components = RuntimeComponentGraph(
            nowMs = { System.nanoTime() / 1_000_000L }, nowUs = { System.nanoTime() / 1_000L },
        )
        components.state.config = config()
        components.state.writer = writer
        val transport = components.runtimeHookEvents
        transport.start(writer)
        try {
            instrumentation.runOnMainSync {
                record(output, label, measure("hook_method_main", SINGLE_EVENTS) {
                    transport.recordMethod(1L, "art.Method")
                })
            }
            assertTrue(transport.flushBlocking(5_000L))
            assertEquals(transport.acceptedForTest(), transport.emittedForTest() + transport.acceptedLossForTest())
            record(output, label, JSONObject().put("workload", "hook_accounting")
                .put("attempted", transport.attemptedForTest()).put("accepted", transport.acceptedForTest())
                .put("emitted", transport.emittedForTest()).put("accepted_loss", transport.acceptedLossForTest()))
        } finally {
            transport.stopAndFlush(5_000L)
            transport.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    private fun measure(name: String, events: Int, warmup: Int = WARMUP, action: () -> Unit): JSONObject {
        repeat(warmup) { action() }
        val durations = LongArray(events)
        val before = Snapshot.capture()
        repeat(events) { event ->
            val start = System.nanoTime()
            action()
            durations[event] = System.nanoTime() - start
        }
        val after = Snapshot.capture()
        return summarize(name, arrayOf(durations), before, after)
    }

    private fun summarize(name: String, samples: Array<LongArray>, before: Snapshot, after: Snapshot): JSONObject {
        val durations = LongArray(samples.sumOf { it.size })
        var offset = 0
        for (sample in samples) { sample.copyInto(durations, offset); offset += sample.size }
        durations.sort()
        val result = JSONObject().put("workload", name).put("events", durations.size)
            .put("wall_ns", after.wallNs - before.wallNs).put("process_cpu_ms", after.cpuMs - before.cpuMs)
            .put("caller_thread_cpu_ns", after.threadCpuNs - before.threadCpuNs)
            .put("process_allocated_bytes", after.allocated - before.allocated)
            .put("gc_count", after.gcCount - before.gcCount).put("gc_time_ms", after.gcTimeMs - before.gcTimeMs)
            .put("rss_kb", rssKb())
        if (durations.isNotEmpty()) {
            for ((key, fraction) in listOf("p50_ns" to .50, "p95_ns" to .95, "p99_ns" to .99, "p999_ns" to .999)) {
                result.put(key, durations[((durations.size - 1) * fraction).toInt()])
            }
        }
        return result
    }

    private fun record(output: File, label: String, value: JSONObject) {
        value.put("label", label).put("sdk", android.os.Build.VERSION.SDK_INT)
            .put("abi", android.os.Build.SUPPORTED_ABIS.first())
        val line = value.toString()
        output.appendText(line + "\n")
        Log.i("JHArtBenchmark", line)
    }

    private fun edge(graph: RuntimeCallGraph, producer: Int, event: Int) {
        val parentId = producer.toLong() + 1L
        val childId = 1_000L + (event and 63)
        val parent = graph.enter(parentId, "art.Parent", true)
        val child = graph.enter(childId, "art.Child", true)
        graph.exit(child, childId)
        graph.exit(parent, parentId)
    }

    private fun config() = JankHunterConfig.builder().autoStartCollectors(false).flushIntervalMs(60_000L).build()

    private fun voluntarySwitches(tid: Int): Long = File("/proc/self/task/$tid/status").readLines()
        .single { it.startsWith("voluntary_ctxt_switches:") }.substringAfter(':').trim().toLong()

    private fun rssKb(): Long = File("/proc/self/status").readLines()
        .single { it.startsWith("VmRSS:") }.substringAfter(':').trim().substringBefore(' ').toLong()

    private data class Snapshot(
        val wallNs: Long, val cpuMs: Long, val allocated: Long, val gcCount: Long, val gcTimeMs: Long, val threadCpuNs: Long,
    ) {
        companion object {
            fun capture() = Snapshot(System.nanoTime(), Process.getElapsedCpuTime(),
                stat("art.gc.bytes-allocated"), stat("art.gc.gc-count"), stat("art.gc.gc-time"), Debug.threadCpuTimeNanos())
            private fun stat(name: String): Long = requireNotNull(Debug.getRuntimeStat(name)).toLong()
        }
    }

    private class LastTaskExecutor : Executor {
        @Volatile var last: Runnable? = null
        override fun execute(command: Runnable) { last = command }
    }

    private val instrumentation get() = InstrumentationRegistry.getInstrumentation()
    private val arguments get() = InstrumentationRegistry.getArguments()

    private companion object {
        const val WARMUP = 10_000
        const val FRAME_WARMUP = 200_000
        const val SINGLE_EVENTS = 50_000
        const val GRAPH_EVENTS = 20_000
        @Volatile var sqlSink: String? = null
    }
}
