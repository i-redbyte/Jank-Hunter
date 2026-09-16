package io.jankhunter.runtime

import android.os.SystemClock
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Test

class HttpCollectionStateWireArtTest {
    @Test
    fun effectiveHttpStateSurvivesRealSessionWriter() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        for (enabled in booleanArrayOf(false, true)) {
            val directory = File(instrumentation.context.cacheDir, "http-state-$enabled")
            directory.deleteRecursively()
            val graph = RuntimeComponentGraph(SystemClock::elapsedRealtime, { SystemClock.elapsedRealtimeNanos() / 1000L })
            val config = JankHunterConfig.builder().logDirectory(directory).autoStartCollectors(false)
                .runtimeCallGraphEnabled(false).metricAggregationEnabled(false)
                .runtimeFeatureEnabled(JankHunterRuntimeFeature.HTTP, enabled).build()
            try {
                instrumentation.runOnMainSync { graph.lifecycle.init(instrumentation.targetContext, config) }
                // A quiet session must still have a final collection timestamp.
                SystemClock.sleep(2_100L)
            } finally {
                instrumentation.runOnMainSync { graph.lifecycle.shutdown() }
            }
            val logs = directory.walkTopDown().filter { it.isFile && it.extension == "jhlog" }.toList()
            assertEquals(1, logs.size)
            logs.single().copyTo(File(instrumentation.context.filesDir, "http-state-$enabled-5.1.0.jhlog"), overwrite = true)
        }
    }
}
