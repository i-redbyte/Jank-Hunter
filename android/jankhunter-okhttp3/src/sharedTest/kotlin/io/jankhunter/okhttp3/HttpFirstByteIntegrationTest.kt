package io.jankhunter.okhttp3

import io.jankhunter.runtime.JankHunterContextSnapshot
import io.jankhunter.runtime.JankHunterHttpEvent
import io.jankhunter.runtime.JankHunterWebSocketEvent
import java.io.InputStream
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.ServerSocket
import java.net.Socket
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLong
import javax.net.SocketFactory
import okhttp3.Call
import okhttp3.EventListener
import okhttp3.OkHttpClient
import okhttp3.Request
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** A real transport with deterministic time: first byte and complete headers are different boundaries. */
class HttpFirstByteIntegrationTest {
    @Test
    fun firstBytePrecedesTheRestOfTheStatusLineAndHeaders() {
        val event = exchange("x")
        assertEquals("first byte must be measured at the transport read, before the remaining headers", 500L, event.ttfbMs)
        assertEquals("the full exchange includes the later headers", 1_000L, event.durationMs)
        assertEquals("actual timing needs both a semantic marker and knownness", (1L shl 24) or (1L shl 25),
            event.flags and ((1L shl 24) or (1L shl 25)))
    }

    @Test
    fun emptyResponseWithoutCallEndStillProducesExactlyOneHttpEvent() {
        assertEquals(1_000L, exchange("").durationMs)
    }

    internal fun exchange(body: String, forward: NetworkTelemetry? = null, firstByteTime: Long = 600L): JankHunterHttpEvent {
        val now = AtomicLong(100L)
        val awaitingResponse = CountDownLatch(1)
        val awaitingRemainingHeaders = CountDownLatch(1)
        val telemetry = Events(forward)
        val server = ServerSocket(0, 1, InetAddress.getByName("127.0.0.1")).apply { soTimeout = 5_000 }
        val worker = Executors.newSingleThreadExecutor()
        val callbacks = mutableListOf<String>()
        val delegate = EventListener.Factory {
            object : EventListener() {
                override fun callStart(call: Call) { callbacks += "start" }
                override fun responseHeadersStart(call: Call) { callbacks += "headersStart"; awaitingResponse.countDown() }
                override fun responseHeadersEnd(call: Call, response: okhttp3.Response) { callbacks += "headersEnd" }
                override fun responseBodyStart(call: Call) { callbacks += "bodyStart" }
                override fun responseBodyEnd(call: Call, byteCount: Long) { callbacks += "bodyEnd:$byteCount" }
                override fun callEnd(call: Call) { callbacks += "end" }
            }
        }
        val constructor = JankHunterEventListenerFactory::class.java.declaredConstructors.single {
            it.parameterTypes.contains(NetworkTelemetry::class.java)
        }.apply { isAccessible = true }
        val factory = constructor.newInstance(delegate, telemetry, NetworkLongSource { now.get() }, null)
            as JankHunterEventListenerFactory
        val builder = OkHttpClient.Builder().eventListenerFactory(factory)
            .socketFactory(ClockedSocketFactory(now, awaitingRemainingHeaders, firstByteTime))
            .readTimeout(5L, TimeUnit.SECONDS)
        val client = checkNotNull(JankHunterOkHttp3.installEventListenerFactory(builder)).build()
        assertTrue(client.eventListenerFactory() === factory)
        try {
            val sent = worker.submit {
                server.accept().use { socket ->
                    socket.soTimeout = 5_000
                    val request = socket.getInputStream().bufferedReader(Charsets.US_ASCII)
                    while (checkNotNull(request.readLine()).isNotEmpty()) { /* Read the request headers. */ }
                    check(awaitingResponse.await(5L, TimeUnit.SECONDS))
                    socket.getOutputStream().apply {
                        write('H'.code)
                        flush()
                        // The next read starts only after the first byte has actually been returned.
                        check(awaitingRemainingHeaders.await(5L, TimeUnit.SECONDS))
                        write("TTP/1.1 200 OK\r\nContent-Length: ${body.length}\r\nConnection: close\r\n\r\n$body"
                            .toByteArray(Charsets.US_ASCII))
                        flush()
                    }
                }
            }
            client.newCall(Request.Builder().url("http://127.0.0.1:${server.localPort}/first-byte").build())
                .execute().use { response ->
                    assertEquals(200, response.code())
                    checkNotNull(response.body()).bytes()
                }
            sent.get(5L, TimeUnit.SECONDS)
            assertEquals("HTTP events for callback trace $callbacks", 1, telemetry.events.size)
            return telemetry.events.single()
        } finally {
            server.close()
            worker.shutdownNow()
            assertTrue(worker.awaitTermination(5L, TimeUnit.SECONDS))
            client.connectionPool().evictAll()
            client.dispatcher().executorService().shutdown()
        }
    }

    private class ClockedSocketFactory(private val now: AtomicLong, private val remaining: CountDownLatch,
        private val firstByteTime: Long) : SocketFactory() {
        override fun createSocket(): Socket = object : Socket() {
            private var observed: InputStream? = null
            override fun getInputStream(): InputStream {
                observed?.let { return it }
                val input = super.getInputStream()
                return object : InputStream() {
                private var reads = 0
                override fun read(): Int = ByteArray(1).let { if (read(it, 0, 1) < 0) -1 else it[0].toInt() and 255 }
                override fun read(bytes: ByteArray, offset: Int, length: Int): Int {
                    if (reads > 0) { now.set(1_100L); remaining.countDown() }
                    val count = input.read(bytes, offset, length)
                    if (count > 0 && reads++ == 0) now.set(firstByteTime)
                    return count
                }
                }.also { observed = it }
            }
        }
        override fun createSocket(host: String, port: Int): Socket = connected(InetAddress.getByName(host), port)
        override fun createSocket(host: InetAddress, port: Int): Socket = connected(host, port)
        override fun createSocket(host: String, port: Int, local: InetAddress, localPort: Int): Socket =
            connected(InetAddress.getByName(host), port, local, localPort)
        override fun createSocket(host: InetAddress, port: Int, local: InetAddress, localPort: Int): Socket =
            connected(host, port, local, localPort)
        private fun connected(host: InetAddress, port: Int, local: InetAddress? = null, localPort: Int = 0): Socket =
            createSocket().apply {
                if (local != null) bind(InetSocketAddress(local, localPort))
                connect(InetSocketAddress(host, port), 5_000)
            }
    }

    private class Events(private val forward: NetworkTelemetry?) : NetworkTelemetry {
        val events = mutableListOf<JankHunterHttpEvent>()
        override fun isHttpCollectionEnabled(): Boolean = true
        override fun captureContextSnapshot(): JankHunterContextSnapshot? = null
        override fun captureHttpContextSnapshot(): JankHunterContextSnapshot? = forward?.captureHttpContextSnapshot()
        override fun recordHttp(event: JankHunterHttpEvent) { events += event; forward?.recordHttp(event) }
        override fun recordWebSocket(event: JankHunterWebSocketEvent) = Unit
    }
}
