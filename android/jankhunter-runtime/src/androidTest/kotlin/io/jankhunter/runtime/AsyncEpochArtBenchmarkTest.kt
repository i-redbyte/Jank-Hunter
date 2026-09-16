package io.jankhunter.runtime

import android.os.Debug
import android.os.Process
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.Jhlog
import io.jankhunter.runtime.internal.io.QualityCounterId
import java.io.File
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test

/** Same service calls and real writer on both revisions; setup and flushing are outside pXX samples. */
class AsyncEpochArtBenchmarkTest {
    @Test
    fun measureDatabaseWorkerAndTransactionObservation() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val args = InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("jankhunterAsyncEpochBenchmark") == "true")
        val label = requireNotNull(args.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        val output = File(instrumentation.context.filesDir, "async-epoch-$label.jsonl")
        output.writeText("")
        for (main in booleanArrayOf(true, false)) {
            for (phase in listOf("database", "worker", "manual_database", "transaction", "inactive")) {
                val root = File(instrumentation.context.cacheDir, "async-epoch-bench-$main-$phase")
                root.deleteRecursively()
                val config = JankHunterConfig.builder().autoStartCollectors(false).runtimeCallGraphEnabled(false)
                    .metricAggregationEnabled(false).sessionLogSizeLimitEnabled(false).build()
                val writer = AsyncLogWriterFactory().open(root, config, "main")
                val graph = RuntimeComponentGraph({ 1L }, { 1_000L })
                graph.state.bindCurrentConfiguration(config)
                graph.state.writer = writer
                graph.state.runtimeEnabled.set(true)
                graph.coordinator.markStarted(config)
                if (phase == "inactive") graph.state.featureGate.deactivate()
                val database = Any()
                val action = Runnable {
                    when (phase) {
                        "database", "inactive" -> query(graph)
                        "worker" -> {
                            val token = graph.workerTelemetry.started(11L, 101L, "BenchmarkWorker", 0, 0)
                            graph.workerTelemetry.finished(token, 11L, 101L, "BenchmarkWorker",
                                JankHunterWorkerOutcome.SUCCESS, 0, 0, 0, false)
                        }
                        "manual_database" -> {
                            val token = graph.manualDatabaseTracing.beginCall("benchmark.manual", "SELECT 1",
                                JankHunterDatabaseOperation.QUERY, JankHunterDatabaseBoundary.EXECUTE)
                            graph.manualDatabaseTracing.endCall(token, null, 0L, null)
                        }
                        "transaction" -> {
                            graph.databaseTelemetry.beginTransaction(database, 201L, "BenchmarkTransaction", 1)
                            graph.databaseTelemetry.markTransactionSuccessful(database)
                            graph.databaseTelemetry.endTransaction(database, null)
                        }
                    }
                }
                val samples = LongArray(EVENTS)
                val execute = { task: Runnable -> if (main) instrumentation.runOnMainSync(task) else task.run() }
                try {
                    execute(Runnable {
                        repeat(WARMUP) { index ->
                            action.run()
                            if ((index + 1) % BATCH == 0) assertTrue(writer.flushBlocking(5_000L))
                        }
                    })
                    assertTrue(writer.flushBlocking(5_000L))
                    val acceptedBefore = generationQuality(writer, QualityCounterId.ACCEPTED_EVENT_TOTAL)
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
                            assertTrue(writer.flushBlocking(5_000L))
                        }
                    })
                    assertTrue(writer.terminalFailureCause()?.toString(), writer.flushBlocking(5_000L))
                    val wall = System.nanoTime() - wallBefore
                    val cpu = Process.getElapsedCpuTime() - processBefore
                    val bytes = allocated() - allocatedBefore
                    val accepted = generationQuality(writer, QualityCounterId.ACCEPTED_EVENT_TOTAL) - acceptedBefore
                    val perOperation = when (phase) { "inactive" -> 0L; "worker", "transaction" -> 2L; else -> 1L }
                    assertEquals(EVENTS * perOperation, accepted)
                    assertEquals(0L, generationQuality(writer, QualityCounterId.WRITER_IO_ERROR_TOTAL))
                    assertEquals(generationQuality(writer, QualityCounterId.ACCEPTED_EVENT_TOTAL),
                        generationQuality(writer, QualityCounterId.WRITTEN_EVENT_TOTAL))
                    samples.sort()
                    output.appendText(JSONObject().put("label", label).put("main", main).put("phase", phase)
                        .put("operations", EVENTS).put("accepted", accepted).put("thread_cpu_ns", threadCPU)
                        .put("process_cpu_ms", cpu).put("process_allocated_bytes", bytes).put("complete_wall_ns", wall)
                        .put("p50_ns", samples[EVENTS / 2]).put("p95_ns", samples[EVENTS * 95 / 100])
                        .put("p99_ns", samples[EVENTS * 99 / 100]).toString() + "\n")
                } finally {
                    graph.session.stop(clearInit = true)
                    writer.close()
                    root.deleteRecursively()
                }
            }
        }
    }

    private fun query(graph: RuntimeComponentGraph) {
        val token = graph.databaseTelemetry.enter()
        graph.databaseTelemetry.exit(token, 301L, "BenchmarkQuery", "SELECT ?", 1L,
            Jhlog.DATABASE_FRAMEWORK_SQLITE.toInt(), Jhlog.DATABASE_OPERATION_QUERY.toInt(),
            Jhlog.DATABASE_BOUNDARY_EXECUTE.toInt(), false, 0, 0, 0L, true, null)
    }

    private companion object {
        const val BATCH = 64
        const val WARMUP = 2_000
        const val EVENTS = 5_000
        fun allocated(): Long = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong()
    }
}
