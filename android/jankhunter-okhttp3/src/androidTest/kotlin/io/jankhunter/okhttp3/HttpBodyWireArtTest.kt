package io.jankhunter.okhttp3

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterConfig
import io.jankhunter.runtime.JankHunterNetworkRuntime
import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class HttpBodyWireArtTest {
    @Test
    fun realHttpExchangesReachTheRuntimeWriter() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val context = instrumentation.targetContext.applicationContext
        val directory = File(instrumentation.context.filesDir, "http-body-wire")
        JankHunter.shutdown()
        directory.deleteRecursively()
        val config = JankHunterConfig.builder().logDirectory(directory).autoStartCollectors(false)
            .runtimeCallGraphEnabled(false).build()
        try {
            instrumentation.runOnMainSync { JankHunter.init(context, config) }
            assertTrue("HTTP runtime did not start", JankHunterNetworkRuntime.isHttpActive())
            val exchanges = HttpBodyExchangeIntegrationTest()
            for (authenticate in booleanArrayOf(true, false)) {
                val event = exchanges.exchange(authenticate).event
                assertTrue("real HTTP event lacks body total marker", event.flags and (1L shl 23) != 0L)
                JankHunterNetworkRuntime.recordHttp(event)
            }
            JankHunter.flush()
        } finally {
            instrumentation.runOnMainSync { JankHunter.shutdown() }
        }
        val files = directory.walkTopDown().filter { it.isFile && it.extension == "jhlog" }.toList()
        assertEquals("fixture must contain one finalized runtime segment", 1, files.size)
        files.single().copyTo(File(instrumentation.context.filesDir, "http-body-totals-5.1.0.jhlog"), overwrite = true)
    }
}
