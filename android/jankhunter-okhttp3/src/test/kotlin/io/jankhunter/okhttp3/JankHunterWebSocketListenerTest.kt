package io.jankhunter.okhttp3

import io.jankhunter.runtime.JankHunterContextSnapshot
import io.jankhunter.runtime.JankHunterHttpEvent
import io.jankhunter.runtime.JankHunterWebSocketEvent
import java.io.IOException
import java.lang.reflect.Modifier
import java.lang.reflect.Proxy
import java.net.SocketTimeoutException
import java.util.ArrayDeque
import java.util.concurrent.atomic.AtomicInteger
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString
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
    fun lifecycleIsRecordedAsTwoBoundedTypedEvents() {
        val telemetry = RecordingTelemetry()
        val times = ArrayDeque(listOf(100L, 175L, 12_175L))
        val listener = listener(
            owner = "RealtimeRepository",
            telemetry = telemetry,
            clock = { times.removeFirst() },
        )
        val socket = webSocket()

        listener.onOpen(socket, response())
        listener.onMessage(socket, "Aé€\ud83d\ude00")
        listener.onMessage(socket, ByteString.of(*byteArrayOf(1, 2, 3)))
        listener.onClosing(socket, 1000, "done")
        listener.onClosed(socket, 1000, "done")

        assertEquals(2, telemetry.webSockets.size)
        val opened = telemetry.webSockets[0]
        assertEquals(JankHunterWebSocketEvent.STAGE_OPENED, opened.stage)
        assertEquals("GET /socket", opened.route)
        assertEquals("RealtimeRepository", opened.owner)
        assertEquals(75L, opened.durationMs)
        assertEquals(101, opened.statusCode)

        val closed = telemetry.webSockets[1]
        assertEquals(opened.connectionId, closed.connectionId)
        assertEquals(JankHunterWebSocketEvent.STAGE_CLOSED, closed.stage)
        assertEquals(12_000L, closed.durationMs)
        assertEquals(1000, closed.closeCode)
        assertEquals(1L, closed.textMessages)
        assertEquals(1L, closed.binaryMessages)
        assertEquals(13L, closed.receivedBytes)
    }

    @Test
    fun terminalCallbackIsRecordedOnce() {
        val telemetry = RecordingTelemetry()
        val listener = listener(telemetry = telemetry)
        val socket = webSocket()

        listener.onFailure(socket, IOException("closed"), null)
        listener.onClosed(socket, 1000, "done")

        assertEquals(1, telemetry.webSockets.size)
        assertEquals(JankHunterWebSocketEvent.STAGE_FAILED, telemetry.webSockets.single().stage)
        assertEquals(JankHunterWebSocketEvent.FAILURE_IO, telemetry.webSockets.single().failureKind)
    }

    @Test
    fun reconnectOrdinalContinuesAcrossListenerWrappers() {
        val telemetry = RecordingTelemetry()
        val delegate = object : WebSocketListener() {}
        val owner = "ReconnectRepository-${System.nanoTime()}"
        val first = listener(
            owner = owner,
            route = "GET /reconnect",
            delegate = delegate,
            telemetry = telemetry,
        )
        val second = listener(
            owner = owner,
            route = "GET /reconnect",
            delegate = delegate,
            telemetry = telemetry,
        )

        first.onOpen(webSocket(), response("https://example.com/reconnect"))
        first.onFailure(webSocket(), IOException("disconnect"), null)
        second.onOpen(webSocket(), response("https://example.com/reconnect"))

        val openedEvents = telemetry.webSockets.filter { it.stage == JankHunterWebSocketEvent.STAGE_OPENED }
        assertEquals(2, openedEvents.size)
        assertEquals(0, openedEvents[0].reconnectOrdinal)
        assertEquals(1, openedEvents[1].reconnectOrdinal)
    }

    @Test
    fun timeoutFailureIsClassifiedWithoutRetainingThrowable() {
        val telemetry = RecordingTelemetry()
        listener(telemetry = telemetry).onFailure(webSocket(), SocketTimeoutException("secret"), null)

        val event = telemetry.webSockets.single()
        assertEquals(JankHunterWebSocketEvent.FAILURE_TIMEOUT, event.failureKind)
        assertFalse(event.javaClass.declaredFields.any { it.type == Throwable::class.java })
    }

    @Test
    fun disabledRuntimeSkipsTelemetryAndStillCallsDelegate() {
        val telemetry = RecordingTelemetry()
        val delegateCalls = AtomicInteger()
        val delegate = object : WebSocketListener() {
            override fun onMessage(webSocket: WebSocket, text: String) {
                delegateCalls.incrementAndGet()
            }
        }
        val listener = listener(delegate = delegate, telemetry = telemetry, telemetryEnabled = { false })

        listener.onMessage(webSocket(), "payload")
        listener.onFailure(webSocket(), IOException("failure"), null)

        assertTrue(telemetry.webSockets.isEmpty())
        assertEquals(1, delegateCalls.get())
    }

    @Test
    fun nonFatalTelemetryFailureDoesNotSkipDelegate() {
        val calls = AtomicInteger()
        val delegate = object : WebSocketListener() {
            override fun onMessage(webSocket: WebSocket, text: String) {
                calls.incrementAndGet()
            }
        }
        val listener = listener(delegate = delegate, telemetry = RecordingTelemetry(recordFailure = IllegalStateException("telemetry")))

        listener.onOpen(webSocket(), response())
        listener.onMessage(webSocket(), "payload")

        assertEquals(1, calls.get())
    }

    @Test
    fun fatalTelemetryFailuresAreNotSwallowed() {
        listOf<Throwable>(OutOfMemoryError("oom"), ThreadDeath()).forEach { expected ->
            val listener = listener(telemetry = RecordingTelemetry(recordFailure = expected))
            try {
                listener.onOpen(webSocket(), response())
                fail("fatal telemetry failure was swallowed")
            } catch (actual: Throwable) {
                assertSame(expected, actual)
            }
        }
    }

    @Test
    fun telemetryTestSeamIsNotPartOfPublicJvmConstructors() {
        val constructors = JankHunterWebSocketListener::class.java.declaredConstructors
        val telemetryConstructor = constructors.single { it.parameterTypes.contains(NetworkTelemetry::class.java) }

        assertTrue(Modifier.isPrivate(telemetryConstructor.modifiers))
        assertFalse(
            constructors.filter { Modifier.isPublic(it.modifiers) }
                .any { it.parameterTypes.contains(NetworkTelemetry::class.java) },
        )
        assertFalse(JankHunterWebSocketListener::class.java.declaredFields.any { Modifier.isPublic(it.modifiers) })
    }

    @Test
    fun hotPathSwitchesUsePrimitivePorts() {
        val testSeam = JankHunterWebSocketListener::class.java.declaredConstructors.single {
            it.parameterTypes.contains(NetworkTelemetry::class.java)
        }

        assertTrue(testSeam.parameterTypes.contains(NetworkLongSource::class.java))
        assertTrue(testSeam.parameterTypes.contains(NetworkBooleanSource::class.java))
        assertFalse(testSeam.parameterTypes.contains(Function0::class.java))
    }

    private fun listener(
        owner: String? = null,
        route: String? = null,
        delegate: WebSocketListener? = null,
        telemetry: NetworkTelemetry = RecordingTelemetry(),
        clock: NetworkLongSource = NetworkLongSource { 100L },
        telemetryEnabled: NetworkBooleanSource = NetworkBooleanSource { true },
    ): JankHunterWebSocketListener {
        val constructor = JankHunterWebSocketListener::class.java.declaredConstructors.single {
            it.parameterTypes.contains(NetworkTelemetry::class.java)
        }
        constructor.isAccessible = true
        return constructor.newInstance(owner, route, delegate, telemetry, clock, telemetryEnabled) as JankHunterWebSocketListener
    }

    private fun response(url: String = "https://example.com/socket?token=secret"): Response {
        return Response.Builder()
            .request(Request.Builder().url(url).build())
            .protocol(Protocol.HTTP_1_1)
            .code(101)
            .message("Switching Protocols")
            .build()
    }

    private fun webSocket(): WebSocket {
        return Proxy.newProxyInstance(WebSocket::class.java.classLoader, arrayOf(WebSocket::class.java)) { proxy, method, _ ->
            when (method.name) {
                "queueSize" -> 0L
                "send", "close" -> false
                "request", "cancel" -> null
                "toString" -> "TestWebSocket"
                "hashCode" -> System.identityHashCode(proxy)
                "equals" -> false
                else -> null
            }
        } as WebSocket
    }

    private class RecordingTelemetry(private val recordFailure: Throwable? = null) : NetworkTelemetry {
        val webSockets = mutableListOf<JankHunterWebSocketEvent>()

        override fun captureContextSnapshot(): JankHunterContextSnapshot? = null
        override fun recordHttp(event: JankHunterHttpEvent) = Unit
        override fun recordWebSocket(event: JankHunterWebSocketEvent) {
            recordFailure?.let { throw it }
            webSockets += event
        }
    }
}
