package io.jankhunter.okhttp3

import com.sun.management.ThreadMXBean
import io.jankhunter.runtime.JankHunterContextSnapshot
import io.jankhunter.runtime.JankHunterHttpEvent
import io.jankhunter.runtime.JankHunterWebSocketEvent
import java.lang.management.ManagementFactory
import java.lang.reflect.Proxy
import java.net.InetSocketAddress
import java.util.Locale
import kotlin.system.measureNanoTime
import okhttp3.Call
import okhttp3.EventListener
import okhttp3.Protocol
import okhttp3.Request
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test

class JankHunterOkHttpBenchmarkTest {
    @Test
    fun singleAttemptCallbacksHaveNoSteadyStateAllocation() {
        assumeTrue(System.getProperty("jankhunter.benchmark") == "true")
        val iterations = System.getProperty("jankhunter.benchmark.iterations")
            ?.toIntOrNull()
            ?.coerceAtLeast(MIN_ITERATIONS)
            ?: DEFAULT_ITERATIONS
        var now = 0L
        val call = call()
        val listener = testFactory(NoOpTelemetry, NetworkLongSource { ++now }).create(call)
        val address = InetSocketAddress("127.0.0.1", 443)
        listener.callStart(call)
        repeat(WARMUP_ITERATIONS) {
            runAttempt(listener, call, address)
        }

        val threadId = Thread.currentThread().id
        val allocatedBefore = allocatedBytes(threadId)
        val elapsedNs = measureNanoTime {
            repeat(iterations) {
                runAttempt(listener, call, address)
            }
        }
        val allocatedAfter = allocatedBytes(threadId)
        val operations = iterations.toLong() * CALLBACKS_PER_ATTEMPT
        val nanosPerCallback = elapsedNs.toDouble() / operations
        val bytesPerCallback = if (allocatedBefore >= 0L && allocatedAfter >= allocatedBefore) {
            (allocatedAfter - allocatedBefore).toDouble() / operations
        } else {
            Double.NaN
        }
        println(
            "JankHunter benchmark: okhttp single attempt, callbacks=$operations, " +
                "ns_per_callback=${format(nanosPerCallback)}, bytes_per_callback=${format(bytesPerCallback)}",
        )
        assertTrue(nanosPerCallback <= LATENCY_BUDGET_NS)
        if (!bytesPerCallback.isNaN()) assertTrue(bytesPerCallback <= ALLOCATION_BUDGET_BYTES)
    }

    private fun runAttempt(listener: EventListener, call: Call, address: InetSocketAddress) {
        listener.dnsStart(call, DOMAIN)
        listener.dnsEnd(call, DOMAIN, emptyList())
        listener.connectStart(call, address, java.net.Proxy.NO_PROXY)
        listener.connectEnd(call, address, java.net.Proxy.NO_PROXY, Protocol.HTTP_1_1)
    }

    private fun call(): Call {
        val request = Request.Builder().url("https://example.com/path").build()
        return Proxy.newProxyInstance(Call::class.java.classLoader, arrayOf(Call::class.java)) { proxy, method, _ ->
            when (method.name) {
                "request" -> request
                "clone" -> proxy
                "isExecuted", "isCanceled" -> false
                else -> null
            }
        } as Call
    }

    private fun testFactory(
        telemetry: NetworkTelemetry,
        clock: NetworkLongSource,
    ): JankHunterEventListenerFactory {
        val constructor = JankHunterEventListenerFactory::class.java.declaredConstructors.single {
            it.parameterTypes.contains(NetworkTelemetry::class.java)
        }
        constructor.isAccessible = true
        return constructor.newInstance(null, telemetry, clock, null) as JankHunterEventListenerFactory
    }

    private fun allocatedBytes(threadId: Long): Long {
        val bean = ManagementFactory.getThreadMXBean() as? ThreadMXBean ?: return -1L
        if (!bean.isThreadAllocatedMemorySupported) return -1L
        if (!bean.isThreadAllocatedMemoryEnabled) bean.isThreadAllocatedMemoryEnabled = true
        return bean.getThreadAllocatedBytes(threadId)
    }

    private fun format(value: Double): String = String.format(Locale.US, "%.1f", value)

    private object NoOpTelemetry : NetworkTelemetry {
        override fun isHttpCollectionEnabled(): Boolean = true

        override fun captureContextSnapshot(): JankHunterContextSnapshot? = null

        override fun recordHttp(event: JankHunterHttpEvent) = Unit

        override fun recordWebSocket(event: JankHunterWebSocketEvent) = Unit
    }

    private companion object {
        const val DOMAIN = "example.com"
        const val MIN_ITERATIONS = 100_000
        const val DEFAULT_ITERATIONS = 500_000
        const val WARMUP_ITERATIONS = 20_000
        const val CALLBACKS_PER_ATTEMPT = 4L
        const val LATENCY_BUDGET_NS = 5_000.0
        const val ALLOCATION_BUDGET_BYTES = 0.5
    }
}
