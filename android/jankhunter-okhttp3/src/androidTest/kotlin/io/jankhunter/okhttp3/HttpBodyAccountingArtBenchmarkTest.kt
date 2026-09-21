package io.jankhunter.okhttp3

import android.os.Debug
import android.os.SystemClock
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.JankHunterContextSnapshot
import io.jankhunter.runtime.JankHunterHttpEvent
import io.jankhunter.runtime.JankHunterWebSocketEvent
import java.io.File
import okhttp3.Call
import okhttp3.EventListener
import okhttp3.OkHttpClient
import okhttp3.Request
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class HttpBodyAccountingArtBenchmarkTest {
    @Test
    fun measureBodyCallbacks() {
        val arguments = InstrumentationRegistry.getArguments()
        assumeTrue(arguments.getString("jankhunterBodyBenchmark") == "true")
        val label = requireNotNull(arguments.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val output = File(instrumentation.context.filesDir, "jankhunter-http-body-$label.jsonl")
        output.writeText("")
        val client = OkHttpClient()
        for (onMain in booleanArrayOf(true, false)) {
            val task = Runnable {
                val telemetry = Events()
                val call = client.newCall(Request.Builder().url("https://example.test/body").build())
                val listener = factory(telemetry).create(call)
                listener.callStart(call)
                repeat(WARMUP) { exchange(listener, call) }
                val samples = LongArray(EVENTS)
                val allocated = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong()
                val cpu = Debug.threadCpuTimeNanos()
                val wall = System.nanoTime()
                repeat(EVENTS) { event ->
                    val started = System.nanoTime()
                    exchange(listener, call)
                    samples[event] = System.nanoTime() - started
                }
                val wallNs = System.nanoTime() - wall
                val cpuNs = Debug.threadCpuTimeNanos() - cpu
                val allocatedBytes = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong() - allocated
                listener.callEnd(call)
                val recorded = requireNotNull(telemetry.event)
                samples.sort()
                output.appendText(JSONObject().put("label", label).put("main", onMain).put("exchanges", EVENTS)
                    .put("wall_ns", wallNs).put("thread_cpu_ns", cpuNs).put("allocated_bytes", allocatedBytes)
                    .put("p50_ns", samples[EVENTS / 2]).put("p95_ns", samples[EVENTS * 95 / 100])
                    .put("p99_ns", samples[EVENTS * 99 / 100]).put("request_bytes", recorded.requestBodyBytes)
                    .put("response_bytes", recorded.responseBodyBytes).put("attempts", recorded.attempts)
                    .toString() + "\n")
                assertEquals(WARMUP + EVENTS, recorded.attempts)
                if (arguments.getString("jankhunterAssertNoCallbackAllocation") == "true") {
                    assertTrue("body callbacks allocated $allocatedBytes bytes", allocatedBytes <= EVENTS.toLong())
                }
                if (arguments.getString("jankhunterAssertBodyTotals") == "true") {
                    assertEquals((WARMUP + EVENTS) * 100L, recorded.requestBodyBytes)
                    assertEquals((WARMUP + EVENTS) * 50L, recorded.responseBodyBytes)
                }
            }
            if (onMain) instrumentation.runOnMainSync(task) else task.run()
        }
        client.connectionPool().evictAll()
        client.dispatcher().executorService().shutdown()
    }

    private fun exchange(listener: EventListener, call: Call) {
        listener.requestHeadersStart(call)
        listener.requestBodyStart(call)
        listener.requestBodyEnd(call, 100L)
        listener.responseHeadersStart(call)
        listener.responseBodyStart(call)
        listener.responseBodyEnd(call, 50L)
    }

    private fun factory(telemetry: NetworkTelemetry): JankHunterEventListenerFactory {
        val constructor = JankHunterEventListenerFactory::class.java.declaredConstructors.single {
            it.parameterTypes.contains(NetworkTelemetry::class.java)
        }
        constructor.isAccessible = true
        return constructor.newInstance(null, telemetry, NetworkLongSource(SystemClock::uptimeMillis), null)
            as JankHunterEventListenerFactory
    }

    private class Events : NetworkTelemetry {
        var event: JankHunterHttpEvent? = null
        override fun isHttpCollectionEnabled(): Boolean = true
        override fun captureContextSnapshot(): JankHunterContextSnapshot? = null
        override fun recordHttp(event: JankHunterHttpEvent) { this.event = event }
        override fun recordWebSocket(event: JankHunterWebSocketEvent) = Unit
    }

    private companion object {
        const val WARMUP = 50_000
        const val EVENTS = 50_000
    }
}
