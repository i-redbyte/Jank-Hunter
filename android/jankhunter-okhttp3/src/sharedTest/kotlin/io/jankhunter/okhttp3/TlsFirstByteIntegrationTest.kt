package io.jankhunter.okhttp3

import io.jankhunter.runtime.JankHunterContextSnapshot
import io.jankhunter.runtime.JankHunterHttpEvent
import io.jankhunter.runtime.JankHunterNetworkEventFlags
import io.jankhunter.runtime.JankHunterWebSocketEvent
import java.net.InetAddress
import java.security.KeyFactory
import java.security.KeyStore
import java.security.cert.CertificateFactory
import java.security.spec.PKCS8EncodedKeySpec
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLong
import javax.net.ssl.KeyManagerFactory
import javax.net.ssl.SSLContext
import javax.net.ssl.SSLServerSocket
import javax.net.ssl.SSLSocket
import javax.net.ssl.TrustManagerFactory
import javax.net.ssl.X509TrustManager
import okhttp3.Call
import okhttp3.EventListener
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class TlsFirstByteIntegrationTest {
    @Test
    fun observesPlaintextAfterRealTlsWithoutReplacingTheApplicationsSocketFactory() {
        val tls = tls()
        val now = AtomicLong(100L)
        val events = Events()
        val delegate = EventListener.Factory { object : EventListener() {
            override fun responseHeadersEnd(call: Call, response: Response) { now.set(1_100L) }
        } }
        val constructor = JankHunterEventListenerFactory::class.java.declaredConstructors.single {
            it.parameterTypes.contains(NetworkTelemetry::class.java)
        }.apply { isAccessible = true }
        val factory = constructor.newInstance(delegate, events, NetworkLongSource { now.get() }, null)
            as JankHunterEventListenerFactory
        val socketFactory = tls.first.socketFactory
        val client = OkHttpClient.Builder().sslSocketFactory(socketFactory, tls.second)
            .eventListenerFactory(factory).protocols(listOf(Protocol.HTTP_1_1))
            .readTimeout(5L, TimeUnit.SECONDS).build()
        val server = tls.first.serverSocketFactory.createServerSocket(0, 1, InetAddress.getByName("127.0.0.1"))
            as SSLServerSocket
        server.soTimeout = 5_000
        val worker = Executors.newSingleThreadExecutor()
        try {
            val sent = worker.submit {
                (server.accept() as SSLSocket).use { socket ->
                    socket.soTimeout = 5_000
                    socket.startHandshake()
                    val input = socket.inputStream.bufferedReader(Charsets.US_ASCII)
                    while (checkNotNull(input.readLine()).isNotEmpty()) { /* request */ }
                    now.set(600L)
                    socket.outputStream.apply {
                        write("HTTP/1.1 200 OK\r\nContent-Length: 1\r\nConnection: close\r\n\r\nx".toByteArray(Charsets.US_ASCII))
                        flush()
                    }
                }
            }
            client.newCall(Request.Builder().url("https://localhost:${server.localPort}/tls").build())
                .execute().use { response -> assertEquals("x", checkNotNull(response.body()).string()) }
            sent.get(5L, TimeUnit.SECONDS)
            assertTrue(client.sslSocketFactory() === socketFactory)
            val event = events.http.single()
            assertEquals(500L, event.ttfbMs)
            assertEquals(1_000L, event.durationMs)
            val flags = JankHunterNetworkEventFlags.HTTP_TLS or JankHunterNetworkEventFlags.HTTP_TTFB_KNOWN
            assertEquals(flags, event.flags and flags)
        } finally {
            server.close()
            worker.shutdownNow()
            assertTrue(worker.awaitTermination(5L, TimeUnit.SECONDS))
            client.connectionPool().evictAll()
            client.dispatcher().executorService().shutdown()
        }
    }

    private fun tls(): Pair<SSLContext, X509TrustManager> {
        val loader = javaClass.classLoader!!
        val certificate = loader.getResourceAsStream("first-byte-tls/certificate.der")!!.use {
            CertificateFactory.getInstance("X.509").generateCertificate(it)
        }
        val key = loader.getResourceAsStream("first-byte-tls/test-key.pk8")!!.use {
            KeyFactory.getInstance("RSA").generatePrivate(PKCS8EncodedKeySpec(it.readBytes()))
        }
        val store = KeyStore.getInstance(KeyStore.getDefaultType()).apply {
            load(null, null)
            setKeyEntry("fixture", key, CharArray(0), arrayOf(certificate))
        }
        val keys = KeyManagerFactory.getInstance(KeyManagerFactory.getDefaultAlgorithm()).apply { init(store, CharArray(0)) }
        val trust = TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm()).apply { init(store) }
        val manager = trust.trustManagers.filterIsInstance<X509TrustManager>().single()
        return SSLContext.getInstance("TLS").apply { init(keys.keyManagers, arrayOf(manager), null) } to manager
    }

    private class Events : NetworkTelemetry {
        val http = mutableListOf<JankHunterHttpEvent>()
        override fun isHttpCollectionEnabled(): Boolean = true
        override fun captureContextSnapshot(): JankHunterContextSnapshot? = null
        override fun recordHttp(event: JankHunterHttpEvent) { http += event }
        override fun recordWebSocket(event: JankHunterWebSocketEvent) = Unit
    }
}
