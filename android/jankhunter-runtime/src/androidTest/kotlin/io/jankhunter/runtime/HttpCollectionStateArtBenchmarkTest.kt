package io.jankhunter.runtime

import android.os.Debug
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.io.BinaryEncodingSink
import io.jankhunter.runtime.internal.io.BinaryPayload
import io.jankhunter.runtime.internal.io.BinaryRecordContext
import io.jankhunter.runtime.internal.io.SessionBinaryRecordEncoder
import java.io.File
import org.json.JSONObject
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test

class HttpCollectionStateArtBenchmarkTest {
    @Test
    fun measureSessionFlagSelectionAndEncoding() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val args = InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("jankhunterHttpStateBenchmark") == "true")
        val label = requireNotNull(args.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        val output = File(instrumentation.context.filesDir, "http-state-$label.jsonl")
        output.writeText("")
        val session = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1_000L }).session
        val flags = RuntimeSessionController::class.java.getDeclaredMethod("collectorFlags", JankHunterConfig::class.java)
            .apply { isAccessible = true }
        instrumentation.runOnMainSync {
            for (enabled in booleanArrayOf(false, true)) {
                val config = JankHunterConfig.builder().runtimeFeatureEnabled(JankHunterRuntimeFeature.HTTP, enabled).build()
                val sink = CountingSink()
                val encoder = SessionBinaryRecordEncoder(sink)
                val action = {
                    val value = flags.invoke(session, config) as Long
                    encoder.session(null, null, null, 35, null, null, null, null, null, null, null, null, null,
                        false, value, true)
                }
                repeat(5_000) { action() }
                val samples = LongArray(20_000)
                val allocated = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong()
                val cpu = Debug.threadCpuTimeNanos()
                repeat(samples.size) { index ->
                    val before = System.nanoTime()
                    action()
                    samples[index] = System.nanoTime() - before
                }
                val cpuNs = Debug.threadCpuTimeNanos() - cpu
                val bytes = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong() - allocated
                samples.sort()
                output.appendText(JSONObject().put("label", label).put("enabled", enabled)
                    .put("events", samples.size).put("thread_cpu_ns", cpuNs).put("allocated_bytes", bytes)
                    .put("p50_ns", samples[samples.size / 2]).put("p95_ns", samples[samples.size * 95 / 100])
                    .put("encoded_bytes", sink.bytes).toString() + "\n")
                assertTrue(sink.bytes > 0L)
            }
        }
    }

    @Test
    fun measureCompleteCollectionLifecycle() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val args = InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("jankhunterHttpStateBenchmark") == "true")
        val label = requireNotNull(args.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        val output = File(instrumentation.context.filesDir, "http-state-$label-lifecycle.jsonl")
        val directory = File(instrumentation.context.cacheDir, "http-lifecycle-benchmark")
        val config = JankHunterConfig.builder().logDirectory(directory).autoStartCollectors(false)
            .runtimeCallGraphEnabled(false).metricAggregationEnabled(false).build()
        instrumentation.runOnMainSync {
            val samples = LongArray(30)
            var allocated = 0L
            var cpuNs = 0L
            repeat(samples.size + 5) { index ->
                directory.deleteRecursively()
                val graph = RuntimeComponentGraph(android.os.SystemClock::elapsedRealtime,
                    { android.os.SystemClock.elapsedRealtimeNanos() / 1_000L })
                val beforeBytes = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong()
                val beforeCpu = Debug.threadCpuTimeNanos()
                val before = System.nanoTime()
                try {
                    graph.lifecycle.init(instrumentation.targetContext, config)
                } finally {
                    graph.lifecycle.shutdown()
                }
                val duration = System.nanoTime() - before
                val cpu = Debug.threadCpuTimeNanos() - beforeCpu
                val bytes = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong() - beforeBytes
                if (index >= 5) {
                    samples[index - 5] = duration
                    allocated += bytes
                    cpuNs += cpu
                }
            }
            samples.sort()
            output.writeText(JSONObject().put("label", label).put("sessions", samples.size)
                .put("thread_cpu_ns", cpuNs).put("allocated_bytes", allocated)
                .put("p50_ns", samples[samples.size / 2]).put("p95_ns", samples[samples.size * 95 / 100])
                .toString() + "\n")
        }
        directory.deleteRecursively()
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
        override fun emitSemantic(recordType: Int, attributes: Long, payload: BinaryPayload,
            context: BinaryRecordContext?, semanticEventCount: Long) { bytes += payload.size }
    }
}
