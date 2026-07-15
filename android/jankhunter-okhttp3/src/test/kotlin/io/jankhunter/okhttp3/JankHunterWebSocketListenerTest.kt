package io.jankhunter.okhttp3

import io.jankhunter.runtime.JankHunterContextSnapshot
import java.io.IOException
import java.lang.reflect.Modifier
import java.lang.reflect.Proxy
import java.util.concurrent.atomic.AtomicInteger
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.Utf8
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class JankHunterWebSocketListenerTest {
    @Test
    fun delegateIsCalledOnceAndItsFailurePropagates() {
        val expected = IllegalStateException("delegate websocket")
        val calls = AtomicInteger()
        val delegate = object : WebSocketListener() {
            override fun onMessage(webSocket: WebSocket, text: String) {
                calls.incrementAndGet()
                throw expected
            }
        }
        val listener = JankHunterWebSocketListener(delegate = delegate)

        try {
            listener.onMessage(webSocket(), "payload")
            fail("delegate failure was swallowed")
        } catch (actual: IllegalStateException) {
            assertSame(expected, actual)
        }
        assertEquals(1, calls.get())
    }

    @Test
    fun failedSocketIsRecognizedAsReconnectBySecondWrapper() {
        val telemetry = RecordingTelemetry()
        val delegate = object : WebSocketListener() {}
        val first = listener(owner = "reconnect_test", delegate = delegate, telemetry = telemetry)
        val second = listener(owner = "reconnect_test", delegate = delegate, telemetry = telemetry)

        first.onFailure(webSocket(), IOException("disconnected"), null)
        second.onOpen(webSocket(), response())

        assertEquals(1L, telemetry.counters["websocket.reconnect_test.reconnect.count"])
        assertEquals(1L, telemetry.counters["websocket.reconnect.count"])
    }

    @Test
    fun independentListenersWithSameOwnerDoNotShareReconnectState() {
        val telemetry = RecordingTelemetry()
        val failedDelegate = TestWebSocketListener()
        val independentDelegate = TestWebSocketListener()

        listener(owner = "shared_owner", delegate = failedDelegate, telemetry = telemetry)
            .onFailure(webSocket(), IOException("disconnected"), null)
        listener(owner = "shared_owner", delegate = independentDelegate, telemetry = telemetry)
            .onOpen(webSocket(), response())

        assertEquals(null, telemetry.counters["websocket.shared_owner.reconnect.count"])
        assertEquals(null, telemetry.counters["websocket.reconnect.count"])

        listener(owner = "shared_owner", delegate = failedDelegate, telemetry = telemetry)
            .onOpen(webSocket(), response())

        assertEquals(1L, telemetry.counters["websocket.shared_owner.reconnect.count"])
        assertEquals(1L, telemetry.counters["websocket.reconnect.count"])
    }

    @Test
    fun sharedReconnectTrackerHasHardCardinalityBound() {
        val telemetry = RecordingTelemetry()
        val delegate = object : WebSocketListener() {}
        val webSocket = webSocket()
        val response = response()
        val socketCount = 300

        repeat(socketCount) { index ->
            listener(owner = "bounded_$index", delegate = delegate, telemetry = telemetry)
                .onFailure(webSocket, IOException("disconnected"), null)
        }
        repeat(socketCount) { index ->
            listener(owner = "bounded_$index", delegate = delegate, telemetry = telemetry)
                .onOpen(webSocket, response)
        }

        assertEquals(256L, telemetry.counters["websocket.reconnect.count"])
    }

    @Test
    fun closeCodeIsCountedOnceAcrossClosingAndClosed() {
        val telemetry = RecordingTelemetry()
        val listener = listener(owner = "close_once", telemetry = telemetry)
        val webSocket = webSocket()

        listener.onClosing(webSocket, 1000, "done")
        listener.onClosed(webSocket, 1000, "done")

        assertEquals(1L, telemetry.counters["websocket.close_once.closing.count"])
        assertEquals(1L, telemetry.counters["websocket.close_once.closed.count"])
        assertEquals(1L, telemetry.counters["websocket.close_once.close_code.1000.count"])
    }

    @Test
    fun disabledRuntimeSkipsMessageTelemetryAndStillCallsDelegate() {
        val telemetry = RecordingTelemetry()
        val delegateCalls = AtomicInteger()
        val delegate = object : WebSocketListener() {
            override fun onMessage(webSocket: WebSocket, text: String) {
                delegateCalls.incrementAndGet()
            }
        }
        val listener = listener(
            owner = "disabled",
            delegate = delegate,
            telemetry = telemetry,
            telemetryEnabled = { false },
        )

        listener.onMessage(webSocket(), "Aé€\ud83d\ude00")

        assertTrue(telemetry.counters.isEmpty())
        assertTrue(telemetry.gauges.isEmpty())
        assertEquals(1, delegateCalls.get())
    }

    @Test
    fun textByteGaugeUsesOkioMalformedSurrogateSemantics() {
        val telemetry = RecordingTelemetry()
        val listener = listener(owner = "utf8", telemetry = telemetry)
        val values = listOf(
            "",
            "plain ascii",
            "Aé€",
            "Aé€\ud83d\ude00\ud800Z\udc00",
        )

        values.forEach { value ->
            listener.onMessage(webSocket(), value)
            assertEquals(
                value,
                Utf8.size(value),
                telemetry.gauges["websocket.utf8.message.text.bytes"],
            )
        }
    }

    @Test
    fun nonFatalTelemetryFailureDoesNotSkipDelegate() {
        val calls = AtomicInteger()
        val delegate = object : WebSocketListener() {
            override fun onMessage(webSocket: WebSocket, text: String) {
                calls.incrementAndGet()
            }
        }
        val listener = listener(
            delegate = delegate,
            telemetry = RecordingTelemetry(recordFailure = IllegalStateException("telemetry")),
        )

        listener.onMessage(webSocket(), "payload")

        assertEquals(1, calls.get())
    }

    @Test
    fun fatalTelemetryFailuresAreNotSwallowed() {
        listOf<Throwable>(OutOfMemoryError("oom"), ThreadDeath()).forEach { expected ->
            val listener = listener(telemetry = RecordingTelemetry(recordFailure = expected))

            try {
                listener.onMessage(webSocket(), "payload")
                fail("fatal telemetry failure was swallowed")
            } catch (actual: Throwable) {
                assertSame(expected, actual)
            }
        }
    }

    @Test
    fun telemetryTestSeamIsNotPartOfPublicJvmConstructors() {
        val constructors = JankHunterWebSocketListener::class.java.declaredConstructors
        val telemetryConstructor = constructors.single { constructor ->
            constructor.parameterTypes.contains(NetworkTelemetry::class.java)
        }

        assertTrue(Modifier.isPrivate(telemetryConstructor.modifiers))
        assertFalse(
            constructors
                .filter { Modifier.isPublic(it.modifiers) }
                .any { it.parameterTypes.contains(NetworkTelemetry::class.java) },
        )
        assertFalse(
            JankHunterWebSocketListener::class.java.declaredFields.any {
                Modifier.isPublic(it.modifiers)
            },
        )
    }

    private fun listener(
        owner: String? = null,
        delegate: WebSocketListener? = null,
        telemetry: NetworkTelemetry = RecordingTelemetry(),
        clock: () -> Long = { 100L },
        telemetryEnabled: () -> Boolean = { true },
    ): JankHunterWebSocketListener {
        val constructor = JankHunterWebSocketListener::class.java.declaredConstructors.single { candidate ->
            candidate.parameterTypes.contains(NetworkTelemetry::class.java)
        }
        constructor.isAccessible = true
        return constructor.newInstance(
            owner,
            null,
            delegate,
            telemetry,
            clock,
            telemetryEnabled,
        ) as JankHunterWebSocketListener
    }

    private class TestWebSocketListener : WebSocketListener()

    private fun response(): Response {
        return Response.Builder()
            .request(Request.Builder().url("https://example.com/socket").build())
            .protocol(Protocol.HTTP_1_1)
            .code(101)
            .message("Switching Protocols")
            .build()
    }

    private fun webSocket(): WebSocket {
        return Proxy.newProxyInstance(
            WebSocket::class.java.classLoader,
            arrayOf(WebSocket::class.java),
        ) { proxy, method, _ ->
            when (method.name) {
                "queueSize" -> 0L
                "send", "close" -> false
                "request" -> null
                "cancel" -> null
                "toString" -> "TestWebSocket"
                "hashCode" -> System.identityHashCode(proxy)
                "equals" -> false
                else -> null
            }
        } as WebSocket
    }

    private class RecordingTelemetry(
        private val recordFailure: Throwable? = null,
    ) : NetworkTelemetry {
        val counters = linkedMapOf<String, Long>()
        val gauges = linkedMapOf<String, Long>()

        override fun captureContextSnapshot(): JankHunterContextSnapshot? = null

        override fun recordCounter(name: String, delta: Long) {
            recordFailure?.let { throw it }
            counters[name] = counters.getOrDefault(name, 0L) + delta
        }

        override fun recordGauge(name: String, value: Long) {
            recordFailure?.let { throw it }
            gauges[name] = value
        }

        override fun recordHttp(
            contextSnapshot: JankHunterContextSnapshot?,
            requestLabel: String,
            durationMs: Long,
            dnsMs: Long,
            connectMs: Long,
            ttfbMs: Long,
            statusClass: Int,
            responseBodyBytes: Long,
            requestBodyBytes: Long,
            flags: Long,
        ) = Unit
    }
}
