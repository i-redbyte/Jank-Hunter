package io.jankhunter.runtime

import android.os.Debug
import android.os.Process
import android.os.SystemClock
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.io.BinaryEncodingSink
import io.jankhunter.runtime.internal.io.BinaryPayload
import io.jankhunter.runtime.internal.io.BinaryRecordContext
import io.jankhunter.runtime.internal.io.SessionBinaryRecordEncoder
import io.jankhunter.runtime.internal.system.AdaptiveRuntimeSampler
import java.io.File
import org.json.JSONObject
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test

class UidTrafficArtBenchmarkTest {
    @Test
    fun measureCollectorSamplingAndEncoding() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val args = InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("jankhunterUidTrafficBenchmark") == "true")
        val label = requireNotNull(args.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        val output = File(instrumentation.context.filesDir, "uid-traffic-$label.jsonl")
        output.writeText("")
        for (main in booleanArrayOf(true, false)) {
            val work = Runnable {
                val sink = CountingSink()
                val encoder = SessionBinaryRecordEncoder(sink)
                val adaptive = AdaptiveRuntimeSampler(60_000L, 60_000L)
                val uid = Process.myUid().toLong() + 1L
                val encode = {
                    encoder.deviceContext(0, 50, 100L, 0, 0, false, false, false, 100L, 100L, 1000L, 0L, 0L, false, true,
                        trafficUidPlusOne = uid, trafficKnownFlags = 3)
                }
                val sample = {
                    adaptive.shouldRecordContext(1L, 0, 50, 100L, false, false, false, 100L, 100L, false,
                        trafficUidPlusOne = uid, trafficKnownFlags = 3)
                    Unit
                }
                repeat(WARMUP) { encode(); sample() }
                measure(output, label, main, "encoder", encode)
                measure(output, label, main, "adaptive_unchanged", sample)
                assertTrue(sink.bytes > 0L)
            }
            if (main) instrumentation.runOnMainSync(work) else work.run()
        }
    }

    @Test
    fun measureRealContextPipeline() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val args = InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("jankhunterUidTrafficBenchmark") == "true")
        val label = requireNotNull(args.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        val output = File(instrumentation.context.filesDir, "uid-traffic-$label-pipeline.jsonl")
        output.writeText("")
        for (main in booleanArrayOf(true, false)) {
            val directory = File(instrumentation.context.cacheDir, "uid-traffic-$label-$main")
            directory.deleteRecursively()
            val graph = RuntimeComponentGraph(SystemClock::elapsedRealtime, { SystemClock.elapsedRealtimeNanos() / 1000L })
            val config = JankHunterConfig.builder().logDirectory(directory).autoStartCollectors(false)
                .runtimeCallGraphEnabled(false).metricAggregationEnabled(false).flushIntervalMs(60_000L).build()
            try {
                instrumentation.runOnMainSync { graph.lifecycle.init(instrumentation.targetContext, config) }
                val uid = Process.myUid().toLong() + 1L
                var counter = 0L
                val action = {
                    graph.systemTelemetry.recordContext(0, 50, 100L, 0, 0, false, false, false,
                        counter, counter, 1000L, 0L, 0L, false, trafficUidPlusOne = uid, trafficKnownFlags = 3)
                    counter++
                    if (counter % 128L == 0L) assertTrue(checkNotNull(graph.writer).flushBlocking(5_000L))
                }
                val work = Runnable {
                    repeat(WARMUP) { action() }
                    assertTrue(checkNotNull(graph.writer).flushBlocking(5_000L))
                    measure(output, label, main, "async_context", action)
                    assertTrue(checkNotNull(graph.writer).flushBlocking(5_000L))
                }
                if (main) instrumentation.runOnMainSync(work) else work.run()
            } finally {
                instrumentation.runOnMainSync { graph.lifecycle.shutdown() }
            }
            val logs = directory.walkTopDown().filter { it.isFile && it.extension == "jhlog" }.toList()
            check(logs.size == 1)
            logs.single().copyTo(File(instrumentation.context.filesDir, "uid-traffic-$label-$main.jhlog"), overwrite = true)
        }
    }

    private fun measure(output: File, label: String, main: Boolean, phase: String, action: () -> Unit) {
        val samples = LongArray(EVENTS)
        val allocation = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong()
        val processCpu = Process.getElapsedCpuTime()
        val cpu = Debug.threadCpuTimeNanos()
        val started = System.nanoTime()
        repeat(EVENTS) { index ->
            val before = System.nanoTime()
            action()
            samples[index] = System.nanoTime() - before
        }
        val wall = System.nanoTime() - started
        val cpuNs = Debug.threadCpuTimeNanos() - cpu
        val processCpuMs = Process.getElapsedCpuTime() - processCpu
        val allocated = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong() - allocation
        samples.sort()
        output.appendText(JSONObject().put("label", label).put("main", main).put("phase", phase)
            .put("events", EVENTS).put("wall_ns", wall).put("thread_cpu_ns", cpuNs)
            .put("process_cpu_ms", processCpuMs).put("process_allocated_bytes", allocated)
            .put("p50_ns", samples[EVENTS / 2]).put("p95_ns", samples[EVENTS * 95 / 100])
            .put("p99_ns", samples[EVENTS * 99 / 100]).toString() + "\n")
    }

    private companion object {
        const val EVENTS = 20_000
        const val WARMUP = 5_000
    }

    private class CountingSink : BinaryEncodingSink {
        var bytes = 0L
        private val payload = BinaryPayload()

        override fun payload(): BinaryPayload = payload.clear()

        override fun optionalSymbolId(kind: Int, value: String?): Long = 0L

        override fun defineStableSymbol(id: Long, name: String?): Long = id

        override fun producerContext(owner: String?): BinaryRecordContext? = null

        override fun emitDictionaryDefinition(payload: BinaryPayload) = Unit

        override fun emitControl(recordType: Int, payload: BinaryPayload) = Unit

        override fun emitSemantic(
            recordType: Int,
            attributes: Long,
            payload: BinaryPayload,
            context: BinaryRecordContext?,
            semanticEventCount: Long,
        ) {
            bytes += payload.size
        }
    }
}
