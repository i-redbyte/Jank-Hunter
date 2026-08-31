package io.jankhunter.okhttp3

import android.os.SystemClock
import io.jankhunter.runtime.JankHunterContextSnapshot
import io.jankhunter.runtime.JankHunterHttpEvent
import io.jankhunter.runtime.JankHunterNetworkEventFlags
import java.io.IOException
import java.io.InterruptedIOException
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.ConnectException
import java.net.NoRouteToHostException
import java.net.Proxy
import java.net.ProtocolException
import java.net.SocketTimeoutException
import java.net.UnknownHostException
import java.util.ArrayDeque
import javax.net.ssl.SSLException
import kotlin.math.max
import okhttp3.Call
import okhttp3.Connection
import okhttp3.EventListener
import okhttp3.Handshake
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response

class JankHunterEventListenerFactory private constructor(
    private val delegate: EventListener.Factory?,
    private val telemetry: NetworkTelemetry,
    private val clock: NetworkLongSource,
    serviceAlias: String?,
) : EventListener.Factory {
    constructor() : this(null)

    constructor(delegate: EventListener.Factory?) : this(
        delegate = delegate,
        telemetry = RuntimeNetworkTelemetry.INSTANCE,
        clock = NetworkLongSource { SystemClock.elapsedRealtime() },
        serviceAlias = null,
    )

    constructor(delegate: EventListener.Factory?, serviceAlias: String?) : this(
        delegate = delegate,
        telemetry = RuntimeNetworkTelemetry.INSTANCE,
        clock = NetworkLongSource { SystemClock.elapsedRealtime() },
        serviceAlias = serviceAlias,
    )

    private val serviceAlias = NetworkMetricNames.serviceAlias(serviceAlias)

    override fun create(call: Call): EventListener {
        // This is application/OkHttp business code: call it once and let its exception propagate.
        val base = delegate?.create(call) ?: EventListener.NONE
        return EventListenerNonFatal.bestEffort(base) { Listener(base, telemetry, clock, serviceAlias) }
    }

    private class Listener(
        private val delegate: EventListener,
        private val telemetry: NetworkTelemetry,
        private val clock: NetworkLongSource,
        private val serviceAlias: String?,
    ) : EventListener() {
        /** Protects only this call's small in-memory state; delegates and metric I/O run outside it. */
        private val stateLock = Any()
        private var dnsStartsByDomain: HashMap<String, ArrayDeque<Long>>? = null
        private var connectAttemptsByRoute: HashMap<ConnectKey, ArrayDeque<ConnectAttempt>>? = null
        private var connectedRoutesAwaitingAcquisition: HashSet<ConnectKey>? = null

        private var startedAt = UNSET_TIME
        private var firstIOAt = UNSET_TIME
        private var requestStartedAt = UNSET_TIME
        private var requestFinishedAt = UNSET_TIME
        private var responseStartedAt = UNSET_TIME
        private var dnsMs = 0L
        private var connectMs = 0L
        private var tlsMs = 0L
        private var requestMs = 0L
        private var ttfbMs = 0L
        private var responseMs = 0L
        private var statusCode = 0
        private var protocol = JankHunterHttpEvent.PROTOCOL_UNKNOWN
        private var flags = 0L
        private var dnsAttemptCount = 0
        private var connectAttemptCount = 0
        private var tlsAttemptCount = 0
        private var connectFailureCount = 0
        private var tlsFailureCount = 0
        private var requestAttemptCount = 0
        private var redirectCount = 0
        private var nextConnectSequence = 0L
        private var requestBodyBytes = 0L
        private var responseBodyBytes = 0L
        private var phase = PHASE_CALL
        private var terminalRecorded = false
        private var contextCaptureAttempted = false
        private var callIdentityAttempted = false
        private var contextSnapshot: JankHunterContextSnapshot? = null
        private var requestLabel = UNKNOWN

        override fun callStart(call: Call) {
            prepareCallback(call)
            delegate.callStart(call)
        }

        override fun dnsStart(call: Call, domainName: String) {
            prepareCallback(call)
            val started = now()
            state {
                markFirstIO(started)
                phase = PHASE_DNS
                dnsAttemptCount++
                val startsByDomain = dnsStartsByDomain
                    ?: HashMap<String, ArrayDeque<Long>>().also { dnsStartsByDomain = it }
                startsByDomain.getOrPut(domainName, ::ArrayDeque).addLast(started)
            }
            delegate.dnsStart(call, domainName)
        }

        override fun dnsEnd(call: Call, domainName: String, inetAddressList: List<InetAddress>) {
            prepareCallback(call)
            val ended = now()
            state {
                val startsByDomain = dnsStartsByDomain
                val starts = startsByDomain?.get(domainName)
                val started = starts?.pollFirst() ?: UNSET_TIME
                if (starts != null && starts.isEmpty()) startsByDomain.remove(domainName)
                if (startsByDomain != null && startsByDomain.isEmpty()) dnsStartsByDomain = null
                dnsMs = addDuration(dnsMs, elapsed(started, ended))
            }
            delegate.dnsEnd(call, domainName, inetAddressList)
        }

        override fun connectStart(call: Call, inetSocketAddress: InetSocketAddress, proxy: Proxy) {
            prepareCallback(call)
            val started = now()
            val callbackThread = Thread.currentThread()
            val key = ConnectKey(inetSocketAddress, proxy)
            state {
                markFirstIO(started)
                phase = PHASE_CONNECT
                connectAttemptCount++
                val attempt = ConnectAttempt(
                    startedAt = started,
                    callbackThread = callbackThread,
                    sequence = nextConnectSequence++,
                )
                val attemptsByRoute = connectAttemptsByRoute
                    ?: HashMap<ConnectKey, ArrayDeque<ConnectAttempt>>().also {
                        connectAttemptsByRoute = it
                    }
                attemptsByRoute.getOrPut(key, ::ArrayDeque).addLast(attempt)
            }
            delegate.connectStart(call, inetSocketAddress, proxy)
        }

        override fun secureConnectStart(call: Call) {
            prepareCallback(call)
            val started = now()
            state {
                markFirstIO(started)
                phase = PHASE_TLS
                tlsAttemptCount++
                // HTTP_TLS is an aggregate fact for the whole OkHttp Call (redirects included).
                // Failure phase classification stays attached to the individual connect attempt.
                flags = flags or JankHunterNetworkEventFlags.HTTP_TLS
                findConnectAttempt(Thread.currentThread()) { !it.tlsStarted }?.let { attempt ->
                    attempt.tlsStarted = true
                    attempt.tlsInProgress = true
                    attempt.tlsStartedAt = started
                }
            }
            delegate.secureConnectStart(call)
        }

        override fun secureConnectEnd(call: Call, handshake: Handshake?) {
            prepareCallback(call)
            val ended = now()
            state {
                findConnectAttempt(Thread.currentThread()) { it.tlsInProgress }?.let { attempt ->
                    tlsMs = addDuration(tlsMs, elapsed(attempt.tlsStartedAt, ended))
                    attempt.tlsStartedAt = UNSET_TIME
                    attempt.tlsInProgress = false
                }
                if (handshake != null) flags = flags or JankHunterNetworkEventFlags.HTTP_TLS
            }
            delegate.secureConnectEnd(call, handshake)
        }

        override fun connectEnd(
            call: Call,
            inetSocketAddress: InetSocketAddress,
            proxy: Proxy,
            protocol: Protocol?,
        ) {
            prepareCallback(call)
            val ended = now()
            state {
                val key = ConnectKey(inetSocketAddress, proxy)
                val attempt = removeConnectAttempt(key, Thread.currentThread())
                connectMs = addDuration(connectMs, elapsed(attempt?.startedAt ?: UNSET_TIME, ended))
                this.protocol = protocolCode(protocol)
                if (attempt != null) {
                    val connectedRoutes = connectedRoutesAwaitingAcquisition
                        ?: HashSet<ConnectKey>().also { connectedRoutesAwaitingAcquisition = it }
                    connectedRoutes.add(key)
                }
            }
            delegate.connectEnd(call, inetSocketAddress, proxy, protocol)
        }

        override fun connectFailed(
            call: Call,
            inetSocketAddress: InetSocketAddress,
            proxy: Proxy,
            protocol: Protocol?,
            ioe: IOException,
        ) {
            prepareCallback(call)
            val ended = now()
            state {
                val key = ConnectKey(inetSocketAddress, proxy)
                val attempt = removeConnectAttempt(key, Thread.currentThread())
                connectMs = addDuration(connectMs, elapsed(attempt?.startedAt ?: UNSET_TIME, ended))
                if (attempt?.tlsInProgress == true) {
                    tlsMs = addDuration(tlsMs, elapsed(attempt.tlsStartedAt, ended))
                }
                val failedPhase = if (attempt?.tlsStarted == true) PHASE_TLS else PHASE_CONNECT
                if (failedPhase == PHASE_TLS) tlsFailureCount++ else connectFailureCount++
                phase = failedPhase
            }
            delegate.connectFailed(call, inetSocketAddress, proxy, protocol, ioe)
        }

        override fun connectionAcquired(call: Call, connection: Connection) {
            prepareCallback(call)
            val acquiredAt = now()
            val connectionKey = EventListenerNonFatal.bestEffort<ConnectKey?>(null) {
                val route = connection.route()
                ConnectKey(route.socketAddress(), route.proxy())
            }
            val hasTls = EventListenerNonFatal.bestEffort(false) { connection.handshake() != null }
            val acquiredProtocol = EventListenerNonFatal.bestEffort(JankHunterHttpEvent.PROTOCOL_UNKNOWN) {
                protocolCode(connection.protocol())
            }
            state {
                markFirstIO(acquiredAt)
                if (hasTls) flags = flags or JankHunterNetworkEventFlags.HTTP_TLS
                if (acquiredProtocol != JankHunterHttpEvent.PROTOCOL_UNKNOWN) protocol = acquiredProtocol
                when {
                    connectionKey == null -> Unit
                    consumeConnectedRoute(connectionKey) -> Unit
                    else -> {
                        flags = flags or JankHunterNetworkEventFlags.HTTP_REUSED_CONNECTION
                    }
                }
            }
            delegate.connectionAcquired(call, connection)
        }

        override fun connectionReleased(call: Call, connection: Connection) {
            prepareCallback(call)
            delegate.connectionReleased(call, connection)
        }

        override fun requestHeadersStart(call: Call) {
            prepareCallback(call)
            val started = now()
            state {
                markFirstIO(started)
                finishRequest(started)
                phase = PHASE_REQUEST
                requestStartedAt = started
                requestAttemptCount++
            }
            delegate.requestHeadersStart(call)
        }

        override fun requestHeadersEnd(call: Call, request: Request) {
            prepareCallback(call)
            val finished = now()
            state {
                phase = PHASE_REQUEST
                val hasBody = EventListenerNonFatal.bestEffort(false) { request.body() != null }
                if (!hasBody) {
                    finishRequest(finished)
                    flags = flags or JankHunterNetworkEventFlags.HTTP_REQUEST_BYTES_KNOWN
                }
            }
            delegate.requestHeadersEnd(call, request)
        }

        override fun requestBodyStart(call: Call) {
            prepareCallback(call)
            val started = now()
            state {
                markFirstIO(started)
                phase = PHASE_REQUEST
                if (requestStartedAt == UNSET_TIME) requestStartedAt = started
            }
            delegate.requestBodyStart(call)
        }

        override fun requestBodyEnd(call: Call, byteCount: Long) {
            prepareCallback(call)
            val finished = now()
            state {
                phase = PHASE_REQUEST
                requestBodyBytes = max(0L, byteCount)
                flags = flags or JankHunterNetworkEventFlags.HTTP_REQUEST_BYTES_KNOWN
                if (requestStartedAt == UNSET_TIME) requestStartedAt = finished
                finishRequest(finished)
            }
            delegate.requestBodyEnd(call, byteCount)
        }

        override fun responseHeadersStart(call: Call) {
            prepareCallback(call)
            val responseStartedAt = now()
            state {
                markFirstIO(responseStartedAt)
                finishRequest(responseStartedAt)
                finishResponse(responseStartedAt)
                phase = PHASE_RESPONSE
                val base = if (requestFinishedAt != UNSET_TIME) requestFinishedAt else startedAt
                ttfbMs = addDuration(ttfbMs, elapsed(base, responseStartedAt))
                this.responseStartedAt = responseStartedAt
            }
            delegate.responseHeadersStart(call)
        }

        override fun responseHeadersEnd(call: Call, response: Response) {
            prepareCallback(call)
            val code = EventListenerNonFatal.bestEffort(0) { response.code() }
            val responseProtocol = EventListenerNonFatal.bestEffort(JankHunterHttpEvent.PROTOCOL_UNKNOWN) {
                protocolCode(response.protocol())
            }
            val cacheHit = EventListenerNonFatal.bestEffort(false) { response.cacheResponse() != null }
            val redirect = EventListenerNonFatal.bestEffort(false) { isRedirect(response) }
            state {
                statusCode = code
                protocol = responseProtocol
                if (cacheHit) flags = flags or JankHunterNetworkEventFlags.HTTP_CACHE_HIT
                if (redirect) redirectCount++
            }
            delegate.responseHeadersEnd(call, response)
        }

        override fun responseBodyStart(call: Call) {
            prepareCallback(call)
            val started = now()
            state {
                markFirstIO(started)
                phase = PHASE_RESPONSE
                if (responseStartedAt == UNSET_TIME) responseStartedAt = started
            }
            delegate.responseBodyStart(call)
        }

        override fun responseBodyEnd(call: Call, byteCount: Long) {
            prepareCallback(call)
            val finished = now()
            state {
                phase = PHASE_RESPONSE
                responseBodyBytes = max(0L, byteCount)
                flags = flags or JankHunterNetworkEventFlags.HTTP_RESPONSE_BYTES_KNOWN
                finishResponse(finished)
            }
            delegate.responseBodyEnd(call, byteCount)
        }

        override fun callEnd(call: Call) {
            prepareCallback(call)
            terminalEvent(failed = false, cancelled = false, throwable = null)?.let(::record)
            delegate.callEnd(call)
        }

        override fun callFailed(call: Call, ioe: IOException) {
            prepareCallback(call)
            val cancelled = EventListenerNonFatal.bestEffort(false) { call.isCanceled() }
            terminalEvent(failed = true, cancelled = cancelled, throwable = ioe)?.let(::record)
            delegate.callFailed(call, ioe)
        }

        private fun prepareCallback(call: Call) {
            var resolveCallIdentity = false
            var captureContext = false
            state {
                if (startedAt == UNSET_TIME) {
                    val candidateStartedAt = now()
                    if (candidateStartedAt != UNSET_TIME) startedAt = candidateStartedAt
                }
                if (!callIdentityAttempted) {
                    callIdentityAttempted = true
                    resolveCallIdentity = true
                }
                if (!contextCaptureAttempted) {
                    contextCaptureAttempted = true
                    captureContext = true
                }
            }
            if (resolveCallIdentity) {
                EventListenerNonFatal.bestEffort<String?>(null) {
                    val request = call.request()
                    NetworkMetricNames.route(request.method(), request.url().encodedPath())
                }?.let { label -> state { requestLabel = label } }
            }
            if (captureContext) {
                val snapshot = EventListenerNonFatal.bestEffort<JankHunterContextSnapshot?>(null) {
                    telemetry.captureContextSnapshot()
                }
                state { contextSnapshot = snapshot }
            }
        }

        private fun terminalEvent(
            failed: Boolean,
            cancelled: Boolean,
            throwable: Throwable?,
        ): JankHunterHttpEvent? {
            val endedAt = now()
            return state {
                if (terminalRecorded) return@state null
                terminalRecorded = true
                finishRequest(endedAt)
                finishResponse(endedAt)
                var terminalFlags = if (failed) flags or JankHunterNetworkEventFlags.HTTP_FAILED else flags
                if (cancelled) terminalFlags = terminalFlags or JankHunterNetworkEventFlags.HTTP_CANCELLED
                val event = JankHunterHttpEvent(
                    contextSnapshot,
                    requestLabel,
                    serviceAlias,
                    elapsed(startedAt, endedAt),
                    elapsed(startedAt, firstIOAt),
                    dnsMs,
                    connectMs,
                    tlsMs,
                    requestMs,
                    ttfbMs,
                    responseMs,
                    statusCode,
                    if (failed) failurePhase(phase, cancelled) else JankHunterHttpEvent.FAILURE_PHASE_UNKNOWN,
                    if (failed) failureKind(throwable, cancelled) else JankHunterHttpEvent.FAILURE_KIND_UNKNOWN,
                    protocol,
                    responseBodyBytes,
                    requestBodyBytes,
                    requestAttemptCount,
                    dnsAttemptCount,
                    connectAttemptCount,
                    tlsAttemptCount,
                    connectFailureCount,
                    tlsFailureCount,
                    redirectCount,
                    terminalFlags,
                )
                clearTerminalReferences()
                event
            }
        }

        /** Must be called with [stateLock] held after the immutable terminal event is built. */
        private fun clearTerminalReferences() {
            dnsStartsByDomain = null
            connectAttemptsByRoute = null
            connectedRoutesAwaitingAcquisition = null
            contextSnapshot = null
            requestLabel = UNKNOWN
        }

        private fun record(event: JankHunterHttpEvent) = telemetry { telemetry.recordHttp(event) }

        private fun now(): Long {
            val value = EventListenerNonFatal.bestEffortLong(UNSET_TIME, clock)
            return value.takeIf { it >= 0L } ?: UNSET_TIME
        }

        private fun removeConnectAttempt(key: ConnectKey, callbackThread: Thread): ConnectAttempt? {
            val attemptsByRoute = connectAttemptsByRoute ?: return null
            val attempts = attemptsByRoute[key] ?: return null
            val matching = attempts.firstOrNull { it.callbackThread === callbackThread }
            val removed = matching ?: attempts.peekFirst()
            if (removed != null) attempts.remove(removed)
            if (attempts.isEmpty()) attemptsByRoute.remove(key)
            if (attemptsByRoute.isEmpty()) connectAttemptsByRoute = null
            return removed
        }

        private inline fun findConnectAttempt(
            callbackThread: Thread,
            predicate: (ConnectAttempt) -> Boolean,
        ): ConnectAttempt? {
            var newestOnThread: ConnectAttempt? = null
            var newestFallback: ConnectAttempt? = null
            connectAttemptsByRoute?.values?.forEach { attempts ->
                attempts.forEach { attempt ->
                    if (!predicate(attempt)) return@forEach
                    if (newestFallback == null || attempt.sequence > newestFallback!!.sequence) {
                        newestFallback = attempt
                    }
                    if (
                        attempt.callbackThread === callbackThread &&
                        (newestOnThread == null || attempt.sequence > newestOnThread!!.sequence)
                    ) {
                        newestOnThread = attempt
                    }
                }
            }
            return newestOnThread ?: newestFallback
        }

        private fun consumeConnectedRoute(key: ConnectKey): Boolean {
            val connectedRoutes = connectedRoutesAwaitingAcquisition ?: return false
            val removed = connectedRoutes.remove(key)
            if (connectedRoutes.isEmpty()) connectedRoutesAwaitingAcquisition = null
            return removed
        }

        private fun elapsed(start: Long, end: Long): Long {
            return if (start == UNSET_TIME || end == UNSET_TIME) 0L else max(0L, end - start)
        }

        private fun addDuration(total: Long, duration: Long): Long {
            return if (duration > Long.MAX_VALUE - total) Long.MAX_VALUE else total + duration
        }

        /** Must be called with [stateLock] held. */
        private fun markFirstIO(timestamp: Long) {
            if (firstIOAt == UNSET_TIME && timestamp != UNSET_TIME) firstIOAt = timestamp
        }

        /** Must be called with [stateLock] held. */
        private fun finishRequest(timestamp: Long) {
            if (requestStartedAt == UNSET_TIME) return
            requestMs = addDuration(requestMs, elapsed(requestStartedAt, timestamp))
            requestStartedAt = UNSET_TIME
            requestFinishedAt = timestamp
        }

        /** Must be called with [stateLock] held. */
        private fun finishResponse(timestamp: Long) {
            if (responseStartedAt == UNSET_TIME) return
            responseMs = addDuration(responseMs, elapsed(responseStartedAt, timestamp))
            responseStartedAt = UNSET_TIME
        }

        private fun failurePhase(currentPhase: String, cancelled: Boolean): Int {
            if (cancelled) return JankHunterHttpEvent.FAILURE_PHASE_CANCELLED
            if (firstIOAt == UNSET_TIME && currentPhase == PHASE_CALL) {
                return JankHunterHttpEvent.FAILURE_PHASE_QUEUE
            }
            return when (currentPhase) {
                PHASE_DNS -> JankHunterHttpEvent.FAILURE_PHASE_DNS
                PHASE_CONNECT -> JankHunterHttpEvent.FAILURE_PHASE_CONNECT
                PHASE_TLS -> JankHunterHttpEvent.FAILURE_PHASE_TLS
                PHASE_REQUEST -> JankHunterHttpEvent.FAILURE_PHASE_REQUEST
                PHASE_RESPONSE -> JankHunterHttpEvent.FAILURE_PHASE_RESPONSE
                else -> JankHunterHttpEvent.FAILURE_PHASE_CALL
            }
        }

        private fun failureKind(throwable: Throwable?, cancelled: Boolean): Int {
            if (cancelled) return JankHunterHttpEvent.FAILURE_KIND_CANCELLED
            return when (throwable) {
                is UnknownHostException -> JankHunterHttpEvent.FAILURE_KIND_DNS
                is SocketTimeoutException, is InterruptedIOException -> JankHunterHttpEvent.FAILURE_KIND_TIMEOUT
                is ConnectException, is NoRouteToHostException -> JankHunterHttpEvent.FAILURE_KIND_CONNECTION
                is SSLException -> JankHunterHttpEvent.FAILURE_KIND_TLS
                is ProtocolException -> JankHunterHttpEvent.FAILURE_KIND_PROTOCOL
                is IOException -> JankHunterHttpEvent.FAILURE_KIND_IO
                null -> JankHunterHttpEvent.FAILURE_KIND_UNKNOWN
                else -> JankHunterHttpEvent.FAILURE_KIND_OTHER
            }
        }

        private fun protocolCode(value: Protocol?): Int {
            return when (value) {
                Protocol.HTTP_1_0 -> JankHunterHttpEvent.PROTOCOL_HTTP_1_0
                Protocol.HTTP_1_1 -> JankHunterHttpEvent.PROTOCOL_HTTP_1_1
                Protocol.HTTP_2, Protocol.H2_PRIOR_KNOWLEDGE -> JankHunterHttpEvent.PROTOCOL_HTTP_2
                null -> JankHunterHttpEvent.PROTOCOL_UNKNOWN
                else -> if (value.toString() == HTTP_3_PROTOCOL) {
                    JankHunterHttpEvent.PROTOCOL_HTTP_3
                } else {
                    JankHunterHttpEvent.PROTOCOL_UNKNOWN
                }
            }
        }

        private fun isRedirect(response: Response): Boolean {
            val redirectStatus = when (response.code()) {
                300, 301, 302, 303, 307, 308 -> true
                else -> false
            }
            return redirectStatus && response.header(LOCATION_HEADER) != null
        }

        private inline fun <T> state(block: () -> T): T = synchronized(stateLock, block)

        private inline fun telemetry(block: () -> Unit) {
            EventListenerNonFatal.bestEffort(Unit, block)
        }

        private companion object {
            private const val UNSET_TIME = -1L
            private const val UNKNOWN = "unknown"
            private const val HTTP_3_PROTOCOL = "h3"
            private const val LOCATION_HEADER = "Location"
            private const val PHASE_CALL = "call"
            private const val PHASE_DNS = "dns"
            private const val PHASE_CONNECT = "connect"
            private const val PHASE_TLS = "tls"
            private const val PHASE_REQUEST = "request"
            private const val PHASE_RESPONSE = "response"
        }
    }

    private data class ConnectKey(
        val inetSocketAddress: InetSocketAddress,
        val proxy: Proxy,
    )

    private data class ConnectAttempt(
        val startedAt: Long,
        val callbackThread: Thread,
        val sequence: Long,
        var tlsStarted: Boolean = false,
        var tlsInProgress: Boolean = false,
        var tlsStartedAt: Long = -1L,
    )

}

private object EventListenerNonFatal {
    fun bestEffortLong(fallback: Long, supplier: NetworkLongSource): Long {
        return try {
            supplier.getAsLong()
        } catch (throwable: Throwable) {
            if (throwable is VirtualMachineError || throwable is ThreadDeath) throw throwable
            fallback
        }
    }

    inline fun <T> bestEffort(fallback: T, block: () -> T): T {
        return try {
            block()
        } catch (throwable: Throwable) {
            if (throwable is VirtualMachineError || throwable is ThreadDeath) throw throwable
            fallback
        }
    }
}
