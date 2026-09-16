package io.jankhunter.okhttp3

import io.jankhunter.runtime.JankHunterContextSnapshot
import io.jankhunter.runtime.JankHunterHttpEvent
import io.jankhunter.runtime.JankHunterNetworkEventFlags
import io.jankhunter.runtime.JankHunterWebSocketEvent
import java.io.IOException
import okhttp3.Call
import okhttp3.EventListener
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.ResponseBody
import okio.Buffer
import okio.Okio
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class HttpTransportLifecycleTest {
    @Test
    fun alreadyConsumedCacheBodyPublishesOutsideTheListenerLock() {
        val fixture = Fixture()
        val source = CachedResponseBodySource(Buffer())
        source.close()
        val body = object : ResponseBody(), JankHunterHttpTransportV1 {
            override fun contentType(): okhttp3.MediaType? = null
            override fun contentLength(): Long = 0L
            override fun source() = Okio.buffer(source)
            override fun jankHunterTransportState(): Any = source
            override fun jankHunterTransportState(state: Any?) = Unit
        }
        val metadata = Response.Builder().request(fixture.call.request()).protocol(Protocol.HTTP_1_1)
            .code(200).message("OK").build()
        fixture.observation.onFinalResponse(metadata.newBuilder().cacheResponse(metadata).body(body).build())
        assertEquals(1, fixture.events.size)
        assertEquals(0, fixture.delegateEnds)
    }

    @Test
    fun missedFirstExchangeCannotBeReplacedByALaterRedirectObservation() {
        val fixture = Fixture()
        fixture.listener.requestHeadersStart(fixture.call)
        val redirect = Response.Builder().request(fixture.call.request()).protocol(Protocol.HTTP_1_1)
            .code(302).message("Found").header("Location", "http://localhost/next").build()
        // Missing or refused transport hook: complete headers prove that the first byte is past.
        fixture.listener.responseHeadersEnd(fixture.call, redirect)
        fixture.listener.requestHeadersStart(fixture.call)
        fixture.observation.armTransport(fixture.source, 0)
        fixture.source.read(Buffer(), 1)
        fixture.listener.callEnd(fixture.call)
        assertEquals(0L, fixture.events.single().flags and JankHunterNetworkEventFlags.HTTP_TTFB_KNOWN)
    }

    @Test
    fun knownZeroIsDistinctFromAnUnavailableClock() {
        for (available in listOf(true, false)) {
            val fixture = Fixture()
            fixture.listener.requestHeadersStart(fixture.call)
            fixture.observation.armTransport(fixture.source, 0)
            fixture.clockAvailable = available
            fixture.source.read(Buffer(), 1)
            fixture.clockAvailable = true
            fixture.listener.callEnd(fixture.call)
            val event = fixture.events.single()
            assertEquals(0L, event.ttfbMs)
            assertEquals(available, event.flags and JankHunterNetworkEventFlags.HTTP_TTFB_KNOWN != 0L)
            assertNoPendingReferences(fixture.source)
        }
    }

    @Test
    fun disabledTransportDoesNotReadTheClockAndCannotReinterpretALaterByteAsTheFirst() {
        val fixture = Fixture()
        fixture.listener.requestHeadersStart(fixture.call)
        fixture.observation.armTransport(fixture.source, 0)
        fixture.enabled = false
        val reads = fixture.clockReads
        fixture.source.read(Buffer(), 1)
        assertEquals(reads, fixture.clockReads)
        fixture.enabled = true
        fixture.observation.armTransport(fixture.source, 0)
        fixture.source.read(Buffer(), 1)
        fixture.listener.callEnd(fixture.call)
        assertEquals(0L, fixture.events.single().flags and JankHunterNetworkEventFlags.HTTP_TTFB_KNOWN)
        assertNoPendingReferences(fixture.source)
    }

    @Test
    fun cancellationReleasesPendingHttp1AndHttp2Observations() {
        for (stream in listOf(0, 3)) {
            val fixture = Fixture()
            fixture.listener.requestHeadersStart(fixture.call)
            if (stream != 0) fixture.source.useHttp2()
            fixture.observation.armTransport(fixture.source, stream)
            fixture.listener.callFailed(fixture.call, IOException("cancelled"))
            val reads = fixture.clockReads
            fixture.source.read(Buffer(), 1)
            assertEquals(reads, fixture.clockReads)
            assertNoPendingReferences(fixture.source)
            assertEquals(1, fixture.events.size)
        }
    }

    @Test
    fun finalResponseOnlyCompletesAnObservedBodyAndNeverSynthesizesADelegateCallback() {
        val fixture = Fixture()
        val response = Response.Builder().request(fixture.call.request()).protocol(Protocol.HTTP_1_1)
            .code(200).message("OK").build()
        fixture.observation.onFinalResponse(response)
        assertTrue(fixture.events.isEmpty())
        fixture.listener.responseBodyEnd(fixture.call, 0)
        fixture.observation.onFinalResponse(response)
        fixture.observation.onFinalResponse(response)
        assertEquals(1, fixture.events.size)
        assertEquals(0, fixture.delegateEnds)
        fixture.listener.callEnd(fixture.call)
        assertEquals(1, fixture.events.size)
        assertEquals(1, fixture.delegateEnds)
    }

    private fun assertNoPendingReferences(source: HttpResponseByteSource) {
        for (name in listOf("http1", "clock")) {
            val field = source.javaClass.getDeclaredField(name).apply { isAccessible = true }
            assertEquals("retained $name", null, field.get(source))
        }
        val pending = source.javaClass.getDeclaredField("pending").apply { isAccessible = true }
        assertEquals(0, pending.getInt(source))
    }

    private class Fixture : NetworkTelemetry {
        val events = mutableListOf<JankHunterHttpEvent>()
        var enabled = true
        var clockAvailable = true
        var clockReads = 0
        var delegateEnds = 0
        val call = OkHttpClient().newCall(Request.Builder().url("http://localhost/test").build())
        val source = HttpResponseByteSource(Buffer().writeUtf8("response"))
        val listener: EventListener
        val observation: HttpTransportObservation
        init {
            val constructor = JankHunterEventListenerFactory::class.java.declaredConstructors.single {
                it.parameterTypes.contains(NetworkTelemetry::class.java)
            }.apply { isAccessible = true }
            val delegate = EventListener.Factory { object : EventListener() {
                override fun callEnd(call: Call) { delegateEnds++ }
            } }
            val clock = NetworkLongSource {
                clockReads++
                if (clockAvailable) 100L else throw IllegalStateException("clock unavailable")
            }
            listener = (constructor.newInstance(delegate, this, clock, null) as JankHunterEventListenerFactory).create(call)
            observation = listener as HttpTransportObservation
            listener.callStart(call)
        }
        override fun isHttpCollectionEnabled(): Boolean = enabled
        override fun captureContextSnapshot(): JankHunterContextSnapshot? = null
        override fun recordHttp(event: JankHunterHttpEvent) {
            val lock = checkNotNull(listener.javaClass.getDeclaredField("stateLock").apply { isAccessible = true }.get(listener))
            check(!Thread.holdsLock(lock)) { "metric publication inside listener state lock" }
            events += event
        }
        override fun recordWebSocket(event: JankHunterWebSocketEvent) = Unit
    }
}
