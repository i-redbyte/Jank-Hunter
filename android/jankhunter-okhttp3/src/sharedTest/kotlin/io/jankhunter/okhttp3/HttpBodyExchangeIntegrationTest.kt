package io.jankhunter.okhttp3

import io.jankhunter.runtime.JankHunterContextSnapshot
import io.jankhunter.runtime.JankHunterHttpEvent
import io.jankhunter.runtime.JankHunterNetworkEventFlags
import io.jankhunter.runtime.JankHunterWebSocketEvent
import java.net.InetAddress
import java.net.ServerSocket
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class HttpBodyExchangeIntegrationTest {
    @Test
    fun informationalContinueDoesNotMakeTheFinalBodyTotalIncomplete() {
        val telemetry = Events()
        val worker = Executors.newSingleThreadExecutor()
        val server = ServerSocket(0, 1, InetAddress.getByName("127.0.0.1")).apply { soTimeout = 5_000 }
        val client = OkHttpClient.Builder().eventListenerFactory(factory(telemetry))
            .readTimeout(5L, TimeUnit.SECONDS).build()
        try {
            val uploaded = worker.submit<Int> {
                server.accept().use { socket ->
                    socket.soTimeout = 5_000
                    val input = socket.getInputStream().bufferedReader(Charsets.US_ASCII)
                    var length = 0
                    while (true) {
                        val line = checkNotNull(input.readLine())
                        if (line.isEmpty()) break
                        if (line.startsWith("Content-Length:", ignoreCase = true)) length = line.substringAfter(':').trim().toInt()
                    }
                    val output = socket.getOutputStream()
                    output.write("HTTP/1.1 100 Continue\r\n\r\n".toByteArray(Charsets.US_ASCII))
                    output.flush()
                    repeat(length) { check(input.read() >= 0) }
                    output.write("HTTP/1.1 200 OK\r\nContent-Length: 5\r\nConnection: close\r\n\r\nhello".toByteArray(Charsets.US_ASCII))
                    output.flush()
                    length
                }
            }
            val request = Request.Builder().url("http://127.0.0.1:${server.localPort}/continue")
                .header("Expect", "100-continue").post(RequestBody.create(null, ByteArray(7))).build()
            client.newCall(request).execute().use { response -> assertEquals(5, checkNotNull(response.body()).bytes().size) }
            assertEquals(7, uploaded.get(2L, TimeUnit.SECONDS))
            val event = telemetry.events.single()
            assertEquals(1, event.attempts)
            assertEquals(7L, event.requestBodyBytes)
            assertEquals(5L, event.responseBodyBytes)
            assertTrue("100 Continue incorrectly marked a complete response body unknown",
                event.flags and JankHunterNetworkEventFlags.HTTP_RESPONSE_BYTES_KNOWN != 0L)
        } finally {
            server.close()
            worker.shutdownNow()
            check(worker.awaitTermination(2L, TimeUnit.SECONDS))
            client.connectionPool().evictAll()
            client.dispatcher().executorService().shutdown()
        }
    }

    @Test
    fun authenticationRetryIncludesBothUploadedAndDownloadedBodies() {
        val observed = exchange(authenticate = true)
        assertEquals(listOf(100, 200), observed.uploads)
        assertEquals(300L, observed.event.requestBodyBytes)
        assertEquals(130L, observed.event.responseBodyBytes)
        assertEquals(2, observed.event.attempts)
        assertTrue("whole-Call body totals lack the agreed wire marker", observed.event.flags and (1L shl 23) != 0L)
    }

    @Test
    fun redirectIncludesDiscardedIntermediateResponseBody() {
        val observed = exchange(authenticate = false)
        assertEquals(listOf(0, 0), observed.uploads)
        assertEquals(0L, observed.event.requestBodyBytes)
        assertEquals(130L, observed.event.responseBodyBytes)
        assertEquals(1, observed.event.redirects)
        assertTrue("whole-Call body totals lack the agreed wire marker", observed.event.flags and (1L shl 23) != 0L)
    }

    internal fun exchange(authenticate: Boolean): Observation {
        val telemetry = Events()
        val worker = Executors.newSingleThreadExecutor()
        val server = ServerSocket(0, 2, InetAddress.getByName("127.0.0.1")).apply { soTimeout = 5_000 }
        val url = "http://127.0.0.1:${server.localPort}/body"
        val client = OkHttpClient.Builder().eventListenerFactory(factory(telemetry))
            .authenticator { _, response ->
                response.request().newBuilder().header("Authorization", "test")
                    .post(RequestBody.create(null, ByteArray(200) { 65 })).build()
            }.readTimeout(5L, TimeUnit.SECONDS).build()
        try {
            val uploads = worker.submit<List<Int>> {
                (0..1).map { index ->
                    server.accept().use { socket ->
                        socket.soTimeout = 5_000
                        val input = socket.getInputStream().bufferedReader(Charsets.US_ASCII)
                        var length = 0
                        while (true) {
                            val line = checkNotNull(input.readLine())
                            if (line.isEmpty()) break
                            if (line.startsWith("Content-Length:", ignoreCase = true)) {
                                length = line.substringAfter(':').trim().toInt()
                            }
                        }
                        repeat(length) { check(input.read() >= 0) }
                        val bodySize = if (index == 0) 50 else 80
                        val status = if (index == 1) "200 OK" else if (authenticate) "401 Unauthorized" else "302 Found"
                        val header = if (index == 1) "" else if (authenticate) {
                            "WWW-Authenticate: Basic realm=\"test\"\r\n"
                        } else {
                            "Location: $url/final\r\n"
                        }
                        val response = "HTTP/1.1 $status\r\n${header}Content-Length: $bodySize\r\n" +
                            "Connection: close\r\n\r\n" + "x".repeat(bodySize)
                        socket.getOutputStream().apply { write(response.toByteArray(Charsets.US_ASCII)); flush() }
                        length
                    }
                }
            }
            val request = Request.Builder().url(url).apply {
                if (authenticate) post(RequestBody.create(null, ByteArray(100) { 65 }))
            }.build()
            client.newCall(request).execute().use { response ->
                assertEquals(200, response.code())
                assertEquals(80, checkNotNull(response.body()).bytes().size)
            }
            assertEquals(1, telemetry.events.size)
            return Observation(telemetry.events.single(), uploads.get(2L, TimeUnit.SECONDS))
        } finally {
            server.close()
            worker.shutdownNow()
            check(worker.awaitTermination(2L, TimeUnit.SECONDS))
            client.connectionPool().evictAll()
            client.dispatcher().executorService().shutdown()
        }
    }

    private fun factory(telemetry: NetworkTelemetry): JankHunterEventListenerFactory {
        val constructor = JankHunterEventListenerFactory::class.java.declaredConstructors.single {
            it.parameterTypes.contains(NetworkTelemetry::class.java)
        }
        constructor.isAccessible = true
        return constructor.newInstance(null, telemetry, NetworkLongSource { System.nanoTime() / 1_000_000L }, null)
            as JankHunterEventListenerFactory
    }

    internal data class Observation(val event: JankHunterHttpEvent, val uploads: List<Int>)

    private class Events : NetworkTelemetry {
        val events = mutableListOf<JankHunterHttpEvent>()
        override fun isHttpCollectionEnabled(): Boolean = true
        override fun captureContextSnapshot(): JankHunterContextSnapshot? = null
        override fun recordHttp(event: JankHunterHttpEvent) { events += event }
        override fun recordWebSocket(event: JankHunterWebSocketEvent) = Unit
    }
}
