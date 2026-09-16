package io.jankhunter.okhttp3

import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterConfig
import java.io.File
import okhttp3.OkHttpClient
import okhttp3.Request
import org.junit.Assert.assertEquals
import org.junit.Test

class HttpFirstByteWireArtTest {
    @Test
    fun realFirstByteAndUnknownObservationReachTheRuntimeWriter() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val directory = File(instrumentation.context.filesDir, "http-first-byte-wire")
        instrumentation.runOnMainSync { JankHunter.shutdown() }
        directory.deleteRecursively()
        val config = JankHunterConfig.builder().logDirectory(directory).autoStartCollectors(false)
            .runtimeCallGraphEnabled(false).build()
        try {
            instrumentation.runOnMainSync { JankHunter.init(instrumentation.targetContext, config) }
            val transport = HttpFirstByteIntegrationTest()
            assertEquals(500L, transport.exchange("x", RuntimeNetworkTelemetry.INSTANCE).ttfbMs)
            assertEquals(0L, transport.exchange("x", RuntimeNetworkTelemetry.INSTANCE, 100L).ttfbMs)
            // Listener-only integration: the headers-read callback cannot establish a first byte.
            val client = OkHttpClient()
            val call = client.newCall(Request.Builder().url("http://localhost/unobserved").build())
            val listener = JankHunterEventListenerFactory().create(call)
            listener.callStart(call)
            listener.requestHeadersStart(call)
            listener.responseHeadersStart(call)
            listener.callEnd(call)
            JankHunter.flush()
        } finally {
            instrumentation.runOnMainSync { JankHunter.shutdown() }
        }
        val files = directory.walkTopDown().filter { it.isFile && it.extension == "jhlog" }.toList()
        assertEquals(1, files.size)
        files.single().copyTo(File(instrumentation.context.filesDir, "http-first-byte-5.1.0.jhlog"), overwrite = true)
    }
}
