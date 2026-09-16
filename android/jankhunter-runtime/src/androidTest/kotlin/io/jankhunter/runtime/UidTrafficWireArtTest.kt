package io.jankhunter.runtime

import android.os.Process
import android.os.SystemClock
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.system.SystemContextSampler
import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class UidTrafficWireArtTest {
    @Test
    fun systemAndManualContextPreserveTrafficProvenanceThroughAsyncWriter() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val directory = File(instrumentation.context.cacheDir, "uid-traffic-wire")
        directory.deleteRecursively()
        val graph = RuntimeComponentGraph(SystemClock::elapsedRealtime, { SystemClock.elapsedRealtimeNanos() / 1000L })
        val config = JankHunterConfig.builder().logDirectory(directory).autoStartCollectors(false)
            .runtimeCallGraphEnabled(false).metricAggregationEnabled(false).build()
        try {
            instrumentation.runOnMainSync { graph.lifecycle.init(instrumentation.targetContext, config) }
            val sampler = SystemContextSampler(instrumentation.targetContext, 1000L, graph.collectorTelemetry)
            val sample = sampler.javaClass.getDeclaredMethod("sampleOnce").apply { isAccessible = true }
            sample.invoke(sampler)
            sample.invoke(sampler)
            for (flags in 0..3) {
                graph.collectorTelemetry.recordContext(
                    0, 50, 100L, 0, 0, false, false, false, 0L, 0L, 1000L, 0L, 0L, false,
                    trafficUidPlusOne = Process.myUid().toLong() + 1L, trafficKnownFlags = flags,
                )
            }
            graph.systemTelemetry.recordContext(
                0, 50, 100L, 0, 0, false, false, false, 0L, 0L, 1000L, 0L, 0L, false,
            )
            assertTrue(checkNotNull(graph.writer).flushBlocking(5_000L))
        } finally {
            instrumentation.runOnMainSync { graph.lifecycle.shutdown() }
        }
        val logs = directory.walkTopDown().filter { it.isFile && it.extension == "jhlog" }.toList()
        assertEquals(1, logs.size)
        logs.single().copyTo(File(instrumentation.context.filesDir, "uid-traffic-5.1.0.jhlog"), overwrite = true)
    }
}
