package io.jankhunter.okhttp3

import io.jankhunter.runtime.JankHunterContextSnapshot
import io.jankhunter.runtime.JankHunterHttpEvent
import io.jankhunter.runtime.JankHunterWebSocketEvent
import java.net.InetAddress
import java.net.ServerSocket
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.Executors
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLong
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.internal.http2.Header
import okhttp3.internal.http2.Http2Connection
import okhttp3.internal.http2.Http2Stream
import okio.Okio
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class Http2FirstByteIntegrationTest {
    @Test
    fun multiplexedRepliesUseTheirActualStreamsAndRequestStartTimes() {
        val now = AtomicLong(100L)
        val events = Events()
        val incoming = LinkedBlockingQueue<Http2Stream>()
        val workers = Executors.newFixedThreadPool(3)
        val server = ServerSocket(0, 1, InetAddress.getByName("127.0.0.1")).apply { soTimeout = 5_000 }
        val constructor = JankHunterEventListenerFactory::class.java.declaredConstructors.single {
            it.parameterTypes.contains(NetworkTelemetry::class.java)
        }.apply { isAccessible = true }
        val factory = constructor.newInstance(null, events, NetworkLongSource { now.get() }, null)
            as JankHunterEventListenerFactory
        val client = OkHttpClient.Builder().eventListenerFactory(factory)
            .protocols(listOf(Protocol.H2_PRIOR_KNOWLEDGE)).readTimeout(5L, TimeUnit.SECONDS).build()
        val connection = workers.submit<Http2Connection> {
            val socket = server.accept()
            Http2Connection.Builder(false).socket(socket, "first-byte-server",
                Okio.buffer(Okio.source(socket)), Okio.buffer(Okio.sink(socket)))
                .listener(object : Http2Connection.Listener() {
                    override fun onStream(stream: Http2Stream) { incoming.add(stream) }
                }).build().also { it.start() }
        }
        try {
            val first = workers.submit { request(client, server.localPort, "first") }
            val peer = connection.get(5L, TimeUnit.SECONDS)
            val firstStream = checkNotNull(incoming.poll(5L, TimeUnit.SECONDS))
            now.set(200L)
            val second = workers.submit { request(client, server.localPort, "second") }
            val secondStream = checkNotNull(incoming.poll(5L, TimeUnit.SECONDS))
            assertTrue(firstStream.id != secondStream.id)
            now.set(700L)
            secondStream.writeHeaders(listOf(Header(":status", "200"), Header("content-length", "0")), false)
            second.get(5L, TimeUnit.SECONDS)
            now.set(1_100L)
            firstStream.writeHeaders(listOf(Header(":status", "200"), Header("content-length", "0")), false)
            first.get(5L, TimeUnit.SECONDS)
            assertEquals(2, events.http.size)
            val byRoute = events.http.associateBy { it.requestLabel }
            assertEquals(1_000L, checkNotNull(byRoute["GET /first"]).ttfbMs)
            assertEquals(500L, checkNotNull(byRoute["GET /second"]).ttfbMs)
            assertTrue(events.http.all { it.protocol == JankHunterHttpEvent.PROTOCOL_HTTP_2 })
            peer.close()
        } finally {
            client.dispatcher().cancelAll()
            client.connectionPool().evictAll()
            client.dispatcher().executorService().shutdown()
            server.close()
            if (connection.isDone && !connection.isCancelled) connection.get().close()
            workers.shutdownNow()
            assertTrue(workers.awaitTermination(5L, TimeUnit.SECONDS))
        }
    }

    private fun request(client: OkHttpClient, port: Int, path: String) {
        client.newCall(Request.Builder().url("http://127.0.0.1:$port/$path").build())
            .execute().use { response ->
                assertEquals(200, response.code())
                assertEquals(0, checkNotNull(response.body()).bytes().size)
            }
    }

    private class Events : NetworkTelemetry {
        val http = ConcurrentLinkedQueue<JankHunterHttpEvent>()
        override fun isHttpCollectionEnabled(): Boolean = true
        override fun captureContextSnapshot(): JankHunterContextSnapshot? = null
        override fun recordHttp(event: JankHunterHttpEvent) { http.add(event) }
        override fun recordWebSocket(event: JankHunterWebSocketEvent) = Unit
    }
}
