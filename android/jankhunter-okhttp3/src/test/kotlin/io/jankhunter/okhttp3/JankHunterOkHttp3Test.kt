package io.jankhunter.okhttp3

import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterContextSnapshot
import io.jankhunter.runtime.JankHunterNetworkEventFlags
import java.io.IOException
import java.lang.reflect.Modifier
import java.lang.reflect.Proxy
import java.net.InetSocketAddress
import java.net.ProxySelector
import java.net.SocketAddress
import java.net.URI
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import javax.net.SocketFactory
import okhttp3.Address
import okhttp3.Authenticator
import okhttp3.Call
import okhttp3.Connection
import okhttp3.ConnectionSpec
import okhttp3.Dns
import okhttp3.EventListener
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Route
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class JankHunterOkHttp3Test {
    @Test
    fun facadeKeepsStaticInstrumentationAbi() {
        val facade = Class.forName("io.jankhunter.okhttp3.JankHunterOkHttp3")
        val wrapEventListenerFactory = facade.getDeclaredMethod(
            "wrapEventListenerFactory",
            EventListener.Factory::class.java,
        )
        val installEventListenerFactory = facade.getDeclaredMethod(
            "installEventListenerFactory",
            OkHttpClient.Builder::class.java,
        )
        val wrapWebSocketListener = facade.getDeclaredMethod(
            "wrapWebSocketListener",
            okhttp3.WebSocketListener::class.java,
            String::class.java,
        )

        listOf(wrapEventListenerFactory, installEventListenerFactory, wrapWebSocketListener).forEach { method ->
            assertTrue("${method.name} must remain public", Modifier.isPublic(method.modifiers))
            assertTrue("${method.name} must remain static", Modifier.isStatic(method.modifiers))
        }
        assertEquals(EventListener.Factory::class.java, wrapEventListenerFactory.returnType)
        assertEquals(OkHttpClient.Builder::class.java, installEventListenerFactory.returnType)
        assertEquals(okhttp3.WebSocketListener::class.java, wrapWebSocketListener.returnType)
    }

    @Test
    fun eventListenerFactoryKeepsPublicConstructors() {
        val factory = JankHunterEventListenerFactory::class.java

        assertTrue(Modifier.isPublic(factory.getDeclaredConstructor().modifiers))
        assertTrue(
            Modifier.isPublic(
                factory.getDeclaredConstructor(EventListener.Factory::class.java).modifiers,
            ),
        )
        val testSeam = factory.declaredConstructors.single { constructor ->
            constructor.parameterTypes.contains(NetworkTelemetry::class.java)
        }
        assertTrue(Modifier.isPrivate(testSeam.modifiers))
        assertFalse(
            factory.declaredConstructors
                .filter { Modifier.isPublic(it.modifiers) }
                .any { it.parameterTypes.contains(NetworkTelemetry::class.java) },
        )
        assertFalse(
            Modifier.isPublic(Class.forName("io.jankhunter.okhttp3.NetworkTelemetry").modifiers),
        )
        assertFalse(
            Modifier.isPublic(Class.forName("io.jankhunter.okhttp3.RuntimeNetworkTelemetry").modifiers),
        )
    }

    @Test
    fun wrapEventListenerFactoryInstallsJankHunterFactoryWhenMissing() {
        assertTrue(JankHunterOkHttp3.wrapEventListenerFactory(null) is JankHunterEventListenerFactory)
    }

    @Test
    fun installEventListenerFactoryAddsJankHunterFactoryToBuilder() {
        val builder = OkHttpClient.Builder()

        val returned = JankHunterOkHttp3.installEventListenerFactory(builder)

        assertSame(builder, returned)
        assertTrue(eventListenerFactory(builder) is JankHunterEventListenerFactory)
    }

    @Test
    fun installEventListenerFactoryPreservesExistingFactoryAsDelegate() {
        val original = EventListener.Factory { EventListener.NONE }
        val builder = OkHttpClient.Builder().eventListenerFactory(original)

        JankHunterOkHttp3.installEventListenerFactory(builder)

        val factory = eventListenerFactory(builder)
        assertTrue(factory is JankHunterEventListenerFactory)
        assertSame(original, delegate(factory as JankHunterEventListenerFactory))
    }

    @Test
    fun installEventListenerFactoryDoesNotWrapTwice() {
        val builder = OkHttpClient.Builder()
        JankHunterOkHttp3.installEventListenerFactory(builder)
        val first = eventListenerFactory(builder)

        JankHunterOkHttp3.installEventListenerFactory(builder)

        assertSame(first, eventListenerFactory(builder))
    }

    @Test
    fun delegateFactoryIsCalledExactlyOnceAndItsFailurePropagates() {
        val expected = IllegalStateException("delegate create")
        val calls = AtomicInteger()
        val factory = JankHunterEventListenerFactory(
            EventListener.Factory {
                calls.incrementAndGet()
                throw expected
            },
        )

        try {
            factory.create(call())
            fail("delegate failure was swallowed")
        } catch (actual: IllegalStateException) {
            assertSame(expected, actual)
        }
        assertEquals(1, calls.get())
    }

    @Test
    fun telemetryFailureDoesNotSkipDelegateCallback() {
        val callbackCalls = AtomicInteger()
        val delegate = object : EventListener() {
            override fun callStart(call: Call) {
                callbackCalls.incrementAndGet()
            }
        }
        val telemetry = RecordingTelemetry(captureFailure = IllegalStateException("capture"))
        val listener = testFactory(
            delegate = EventListener.Factory { delegate },
            telemetry = telemetry,
            clock = { 100L },
        ).create(call())

        listener.callStart(call())

        assertEquals(1, callbackCalls.get())
    }

    @Test
    fun callAccessorFailureDoesNotSkipDelegateCallback() {
        val expected = IllegalStateException("request unavailable")
        val callbackCalls = AtomicInteger()
        val delegate = object : EventListener() {
            override fun callStart(call: Call) {
                callbackCalls.incrementAndGet()
            }
        }
        val brokenCall = call(requestFailure = expected)
        val listener = JankHunterEventListenerFactory(EventListener.Factory { delegate }).create(brokenCall)

        listener.callStart(brokenCall)

        assertEquals(1, callbackCalls.get())
    }

    @Test
    fun contextCaptureFailureDoesNotCorruptCallState() {
        var now = 100L
        val telemetry = RecordingTelemetry(captureFailure = IllegalStateException("capture"))
        val call = call()
        val listener = testFactory(
            delegate = null,
            telemetry = telemetry,
            clock = { now },
        ).create(call)

        listener.callStart(call)
        now = 130L
        listener.requestBodyEnd(call, 12L)
        now = 160L
        listener.responseBodyEnd(call, 34L)
        now = 225L
        listener.callEnd(call)

        val event = telemetry.singleHttpEvent()
        assertEquals("GET /path", event.requestLabel)
        assertEquals(125L, event.durationMs)
        assertEquals(12L, event.requestBodyBytes)
        assertEquals(34L, event.responseBodyBytes)
    }

    @Test
    fun counterFailureDoesNotStopTimingsAttemptsOrByteAccounting() {
        var now = 100L
        val telemetry = RecordingTelemetry(counterFailuresRemaining = 1)
        val call = call()
        val listener = testFactory(
            delegate = null,
            telemetry = telemetry,
            clock = { now },
        ).create(call)

        listener.callStart(call)
        now = 110L
        listener.dnsStart(call, "example.com")
        now = 125L
        listener.dnsEnd(call, "example.com", emptyList())
        now = 130L
        listener.connectStart(
            call,
            java.net.InetSocketAddress("127.0.0.1", 443),
            java.net.Proxy.NO_PROXY,
        )
        now = 150L
        listener.connectEnd(
            call,
            java.net.InetSocketAddress("127.0.0.1", 443),
            java.net.Proxy.NO_PROXY,
            null,
        )
        now = 155L
        listener.requestBodyEnd(call, 21L)
        now = 175L
        listener.responseHeadersStart(call)
        listener.responseBodyEnd(call, 55L)
        now = 200L
        listener.callEnd(call)

        val event = telemetry.singleHttpEvent()
        assertEquals(100L, event.durationMs)
        assertEquals(15L, event.dnsMs)
        assertEquals(20L, event.connectMs)
        assertEquals(20L, event.ttfbMs)
        assertEquals(21L, event.requestBodyBytes)
        assertEquals(55L, event.responseBodyBytes)
        assertEquals(1L, telemetry.gauges["network.request.dns_attempts"])
        assertEquals(1L, telemetry.gauges["network.request.connect_attempts"])
    }

    @Test
    fun callbackBeforeCallStartUsesRealRouteAndStartsSafeTimer() {
        var now = 40L
        val telemetry = RecordingTelemetry()
        val call = call()
        val listener = testFactory(
            delegate = null,
            telemetry = telemetry,
            clock = { now },
        ).create(call)

        listener.dnsStart(call, "example.com")
        now = 50L
        listener.dnsEnd(call, "example.com", emptyList())
        now = 80L
        listener.callEnd(call)

        assertEquals(1L, telemetry.counters["network.route.get_path.dns.lookup.count"])
        val event = telemetry.singleHttpEvent()
        assertEquals("GET /path", event.requestLabel)
        assertEquals(40L, event.durationMs)
        assertEquals(10L, event.dnsMs)
    }

    @Test
    fun callbackOrderAnomaliesAndRegressingClockCannotCreateHugeDurations() {
        var now = 500L
        val telemetry = RecordingTelemetry()
        val call = call()
        val listener = testFactory(
            delegate = null,
            telemetry = telemetry,
            clock = { now },
        ).create(call)

        listener.dnsEnd(call, "example.com", emptyList())
        listener.connectEnd(
            call,
            java.net.InetSocketAddress("127.0.0.1", 443),
            java.net.Proxy.NO_PROXY,
            null,
        )
        now = 400L
        listener.responseHeadersStart(call)
        now = 300L
        listener.callEnd(call)

        val event = telemetry.singleHttpEvent()
        assertEquals(0L, event.durationMs)
        assertEquals(0L, event.dnsMs)
        assertEquals(0L, event.connectMs)
        assertEquals(0L, event.ttfbMs)
    }

    @Test
    fun clockFailureCannotTurnDeviceUptimeIntoRequestDuration() {
        var clockCalls = 0
        val telemetry = RecordingTelemetry()
        val call = call()
        val listener = testFactory(
            delegate = null,
            telemetry = telemetry,
            clock = {
                if (clockCalls++ == 0) throw IllegalStateException("clock")
                900_000_000L
            },
        ).create(call)

        listener.callStart(call)
        listener.callEnd(call)

        assertEquals(0L, telemetry.singleHttpEvent().durationMs)
    }

    @Test
    fun repeatedAndOverlappingDnsCallbacksArePairedByDomain() {
        var now = 0L
        val telemetry = RecordingTelemetry()
        val call = call()
        val listener = testFactory(
            delegate = null,
            telemetry = telemetry,
            clock = { now },
        ).create(call)

        listener.callStart(call)
        now = 10L
        listener.dnsStart(call, "one.example")
        now = 20L
        listener.dnsStart(call, "two.example")
        now = 50L
        listener.dnsEnd(call, "one.example", emptyList())
        now = 70L
        listener.dnsEnd(call, "two.example", emptyList())
        now = 80L
        listener.dnsStart(call, "same.example")
        now = 90L
        listener.dnsStart(call, "same.example")
        now = 110L
        listener.dnsEnd(call, "same.example", emptyList())
        now = 130L
        listener.dnsEnd(call, "same.example", emptyList())
        now = 150L
        listener.callEnd(call)

        assertEquals(160L, telemetry.singleHttpEvent().dnsMs)
        assertEquals(4L, telemetry.gauges["network.request.dns_attempts"])
        assertEquals(1L, telemetry.counters["network.request.retry_or_reconnect.count"])
    }

    @Test
    fun concurrentSameRouteConnectAttemptsKeepTheirOwnTlsStateAndTiming() {
        val time = ThreadLocal.withInitial { 50L }
        val telemetry = RecordingTelemetry()
        val call = call()
        val listener = testFactory(
            delegate = null,
            telemetry = telemetry,
            clock = { time.get() ?: 50L },
        ).create(call)
        val socketAddress = InetSocketAddress("127.0.0.1", 443)
        val bothStarted = CountDownLatch(2)
        val finishAttempts = CountDownLatch(1)
        val executor = Executors.newFixedThreadPool(2)

        listener.callStart(call)
        try {
            val tlsAttempt = executor.submit {
                time.set(100L)
                listener.connectStart(call, socketAddress, java.net.Proxy.NO_PROXY)
                listener.secureConnectStart(call)
                listener.secureConnectEnd(call, null)
                bothStarted.countDown()
                assertTrue(finishAttempts.await(5, TimeUnit.SECONDS))
                time.set(170L)
                listener.connectEnd(call, socketAddress, java.net.Proxy.NO_PROXY, Protocol.HTTP_1_1)
            }
            val plainAttempt = executor.submit {
                time.set(110L)
                listener.connectStart(call, socketAddress, java.net.Proxy.NO_PROXY)
                bothStarted.countDown()
                assertTrue(finishAttempts.await(5, TimeUnit.SECONDS))
                time.set(160L)
                listener.connectFailed(
                    call,
                    socketAddress,
                    java.net.Proxy.NO_PROXY,
                    null,
                    IOException("plain connect"),
                )
            }

            assertTrue(bothStarted.await(5, TimeUnit.SECONDS))
            finishAttempts.countDown()
            tlsAttempt.get(5, TimeUnit.SECONDS)
            plainAttempt.get(5, TimeUnit.SECONDS)
        } finally {
            finishAttempts.countDown()
            executor.shutdownNow()
            assertTrue(executor.awaitTermination(5, TimeUnit.SECONDS))
        }
        time.set(250L)
        listener.callEnd(call)

        val event = telemetry.singleHttpEvent()
        assertEquals(120L, event.connectMs)
        assertEquals(2L, telemetry.gauges["network.request.connect_attempts"])
        assertEquals(1L, telemetry.gauges["network.request.tls_attempts"])
        assertEquals(1L, telemetry.counters["network.phase.connect.failure.count"])
        assertNull(telemetry.counters["network.phase.tls.failure.count"])
        assertEquals(
            JankHunterNetworkEventFlags.HTTP_TLS,
            event.flags and JankHunterNetworkEventFlags.HTTP_TLS,
        )
    }

    @Test
    fun pooledConnectionAfterNewConnectionIsClassifiedPerAcquisition() {
        var now = 0L
        val telemetry = RecordingTelemetry()
        val call = call()
        val listener = testFactory(
            delegate = null,
            telemetry = telemetry,
            clock = { now },
        ).create(call)
        val socketAddress = InetSocketAddress("127.0.0.1", 443)
        val connection = connection(socketAddress)

        listener.callStart(call)
        now = 10L
        listener.connectStart(call, socketAddress, java.net.Proxy.NO_PROXY)
        now = 30L
        listener.connectEnd(call, socketAddress, java.net.Proxy.NO_PROXY, Protocol.HTTP_1_1)
        listener.connectionAcquired(call, connection)
        listener.connectionReleased(call, connection)
        listener.connectionAcquired(call, connection)
        now = 50L
        listener.callEnd(call)

        assertEquals(1L, telemetry.counters["network.request.new_connection.count"])
        assertEquals(1L, telemetry.counters["network.request.reused_connection.count"])
        assertEquals(
            JankHunterNetworkEventFlags.HTTP_REUSED_CONNECTION,
            telemetry.singleHttpEvent().flags and JankHunterNetworkEventFlags.HTTP_REUSED_CONNECTION,
        )
    }

    @Test
    fun parallelSuccessfulConnectsForSameRouteCannotLeavePhantomNewConnection() {
        var now = 0L
        val telemetry = RecordingTelemetry()
        val call = call()
        val listener = testFactory(
            delegate = null,
            telemetry = telemetry,
            clock = { now },
        ).create(call)
        val socketAddress = InetSocketAddress("127.0.0.1", 443)
        val connection = connection(socketAddress)

        listener.callStart(call)
        now = 10L
        listener.connectStart(call, socketAddress, java.net.Proxy.NO_PROXY)
        now = 20L
        listener.connectStart(call, socketAddress, java.net.Proxy.NO_PROXY)
        now = 30L
        listener.connectEnd(call, socketAddress, java.net.Proxy.NO_PROXY, Protocol.HTTP_1_1)
        now = 40L
        listener.connectEnd(call, socketAddress, java.net.Proxy.NO_PROXY, Protocol.HTTP_1_1)
        listener.connectionAcquired(call, connection)
        listener.connectionReleased(call, connection)
        listener.connectionAcquired(call, connection)
        now = 50L
        listener.callEnd(call)

        assertEquals(1L, telemetry.counters["network.request.new_connection.count"])
        assertEquals(1L, telemetry.counters["network.request.reused_connection.count"])
    }

    @Test
    fun tlsOnEarlierRouteDoesNotMisclassifyLaterPlainConnectFailure() {
        var now = 0L
        val telemetry = RecordingTelemetry()
        val call = call()
        val listener = testFactory(
            delegate = null,
            telemetry = telemetry,
            clock = { now },
        ).create(call)
        val tlsAddress = InetSocketAddress("127.0.0.1", 443)
        val plainAddress = InetSocketAddress("127.0.0.2", 80)
        val expected = IOException("plain route failed")

        listener.callStart(call)
        now = 10L
        listener.connectStart(call, tlsAddress, java.net.Proxy.NO_PROXY)
        listener.secureConnectStart(call)
        listener.secureConnectEnd(call, null)
        now = 20L
        listener.connectEnd(call, tlsAddress, java.net.Proxy.NO_PROXY, Protocol.HTTP_1_1)
        listener.connectionAcquired(call, connection(tlsAddress))
        listener.connectionReleased(call, connection(tlsAddress))
        now = 30L
        listener.connectStart(call, plainAddress, java.net.Proxy.NO_PROXY)
        now = 40L
        listener.connectFailed(call, plainAddress, java.net.Proxy.NO_PROXY, null, expected)
        now = 50L
        listener.callFailed(call, expected)

        assertEquals(1L, telemetry.counters["network.phase.connect.failure.count"])
        assertNull(telemetry.counters["network.phase.tls.failure.count"])
    }

    @Test
    fun duplicateTerminalCallbackCannotEmitFailureOrPhaseMetrics() {
        var now = 0L
        val telemetry = RecordingTelemetry()
        val call = call()
        val listener = testFactory(
            delegate = null,
            telemetry = telemetry,
            clock = { now },
        ).create(call)

        listener.callStart(call)
        now = 10L
        listener.callEnd(call)
        listener.callFailed(call, IOException("late duplicate"))

        assertEquals(1, telemetry.httpEvents.size)
        assertEquals(1L, telemetry.counters["network.request.finished.count"])
        assertNull(telemetry.counters["network.request.failed.count"])
        assertTrue(telemetry.counters.keys.none { it.startsWith("network.phase.") })
    }

    @Test
    fun racingTerminalCallbacksRecordExactlyOneConsistentOutcome() {
        val telemetry = RecordingTelemetry()
        val call = call()
        val listener = testFactory(
            delegate = null,
            telemetry = telemetry,
            clock = { 100L },
        ).create(call)
        val start = CountDownLatch(1)
        val executor = Executors.newFixedThreadPool(2)

        listener.callStart(call)
        try {
            val ended = executor.submit {
                assertTrue(start.await(5, TimeUnit.SECONDS))
                listener.callEnd(call)
            }
            val failed = executor.submit {
                assertTrue(start.await(5, TimeUnit.SECONDS))
                listener.callFailed(call, IOException("race"))
            }
            start.countDown()
            ended.get(5, TimeUnit.SECONDS)
            failed.get(5, TimeUnit.SECONDS)
        } finally {
            start.countDown()
            executor.shutdownNow()
            assertTrue(executor.awaitTermination(5, TimeUnit.SECONDS))
        }

        val finishedCount = telemetry.counters["network.request.finished.count"] ?: 0L
        val failedCount = telemetry.counters["network.request.failed.count"] ?: 0L
        val phaseFailureCount = telemetry.counters["network.phase.call.failure.count"] ?: 0L
        assertEquals(1, telemetry.httpEvents.size)
        assertEquals(1L, finishedCount + failedCount)
        assertEquals(failedCount, phaseFailureCount)
    }

    @Test
    fun fatalTelemetryAndClockFailuresAreNeverSuppressed() {
        val call = call()
        val outOfMemory = OutOfMemoryError("telemetry fatal")
        val telemetryListener = testFactory(
            delegate = null,
            telemetry = RecordingTelemetry(captureFailure = outOfMemory),
            clock = { 0L },
        ).create(call)

        try {
            telemetryListener.callStart(call)
            fail("VirtualMachineError was swallowed")
        } catch (actual: OutOfMemoryError) {
            assertSame(outOfMemory, actual)
        }

        val threadDeath = ThreadDeath()
        val clockListener = testFactory(
            delegate = null,
            telemetry = RecordingTelemetry(),
            clock = { throw threadDeath },
        ).create(call)
        try {
            clockListener.callStart(call)
            fail("ThreadDeath was swallowed")
        } catch (actual: ThreadDeath) {
            assertSame(threadDeath, actual)
        }
    }

    @Test
    fun delegateCallbackIsCalledOnceAndItsFailurePropagates() {
        val expected = IllegalArgumentException("delegate callback")
        val callbackCalls = AtomicInteger()
        val delegate = object : EventListener() {
            override fun callStart(call: Call) {
                callbackCalls.incrementAndGet()
                throw expected
            }
        }
        val call = call()
        val listener = JankHunterEventListenerFactory(EventListener.Factory { delegate }).create(call)

        try {
            listener.callStart(call)
            fail("delegate failure was swallowed")
        } catch (actual: IllegalArgumentException) {
            assertSame(expected, actual)
        }
        assertEquals(1, callbackCalls.get())
    }

    @Test
    fun capturesNetworkAttributionAtCallStart() {
        val call = call()
        val listener = JankHunterEventListenerFactory().create(call)
        try {
            JankHunter.setScreen("CheckoutScreen")

            listener.callStart(call)
            JankHunter.setScreen("ConfirmationScreen")

            assertEquals("CheckoutScreen", contextSnapshot(listener)?.screen)
        } finally {
            JankHunter.setScreen(null)
        }
    }

    @Test
    fun callbacksWithoutTelemetryAreStillForwarded() {
        val calls = AtomicInteger()
        val delegate = object : EventListener() {
            override fun requestHeadersStart(call: Call) {
                calls.incrementAndGet()
            }

            override fun requestBodyStart(call: Call) {
                calls.incrementAndGet()
            }

            override fun responseBodyStart(call: Call) {
                calls.incrementAndGet()
            }
        }
        val call = call()
        val listener = JankHunterEventListenerFactory(EventListener.Factory { delegate }).create(call)

        listener.requestHeadersStart(call)
        listener.requestBodyStart(call)
        listener.responseBodyStart(call)

        assertEquals(3, calls.get())
    }

    private fun eventListenerFactory(builder: OkHttpClient.Builder): EventListener.Factory {
        val field = builder.javaClass.getDeclaredField("eventListenerFactory")
        field.isAccessible = true
        return field.get(builder) as EventListener.Factory
    }

    private fun delegate(factory: JankHunterEventListenerFactory): EventListener.Factory? {
        val field = JankHunterEventListenerFactory::class.java.getDeclaredField("delegate")
        field.isAccessible = true
        return field.get(factory) as? EventListener.Factory
    }

    private fun contextSnapshot(listener: EventListener): JankHunterContextSnapshot? {
        val field = listener.javaClass.getDeclaredField("contextSnapshot")
        field.isAccessible = true
        return field.get(listener) as? JankHunterContextSnapshot
    }

    private fun call(requestFailure: Throwable? = null): Call {
        val request = Request.Builder().url("https://example.com/path").build()
        return Proxy.newProxyInstance(
            Call::class.java.classLoader,
            arrayOf(Call::class.java),
        ) { proxy, method, _ ->
            when (method.name) {
                "request" -> requestFailure?.let { throw it } ?: request
                "clone" -> proxy
                "isExecuted", "isCanceled" -> false
                else -> null
            }
        } as Call
    }

    private fun connection(
        socketAddress: InetSocketAddress,
        proxy: java.net.Proxy = java.net.Proxy.NO_PROXY,
    ): Connection {
        val address = Address(
            "example.com",
            socketAddress.port,
            Dns.SYSTEM,
            SocketFactory.getDefault(),
            null,
            null,
            null,
            Authenticator.NONE,
            null,
            listOf(Protocol.HTTP_1_1),
            listOf(ConnectionSpec.CLEARTEXT),
            TestProxySelector,
        )
        val route = Route(address, proxy, socketAddress)
        return Proxy.newProxyInstance(
            Connection::class.java.classLoader,
            arrayOf(Connection::class.java),
        ) { _, method, _ ->
            when (method.name) {
                "route" -> route
                "socket" -> java.net.Socket()
                "handshake" -> null
                "protocol" -> Protocol.HTTP_1_1
                else -> null
            }
        } as Connection
    }

    private fun testFactory(
        delegate: EventListener.Factory?,
        telemetry: RecordingTelemetry,
        clock: () -> Long,
    ): JankHunterEventListenerFactory {
        val constructor = JankHunterEventListenerFactory::class.java.declaredConstructors.single {
            it.parameterTypes.contains(NetworkTelemetry::class.java)
        }
        constructor.isAccessible = true
        return constructor.newInstance(delegate, telemetry, clock) as JankHunterEventListenerFactory
    }

    private class RecordingTelemetry(
        private val captureFailure: Throwable? = null,
        private var counterFailuresRemaining: Int = 0,
    ) : NetworkTelemetry {
        val counters = linkedMapOf<String, Long>()
        val gauges = linkedMapOf<String, Long>()
        val httpEvents = mutableListOf<RecordedHttpEvent>()

        @Synchronized
        override fun captureContextSnapshot(): JankHunterContextSnapshot? {
            captureFailure?.let { throw it }
            return null
        }

        @Synchronized
        override fun recordCounter(name: String, delta: Long) {
            if (counterFailuresRemaining > 0) {
                counterFailuresRemaining--
                throw IllegalStateException("counter")
            }
            counters[name] = counters.getOrDefault(name, 0L) + delta
        }

        @Synchronized
        override fun recordGauge(name: String, value: Long) {
            gauges[name] = value
        }

        @Synchronized
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
        ) {
            httpEvents += RecordedHttpEvent(
                requestLabel = requestLabel,
                durationMs = durationMs,
                dnsMs = dnsMs,
                connectMs = connectMs,
                ttfbMs = ttfbMs,
                statusClass = statusClass,
                responseBodyBytes = responseBodyBytes,
                requestBodyBytes = requestBodyBytes,
                flags = flags,
            )
        }

        @Synchronized
        fun singleHttpEvent(): RecordedHttpEvent {
            assertEquals(1, httpEvents.size)
            return httpEvents.single()
        }
    }

    @Suppress("LongParameterList")
    private data class RecordedHttpEvent(
        val requestLabel: String,
        val durationMs: Long,
        val dnsMs: Long,
        val connectMs: Long,
        val ttfbMs: Long,
        val statusClass: Int,
        val responseBodyBytes: Long,
        val requestBodyBytes: Long,
        val flags: Long,
    )

    private object TestProxySelector : ProxySelector() {
        override fun select(uri: URI?): MutableList<java.net.Proxy> {
            return mutableListOf(java.net.Proxy.NO_PROXY)
        }

        override fun connectFailed(uri: URI?, socketAddress: SocketAddress?, error: IOException?) = Unit
    }
}
