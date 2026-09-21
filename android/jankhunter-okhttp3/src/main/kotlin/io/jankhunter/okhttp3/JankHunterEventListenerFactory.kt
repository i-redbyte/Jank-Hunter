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
    private val firstByteClock = NetworkLongSource {
        if (telemetry.isHttpCollectionEnabled()) clock.getAsLong() else Long.MIN_VALUE
    }

    override fun create(call: Call): EventListener {
        // This is application/OkHttp business code: call it once and let its exception propagate.
        val base = delegate?.create(call) ?: EventListener.NONE
        val telemetryActive = EventListenerNonFatal.bestEffort(false) {
            telemetry.isHttpCollectionEnabled()
        }
        if (!telemetryActive) return base
        return EventListenerNonFatal.bestEffort(base) { Listener(base, telemetry, clock, serviceAlias, firstByteClock) }
    }

    private class Listener(
        private val delegate: EventListener,
        private val telemetry: NetworkTelemetry,
        private val clock: NetworkLongSource,
        private val serviceAlias: String?,
        override val firstByteClock: NetworkLongSource,
    ) : EventListener(), HttpTransportObservation {
        /** Protects only this call's small in-memory state; delegates and metric I/O run outside it. */
        private val stateLock = Any()
        private var dnsDomain: String? = null
        private var dnsStartedAt = UNSET_TIME
        private var dnsStartsByDomain: HashMap<String, ArrayDeque<Long>>? = null
        private var connectAddress: InetSocketAddress? = null
        private var connectProxy: Proxy? = null
        private var connectStartedAt = UNSET_TIME
        private var connectThread: Thread? = null
        private var connectSequence = 0L
        private var connectTlsStarted = false
        private var connectTlsInProgress = false
        private var connectTlsStartedAt = UNSET_TIME
        private var connectAttemptsByRoute: HashMap<ConnectKey, ArrayDeque<ConnectAttempt>>? = null
        private var connectedAddress: InetSocketAddress? = null
        private var connectedProxy: Proxy? = null
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
        private var firstRequestAt = UNSET_TIME
        private var firstRequestObserved = false
        private var firstByteObserved = false
        private var firstByteKnown = false
        private var transportSource: HttpResponseByteSource? = null
        private var cachedSource: CachedResponseBodySource? = null
        private var transportStream = 0
        private var http1Transport = false
        private var responseMs = 0L
        private var statusCode = 0
        private var protocol = JankHunterHttpEvent.PROTOCOL_UNKNOWN
        private var flags = JankHunterNetworkEventFlags.HTTP_BODY_TOTALS or JankHunterNetworkEventFlags.HTTP_TTFB_OBSERVED
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
        // Four state bits per direction; no per-exchange objects or callback allocations.
        private var bodyByteState = 0
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
                addDnsStart(domainName, started)
            }
            delegate.dnsStart(call, domainName)
        }

        override fun dnsEnd(call: Call, domainName: String, inetAddressList: List<InetAddress>) {
            prepareCallback(call)
            val ended = now()
            state {
                val started = removeDnsStart(domainName)
                dnsMs = addDuration(dnsMs, elapsed(started, ended))
            }
            delegate.dnsEnd(call, domainName, inetAddressList)
        }

        override fun connectStart(call: Call, inetSocketAddress: InetSocketAddress, proxy: Proxy) {
            prepareCallback(call)
            val started = now()
            val callbackThread = Thread.currentThread()
            state {
                markFirstIO(started)
                phase = PHASE_CONNECT
                connectAttemptCount++
                addConnectAttempt(inetSocketAddress, proxy, started, callbackThread)
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
                startTls(Thread.currentThread(), started)
            }
            delegate.secureConnectStart(call)
        }

        override fun secureConnectEnd(call: Call, handshake: Handshake?) {
            prepareCallback(call)
            val ended = now()
            state {
                finishTls(Thread.currentThread(), ended)
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
                val completion = finishConnectAttempt(inetSocketAddress, proxy, Thread.currentThread(), ended)
                this.protocol = protocolCode(protocol)
                if (completion != CONNECT_NOT_FOUND) markConnectedRoute(inetSocketAddress, proxy)
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
                val completion = finishConnectAttempt(inetSocketAddress, proxy, Thread.currentThread(), ended)
                val failedPhase = if (completion == CONNECT_TLS) PHASE_TLS else PHASE_CONNECT
                if (failedPhase == PHASE_TLS) tlsFailureCount++ else connectFailureCount++
                phase = failedPhase
            }
            delegate.connectFailed(call, inetSocketAddress, proxy, protocol, ioe)
        }

        override fun connectionAcquired(call: Call, connection: Connection) {
            prepareCallback(call)
            val acquiredAt = now()
            val route = EventListenerNonFatal.bestEffort(null) { connection.route() }
            val hasTls = EventListenerNonFatal.bestEffort(false) { connection.handshake() != null }
            val acquiredProtocol = EventListenerNonFatal.bestEffort(JankHunterHttpEvent.PROTOCOL_UNKNOWN) {
                protocolCode(connection.protocol())
            }
            state {
                markFirstIO(acquiredAt)
                disarmTransport()
                if (!firstByteObserved) {
                    transportSource = (connection as? JankHunterHttpTransportV1)?.jankHunterTransportState()
                        as? HttpResponseByteSource
                    http1Transport = acquiredProtocol == JankHunterHttpEvent.PROTOCOL_HTTP_1_0 ||
                        acquiredProtocol == JankHunterHttpEvent.PROTOCOL_HTTP_1_1
                }
                if (hasTls) flags = flags or JankHunterNetworkEventFlags.HTTP_TLS
                if (acquiredProtocol != JankHunterHttpEvent.PROTOCOL_UNKNOWN) protocol = acquiredProtocol
                when {
                    route == null -> Unit
                    consumeConnectedRoute(route.socketAddress(), route.proxy()) -> Unit
                    else -> {
                        flags = flags or JankHunterNetworkEventFlags.HTTP_REUSED_CONNECTION
                    }
                }
            }
            delegate.connectionAcquired(call, connection)
        }

        override fun connectionReleased(call: Call, connection: Connection) {
            prepareCallback(call)
            state { disarmTransport() }
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
                if (!firstRequestObserved) {
                    firstRequestObserved = true
                    firstRequestAt = started
                }
                if (http1Transport) transportSource?.let { armTransport(it, 0) }
                requestAttemptCount++
                bodyByteState = bodyByteState and RESPONSE_HEADERS_STARTED.inv()
                beginByteExchange(REQUEST_BYTE_SHIFT)
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
                    // Headers can establish a known empty body without consuming a body callback.
                    bodyByteState = (bodyByteState and BODY_PENDING.inv()) or BODY_OBSERVED
                }
            }
            delegate.requestHeadersEnd(call, request)
        }

        override fun requestBodyStart(call: Call) {
            prepareCallback(call)
            val started = now()
            state {
                if (bodyBytesCompleted(REQUEST_BYTE_SHIFT)) return@state
                markFirstIO(started)
                phase = PHASE_REQUEST
                beginBodyBytes(REQUEST_BYTE_SHIFT)
                if (requestStartedAt == UNSET_TIME) requestStartedAt = started
            }
            delegate.requestBodyStart(call)
        }

        override fun requestBodyEnd(call: Call, byteCount: Long) {
            prepareCallback(call)
            val finished = now()
            state {
                if (bodyBytesCompleted(REQUEST_BYTE_SHIFT)) return@state
                phase = PHASE_REQUEST
                requestBodyBytes = completeBodyBytes(requestBodyBytes, byteCount, REQUEST_BYTE_SHIFT)
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
                // Expect: 100-continue can start response headers twice within one request.
                if (bodyByteState and RESPONSE_HEADERS_STARTED == 0 || bodyBytesCompleted(RESPONSE_BYTE_SHIFT)) {
                    beginByteExchange(RESPONSE_BYTE_SHIFT)
                    bodyByteState = bodyByteState or RESPONSE_HEADERS_STARTED
                }
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
                // Complete headers prove that the first response has already arrived. If the
                // transport observation was unavailable, a later retry must not replace it.
                if (!firstByteObserved) {
                    firstByteObserved = true
                    disarmTransport()
                }
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
                if (bodyBytesCompleted(RESPONSE_BYTE_SHIFT)) return@state
                markFirstIO(started)
                phase = PHASE_RESPONSE
                beginBodyBytes(RESPONSE_BYTE_SHIFT)
                if (responseStartedAt == UNSET_TIME) responseStartedAt = started
            }
            delegate.responseBodyStart(call)
        }

        override fun responseBodyEnd(call: Call, byteCount: Long) {
            prepareCallback(call)
            val finished = now()
            state {
                if (bodyBytesCompleted(RESPONSE_BYTE_SHIFT)) return@state
                phase = PHASE_RESPONSE
                responseBodyBytes = completeBodyBytes(responseBodyBytes, byteCount, RESPONSE_BYTE_SHIFT)
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

        override fun armTransport(source: HttpResponseByteSource, streamId: Int) = state {
            if (terminalRecorded || firstByteObserved) return@state
            disarmTransport()
            val armed = if (streamId == 0) source.armHttp1(this) else source.armStream(streamId, this)
            if (armed) {
                transportSource = source
                transportStream = streamId
            }
        }

        override fun onFirstByte(atMs: Long) = state {
            if (terminalRecorded || firstByteObserved) return@state
            firstByteObserved = true
            firstByteKnown = firstRequestObserved && firstRequestAt >= 0L && atMs >= firstRequestAt
            if (firstByteKnown) ttfbMs = atMs - firstRequestAt
            disarmTransport()
        }

        override fun onFinalResponse(response: Response) {
            if (response.cacheResponse() != null && response.networkResponse() == null) {
                val source = (response.body() as? JankHunterHttpTransportV1)?.jankHunterTransportState()
                    as? CachedResponseBodySource ?: return
                state {
                    if (terminalRecorded) return
                    statusCode = response.code()
                    protocol = protocolCode(response.protocol())
                    phase = PHASE_RESPONSE
                    responseStartedAt = now()
                    flags = flags or JankHunterNetworkEventFlags.HTTP_CACHE_HIT
                    // A redirect may have performed network exchanges before this cached response.
                    // Cache reads add no network bytes and cannot repair incomplete earlier totals.
                    if (requestAttemptCount == 0) {
                        bodyByteState = (BODY_OBSERVED or BODY_COMPLETED) or
                            ((BODY_OBSERVED or BODY_COMPLETED) shl RESPONSE_BYTE_SHIFT)
                    }
                    cachedSource = source
                }
                // bind may synchronously publish a body already consumed by an interceptor.
                source.bind(this)
                // A competing terminal callback may have cleared the reference before bind.
                state { if (terminalRecorded) source.detach(this) }
                return
            }
            val completed = synchronized(stateLock) { bodyBytesCompleted(RESPONSE_BYTE_SHIFT) }
            if (completed) terminalEvent(failed = false, cancelled = false, throwable = null)?.let(::record)
        }

        override fun onCachedBodyComplete(failureKind: Int) {
            terminalEvent(failed = failureKind != JankHunterHttpEvent.FAILURE_KIND_UNKNOWN,
                cancelled = false, throwable = null, failureKindOverride = failureKind)?.let(::record)
        }

        /** Called only with this listener's lock held. Source callbacks run outside the Source lock. */
        private fun disarmTransport() {
            transportSource?.disarm(transportStream, this)
            transportSource = null
            transportStream = 0
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
                    telemetry.captureHttpContextSnapshot()
                }
                state { contextSnapshot = snapshot }
            }
        }

        private fun terminalEvent(
            failed: Boolean,
            cancelled: Boolean,
            throwable: Throwable?,
            failureKindOverride: Int = JankHunterHttpEvent.FAILURE_KIND_UNKNOWN,
        ): JankHunterHttpEvent? {
            val endedAt = now()
            return synchronized(stateLock) {
                if (terminalRecorded) return@synchronized null
                terminalRecorded = true
                finishRequest(endedAt)
                finishResponse(endedAt)
                var terminalFlags = if (failed) flags or JankHunterNetworkEventFlags.HTTP_FAILED else flags
                if (firstByteKnown && ttfbMs <= elapsed(startedAt, endedAt)) {
                    terminalFlags = terminalFlags or JankHunterNetworkEventFlags.HTTP_TTFB_KNOWN
                } else {
                    ttfbMs = 0L
                }
                if (bodyBytesKnown(REQUEST_BYTE_SHIFT)) {
                    terminalFlags = terminalFlags or JankHunterNetworkEventFlags.HTTP_REQUEST_BYTES_KNOWN
                }
                if (bodyBytesKnown(RESPONSE_BYTE_SHIFT)) {
                    terminalFlags = terminalFlags or JankHunterNetworkEventFlags.HTTP_RESPONSE_BYTES_KNOWN
                }
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
                    if (failureKindOverride != JankHunterHttpEvent.FAILURE_KIND_UNKNOWN) failureKindOverride else
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
            disarmTransport()
            cachedSource?.detach(this)
            cachedSource = null
            dnsDomain = null
            dnsStartedAt = UNSET_TIME
            dnsStartsByDomain = null
            clearFastConnect()
            connectAttemptsByRoute = null
            connectedAddress = null
            connectedProxy = null
            connectedRoutesAwaitingAcquisition = null
            contextSnapshot = null
            requestLabel = UNKNOWN
        }

        private fun record(event: JankHunterHttpEvent) = telemetry { telemetry.recordHttp(event) }

        /** A new exchange cannot make bytes from an unfinished earlier body known again. */
        private fun beginByteExchange(shift: Int) {
            if (bodyByteState and (BODY_PENDING shl shift) != 0) {
                bodyByteState = bodyByteState or (BODY_INCOMPLETE shl shift)
            }
            beginBodyBytes(shift)
        }

        private fun beginBodyBytes(shift: Int) {
            bodyByteState = (bodyByteState and (BODY_COMPLETED shl shift).inv()) or (BODY_PENDING shl shift)
        }

        private fun bodyBytesCompleted(shift: Int): Boolean = bodyByteState and (BODY_COMPLETED shl shift) != 0

        private fun bodyBytesKnown(shift: Int): Boolean {
            val state = bodyByteState ushr shift
            return state and BODY_OBSERVED != 0 && state and (BODY_PENDING or BODY_INCOMPLETE) == 0
        }

        private fun completeBodyBytes(total: Long, count: Long, shift: Int): Long {
            if (bodyBytesCompleted(shift)) return total
            bodyByteState = (bodyByteState and (BODY_PENDING shl shift).inv()) or
                ((BODY_COMPLETED or BODY_OBSERVED) shl shift)
            if (count < 0L) {
                bodyByteState = bodyByteState or (BODY_INCOMPLETE shl shift)
                return total
            }
            if (count > Long.MAX_VALUE - total) {
                bodyByteState = bodyByteState or (BODY_INCOMPLETE shl shift)
                return Long.MAX_VALUE
            }
            return total + count
        }

        private fun now(): Long {
            val value = EventListenerNonFatal.bestEffortLong(UNSET_TIME, clock)
            return if (value >= 0L) value else UNSET_TIME
        }

        /** Must be called with [stateLock] held. */
        private fun addDnsStart(domainName: String, startedAt: Long) {
            if (dnsDomain == null && dnsStartsByDomain == null) {
                dnsDomain = domainName
                dnsStartedAt = startedAt
                return
            }
            val startsByDomain = dnsStartsByDomain
                ?: HashMap<String, ArrayDeque<Long>>().also { dnsStartsByDomain = it }
            startsByDomain.getOrPut(domainName, ::ArrayDeque).addLast(startedAt)
        }

        /** Must be called with [stateLock] held. */
        private fun removeDnsStart(domainName: String): Long {
            if (dnsDomain == domainName) {
                val startedAt = dnsStartedAt
                dnsDomain = null
                dnsStartedAt = UNSET_TIME
                return startedAt
            }
            val startsByDomain = dnsStartsByDomain ?: return UNSET_TIME
            val starts = startsByDomain[domainName] ?: return UNSET_TIME
            val startedAt = starts.pollFirst() ?: UNSET_TIME
            if (starts.isEmpty()) startsByDomain.remove(domainName)
            if (startsByDomain.isEmpty()) dnsStartsByDomain = null
            return startedAt
        }

        /** Must be called with [stateLock] held. */
        private fun addConnectAttempt(
            address: InetSocketAddress,
            proxy: Proxy,
            startedAt: Long,
            callbackThread: Thread,
        ) {
            val sequence = nextConnectSequence++
            if (connectAddress == null && connectAttemptsByRoute == null) {
                connectAddress = address
                connectProxy = proxy
                connectStartedAt = startedAt
                connectThread = callbackThread
                connectSequence = sequence
                return
            }
            val attemptsByRoute = connectAttemptsByRoute
                ?: HashMap<ConnectKey, ArrayDeque<ConnectAttempt>>().also { connectAttemptsByRoute = it }
            attemptsByRoute.getOrPut(ConnectKey(address, proxy), ::ArrayDeque).addLast(
                ConnectAttempt(startedAt, callbackThread, sequence),
            )
        }

        /** Must be called with [stateLock] held. */
        private fun startTls(callbackThread: Thread, startedAt: Long) {
            var newestOnThread: ConnectAttempt? = null
            var newestFallback: ConnectAttempt? = null
            connectAttemptsByRoute?.values?.forEach { attempts ->
                attempts.forEach { attempt ->
                    if (attempt.tlsStarted) return@forEach
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
            val fastEligible = connectAddress != null && !connectTlsStarted
            val useFast = shouldUseFastConnect(fastEligible, callbackThread, newestOnThread, newestFallback)
            if (useFast) {
                connectTlsStarted = true
                connectTlsInProgress = true
                connectTlsStartedAt = startedAt
                return
            }
            (newestOnThread ?: newestFallback)?.let { attempt ->
                attempt.tlsStarted = true
                attempt.tlsInProgress = true
                attempt.tlsStartedAt = startedAt
            }
        }

        /** Must be called with [stateLock] held. */
        private fun finishTls(callbackThread: Thread, endedAt: Long) {
            var newestOnThread: ConnectAttempt? = null
            var newestFallback: ConnectAttempt? = null
            connectAttemptsByRoute?.values?.forEach { attempts ->
                attempts.forEach { attempt ->
                    if (!attempt.tlsInProgress) return@forEach
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
            val useFast = shouldUseFastConnect(connectTlsInProgress, callbackThread, newestOnThread, newestFallback)
            if (useFast) {
                tlsMs = addDuration(tlsMs, elapsed(connectTlsStartedAt, endedAt))
                connectTlsStartedAt = UNSET_TIME
                connectTlsInProgress = false
                return
            }
            (newestOnThread ?: newestFallback)?.let { attempt ->
                tlsMs = addDuration(tlsMs, elapsed(attempt.tlsStartedAt, endedAt))
                attempt.tlsStartedAt = UNSET_TIME
                attempt.tlsInProgress = false
            }
        }

        private fun shouldUseFastConnect(
            eligible: Boolean,
            callbackThread: Thread,
            newestOnThread: ConnectAttempt?,
            newestFallback: ConnectAttempt?,
        ): Boolean {
            if (!eligible) return false
            return if (connectThread === callbackThread) {
                newestOnThread == null || connectSequence > newestOnThread.sequence
            } else {
                newestOnThread == null && (newestFallback == null || connectSequence > newestFallback.sequence)
            }
        }

        /** Returns CONNECT_NOT_FOUND, CONNECT_PLAIN or CONNECT_TLS. Must hold [stateLock]. */
        private fun finishConnectAttempt(
            address: InetSocketAddress,
            proxy: Proxy,
            callbackThread: Thread,
            endedAt: Long,
        ): Int {
            val fastMatches = connectAddress == address && connectProxy == proxy
            if (fastMatches && connectThread === callbackThread) return finishFastConnect(endedAt)

            val attemptsByRoute = connectAttemptsByRoute
            val key = if (attemptsByRoute == null) null else ConnectKey(address, proxy)
            val attempts = if (key == null || attemptsByRoute == null) null else attemptsByRoute[key]
            val matching = attempts?.firstOrNull { it.callbackThread === callbackThread }
            if (matching != null) return finishFallbackConnect(checkNotNull(key), attempts, matching, endedAt)
            if (fastMatches) return finishFastConnect(endedAt)
            val first = attempts?.peekFirst() ?: return CONNECT_NOT_FOUND
            return finishFallbackConnect(checkNotNull(key), attempts, first, endedAt)
        }

        private fun finishFastConnect(endedAt: Long): Int {
            connectMs = addDuration(connectMs, elapsed(connectStartedAt, endedAt))
            if (connectTlsInProgress) {
                tlsMs = addDuration(tlsMs, elapsed(connectTlsStartedAt, endedAt))
            }
            val result = if (connectTlsStarted) CONNECT_TLS else CONNECT_PLAIN
            clearFastConnect()
            return result
        }

        private fun finishFallbackConnect(
            key: ConnectKey,
            attempts: ArrayDeque<ConnectAttempt>,
            attempt: ConnectAttempt,
            endedAt: Long,
        ): Int {
            attempts.remove(attempt)
            val attemptsByRoute = checkNotNull(connectAttemptsByRoute)
            if (attempts.isEmpty()) attemptsByRoute.remove(key)
            if (attemptsByRoute.isEmpty()) connectAttemptsByRoute = null
            connectMs = addDuration(connectMs, elapsed(attempt.startedAt, endedAt))
            if (attempt.tlsInProgress) {
                tlsMs = addDuration(tlsMs, elapsed(attempt.tlsStartedAt, endedAt))
            }
            return if (attempt.tlsStarted) CONNECT_TLS else CONNECT_PLAIN
        }

        private fun clearFastConnect() {
            connectAddress = null
            connectProxy = null
            connectStartedAt = UNSET_TIME
            connectThread = null
            connectSequence = 0L
            connectTlsStarted = false
            connectTlsInProgress = false
            connectTlsStartedAt = UNSET_TIME
        }

        /** Must be called with [stateLock] held. */
        private fun markConnectedRoute(address: InetSocketAddress, proxy: Proxy) {
            val connectedRoutes = connectedRoutesAwaitingAcquisition
            if (connectedAddress == null && connectedRoutes == null) {
                connectedAddress = address
                connectedProxy = proxy
                return
            }
            if (connectedAddress == address && connectedProxy == proxy) return
            val routes = connectedRoutes ?: HashSet<ConnectKey>().also { created ->
                created.add(ConnectKey(checkNotNull(connectedAddress), checkNotNull(connectedProxy)))
                connectedAddress = null
                connectedProxy = null
                connectedRoutesAwaitingAcquisition = created
            }
            routes.add(ConnectKey(address, proxy))
        }

        /** Must be called with [stateLock] held. */
        private fun consumeConnectedRoute(address: InetSocketAddress, proxy: Proxy): Boolean {
            if (connectedAddress == address && connectedProxy == proxy) {
                connectedAddress = null
                connectedProxy = null
                return true
            }
            val connectedRoutes = connectedRoutesAwaitingAcquisition ?: return false
            val removed = connectedRoutes.remove(ConnectKey(address, proxy))
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

        private inline fun state(block: () -> Unit) {
            synchronized(stateLock, block)
        }

        private inline fun telemetry(block: () -> Unit) {
            EventListenerNonFatal.bestEffort(Unit, block)
        }

        private companion object {
            private const val REQUEST_BYTE_SHIFT = 0
            private const val RESPONSE_BYTE_SHIFT = 4
            private const val RESPONSE_HEADERS_STARTED = 1 shl 8
            private const val BODY_PENDING = 1
            private const val BODY_COMPLETED = 2
            private const val BODY_OBSERVED = 4
            private const val BODY_INCOMPLETE = 8
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
            private const val CONNECT_NOT_FOUND = -1
            private const val CONNECT_PLAIN = 0
            private const val CONNECT_TLS = 1
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
