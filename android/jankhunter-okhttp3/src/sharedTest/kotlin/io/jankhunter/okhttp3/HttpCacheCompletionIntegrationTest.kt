package io.jankhunter.okhttp3

import io.jankhunter.runtime.JankHunterContextSnapshot
import io.jankhunter.runtime.JankHunterHttpEvent
import io.jankhunter.runtime.JankHunterNetworkEventFlags
import io.jankhunter.runtime.JankHunterWebSocketEvent
import java.io.File
import java.net.InetAddress
import java.net.ServerSocket
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import okhttp3.Cache
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.ResponseBody
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class HttpCacheCompletionIntegrationTest {
    @Test
    fun cacheOnlyResponseIsRecordedWithoutInventingNetworkFirstByte() {
        exchange(consumeBeforeReturn = false)
    }

    @Test
    fun cacheBodyConsumedByInterceptorStillKeepsNativeIdentityAndOneEvent() {
        exchange(consumeBeforeReturn = true)
    }

    private fun exchange(consumeBeforeReturn: Boolean) {
        val events = Events()
        val constructor = JankHunterEventListenerFactory::class.java.declaredConstructors.single {
            it.parameterTypes.contains(NetworkTelemetry::class.java)
        }.apply { isAccessible = true }
        val factory = constructor.newInstance(null, events, NetworkLongSource { System.nanoTime() / 1_000_000L }, null)
            as JankHunterEventListenerFactory
        val directory = File.createTempFile("jh-http-cache", ".tmp").apply { check(delete()); check(mkdir()) }
        val cache = Cache(directory, 1024L * 1024L)
        val server = ServerSocket(0, 1, InetAddress.getByName("127.0.0.1")).apply { soTimeout = 5_000 }
        val worker = Executors.newSingleThreadExecutor()
        var returned: Response? = null
        var returnedBody: ResponseBody? = null
        val client = OkHttpClient.Builder().cache(cache).eventListenerFactory(factory)
            .addInterceptor { chain -> chain.proceed(chain.request()).also { response ->
                returned = response
                returnedBody = response.body()
                if (consumeBeforeReturn) assertEquals("x", checkNotNull(response.body()).string())
            } }.readTimeout(5L, TimeUnit.SECONDS).build()
        val request = Request.Builder().url("http://127.0.0.1:${server.localPort}/cache").build()
        try {
            val sent = worker.submit {
                server.accept().use { socket ->
                    val input = socket.getInputStream().bufferedReader(Charsets.US_ASCII)
                    while (checkNotNull(input.readLine()).isNotEmpty()) { /* request */ }
                    socket.getOutputStream().write(("HTTP/1.1 200 OK\r\nContent-Length: 1\r\n" +
                        "Cache-Control: public, max-age=3600\r\nConnection: close\r\n\r\nx").toByteArray(Charsets.US_ASCII))
                }
            }
            client.newCall(request).execute().use { if (!consumeBeforeReturn) assertEquals("x", checkNotNull(it.body()).string()) }
            sent.get(5L, TimeUnit.SECONDS)
            server.close()
            client.newCall(request).execute().use {
                assertNotNull("second response must actually come from cache", it.cacheResponse())
                assertSame(returned, it)
                assertSame(returnedBody, it.body())
                if (!consumeBeforeReturn) assertEquals("x", checkNotNull(it.body()).string())
            }
            assertEquals("each completed Call needs an HTTP event", 2, events.http.size)
            val cached = events.http.last()
            assertTrue(cached.flags and JankHunterNetworkEventFlags.HTTP_CACHE_HIT != 0L)
            assertEquals(0L, cached.flags and JankHunterNetworkEventFlags.HTTP_TTFB_KNOWN)
            assertEquals(0L, cached.ttfbMs)
            assertEquals(0L, cached.requestBodyBytes)
            assertEquals(0L, cached.responseBodyBytes)
        } finally {
            server.close()
            worker.shutdownNow()
            assertTrue(worker.awaitTermination(5L, TimeUnit.SECONDS))
            client.connectionPool().evictAll()
            client.dispatcher().executorService().shutdown()
            cache.close()
            check(directory.deleteRecursively())
        }
    }

    private class Events : NetworkTelemetry {
        val http = mutableListOf<JankHunterHttpEvent>()
        override fun isHttpCollectionEnabled(): Boolean = true
        override fun captureContextSnapshot(): JankHunterContextSnapshot? = null
        override fun recordHttp(event: JankHunterHttpEvent) { http += event }
        override fun recordWebSocket(event: JankHunterWebSocketEvent) = Unit
    }
}
