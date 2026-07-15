package io.jankhunter.okhttp3

import android.os.SystemClock
import io.jankhunter.runtime.JankHunterContextSnapshot
import io.jankhunter.runtime.JankHunterNetworkEventFlags
import java.io.IOException
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.Proxy
import java.util.ArrayDeque
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
    private val clock: () -> Long,
) : EventListener.Factory {
    constructor() : this(null)

    constructor(delegate: EventListener.Factory?) : this(
        delegate = delegate,
        telemetry = RuntimeNetworkTelemetry.INSTANCE,
        clock = SystemClock::elapsedRealtime,
    )

    override fun create(call: Call): EventListener {
        // This is application/OkHttp business code: call it once and let its exception propagate.
        val base = delegate?.create(call) ?: EventListener.NONE
        return EventListenerNonFatal.bestEffort(base) { Listener(base, telemetry, clock) }
    }

    private class Listener(
        private val delegate: EventListener,
        private val telemetry: NetworkTelemetry,
        private val clock: () -> Long,
    ) : EventListener() {
        /** Protects only this call's small in-memory state; delegates and metric I/O run outside it. */
        private val stateLock = Any()
        private var dnsStartsByDomain: HashMap<String, ArrayDeque<Long>>? = null
        private var connectAttemptsByRoute: HashMap<ConnectKey, ArrayDeque<ConnectAttempt>>? = null
        private var connectedRoutesAwaitingAcquisition: HashSet<ConnectKey>? = null

        private var startedAt = UNSET_TIME
        private var requestFinishedAt = UNSET_TIME
        private var dnsMs = 0L
        private var connectMs = 0L
        private var ttfbMs = 0L
        private var statusClass = 0
        private var flags = 0L
        private var connectionReleased = false
        private var dnsAttemptCount = 0
        private var connectAttemptCount = 0
        private var tlsAttemptCount = 0
        private var nextConnectSequence = 0L
        private var requestBodyBytes = 0L
        private var responseBodyBytes = 0L
        private var phase = PHASE_CALL
        private var phaseFailureRecorded = false
        private var terminalRecorded = false
        private var contextCaptureAttempted = false
        private var callIdentityAttempted = false
        private var contextSnapshot: JankHunterContextSnapshot? = null
        private var routeKey = UNKNOWN
        private var requestLabel = UNKNOWN

        override fun callStart(call: Call) {
            prepareCallback(call)
            val route = state { routeKey }
            recordCounter("network.request.started.count")
            recordCounter("network.route.$route.started.count")
            delegate.callStart(call)
        }

        override fun dnsStart(call: Call, domainName: String) {
            prepareCallback(call)
            val started = now()
            val route = state {
                phase = PHASE_DNS
                dnsAttemptCount++
                val startsByDomain = dnsStartsByDomain
                    ?: HashMap<String, ArrayDeque<Long>>().also { dnsStartsByDomain = it }
                startsByDomain.getOrPut(domainName, ::ArrayDeque).addLast(started)
                routeKey
            }
            recordCounter("network.dns.lookup.count")
            recordCounter("network.route.$route.dns.lookup.count")
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
            val route = state {
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
                routeKey
            }
            recordCounter("network.connect.attempt.count")
            recordCounter("network.route.$route.connect.attempt.count")
            delegate.connectStart(call, inetSocketAddress, proxy)
        }

        override fun secureConnectStart(call: Call) {
            prepareCallback(call)
            state {
                phase = PHASE_TLS
                tlsAttemptCount++
                // HTTP_TLS is an aggregate fact for the whole OkHttp Call (redirects included).
                // Failure phase classification stays attached to the individual connect attempt.
                flags = flags or JankHunterNetworkEventFlags.HTTP_TLS
                findConnectAttempt(Thread.currentThread()) { !it.tlsStarted }?.let { attempt ->
                    attempt.tlsStarted = true
                    attempt.tlsInProgress = true
                }
            }
            recordCounter("network.tls.handshake.count")
            delegate.secureConnectStart(call)
        }

        override fun secureConnectEnd(call: Call, handshake: Handshake?) {
            prepareCallback(call)
            state {
                findConnectAttempt(Thread.currentThread()) { it.tlsInProgress }?.tlsInProgress = false
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
            val failure = state {
                val key = ConnectKey(inetSocketAddress, proxy)
                val attempt = removeConnectAttempt(key, Thread.currentThread())
                connectMs = addDuration(connectMs, elapsed(attempt?.startedAt ?: UNSET_TIME, ended))
                val failedPhase = if (attempt?.tlsStarted == true) PHASE_TLS else PHASE_CONNECT
                phase = failedPhase
                if (terminalRecorded) null else claimPhaseFailure(failedPhase, ioe)
            }
            recordPhaseFailure(failure)
            delegate.connectFailed(call, inetSocketAddress, proxy, protocol, ioe)
        }

        override fun connectionAcquired(call: Call, connection: Connection) {
            prepareCallback(call)
            val connectionKey = EventListenerNonFatal.bestEffort<ConnectKey?>(null) {
                val route = connection.route()
                ConnectKey(route.socketAddress(), route.proxy())
            }
            val hasTls = EventListenerNonFatal.bestEffort(false) { connection.handshake() != null }
            val classification = state {
                if (hasTls) flags = flags or JankHunterNetworkEventFlags.HTTP_TLS
                when {
                    connectionKey == null -> ConnectionClassification.UNKNOWN
                    consumeConnectedRoute(connectionKey) -> ConnectionClassification.NEW
                    else -> {
                        flags = flags or JankHunterNetworkEventFlags.HTTP_REUSED_CONNECTION
                        ConnectionClassification.REUSED
                    }
                }
            }
            val route = state { routeKey }
            when (classification) {
                ConnectionClassification.NEW -> recordCounter("network.request.new_connection.count")
                ConnectionClassification.REUSED -> {
                    recordCounter("network.request.reused_connection.count")
                    recordCounter("network.route.$route.reused_connection.count")
                }

                ConnectionClassification.UNKNOWN -> Unit
            }
            delegate.connectionAcquired(call, connection)
        }

        override fun connectionReleased(call: Call, connection: Connection) {
            prepareCallback(call)
            state { connectionReleased = true }
            delegate.connectionReleased(call, connection)
        }

        override fun requestHeadersStart(call: Call) {
            prepareCallback(call)
            state { phase = PHASE_REQUEST }
            delegate.requestHeadersStart(call)
        }

        override fun requestHeadersEnd(call: Call, request: Request) {
            prepareCallback(call)
            val finished = now()
            state {
                phase = PHASE_REQUEST
                requestFinishedAt = finished
            }
            delegate.requestHeadersEnd(call, request)
        }

        override fun requestBodyStart(call: Call) {
            prepareCallback(call)
            state { phase = PHASE_REQUEST }
            delegate.requestBodyStart(call)
        }

        override fun requestBodyEnd(call: Call, byteCount: Long) {
            prepareCallback(call)
            val finished = now()
            state {
                phase = PHASE_REQUEST
                requestBodyBytes = max(0L, byteCount)
                requestFinishedAt = finished
            }
            delegate.requestBodyEnd(call, byteCount)
        }

        override fun responseHeadersStart(call: Call) {
            prepareCallback(call)
            val responseStartedAt = now()
            state {
                phase = PHASE_RESPONSE
                val base = if (requestFinishedAt != UNSET_TIME) requestFinishedAt else startedAt
                ttfbMs = elapsed(base, responseStartedAt)
            }
            delegate.responseHeadersStart(call)
        }

        override fun responseHeadersEnd(call: Call, response: Response) {
            prepareCallback(call)
            val responseStatusClass = EventListenerNonFatal.bestEffort<Int?>(null) {
                response.code() / HTTP_STATUS_CLASS_DIVISOR
            }
            if (responseStatusClass != null) state { statusClass = responseStatusClass }
            delegate.responseHeadersEnd(call, response)
        }

        override fun responseBodyStart(call: Call) {
            prepareCallback(call)
            state { phase = PHASE_RESPONSE }
            delegate.responseBodyStart(call)
        }

        override fun responseBodyEnd(call: Call, byteCount: Long) {
            prepareCallback(call)
            state {
                phase = PHASE_RESPONSE
                responseBodyBytes = max(0L, byteCount)
            }
            delegate.responseBodyEnd(call, byteCount)
        }

        override fun callEnd(call: Call) {
            prepareCallback(call)
            val snapshot = terminalSnapshot(failed = false, throwable = null)
            if (snapshot != null) {
                record(snapshot)
                recordCounter("network.request.finished.count")
                recordCounter("network.route.${snapshot.routeKey}.finished.count")
            }
            delegate.callEnd(call)
        }

        override fun callFailed(call: Call, ioe: IOException) {
            prepareCallback(call)
            // The terminal claim and phase-failure claim are atomic. Duplicate terminal callbacks
            // cannot emit phase counters before discovering that the call was already recorded.
            val snapshot = terminalSnapshot(failed = true, throwable = ioe)
            if (snapshot != null) {
                recordPhaseFailure(snapshot.phaseFailure)
                record(snapshot)
                recordCounter("network.request.failed.count")
                recordCounter("network.failure.${NetworkMetricNames.throwable(ioe)}.count")
                recordCounter("network.route.${snapshot.routeKey}.failed.count")
            }
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
                EventListenerNonFatal.bestEffort<CallIdentity?>(null) {
                    val request = call.request()
                    CallIdentity(
                        routeKey = metricRouteKey(request),
                        requestLabel = "${request.method()} ${request.url().encodedPath()}",
                    )
                }?.let { identity ->
                    state {
                        routeKey = identity.routeKey
                        requestLabel = identity.requestLabel
                    }
                }
            }
            if (captureContext) {
                val snapshot = EventListenerNonFatal.bestEffort<JankHunterContextSnapshot?>(null) {
                    telemetry.captureContextSnapshot()
                }
                state { contextSnapshot = snapshot }
            }
        }

        private fun terminalSnapshot(failed: Boolean, throwable: Throwable?): TerminalSnapshot? {
            val endedAt = now()
            return state {
                if (terminalRecorded) return@state null
                terminalRecorded = true
                val phaseFailure = if (failed) claimPhaseFailure(phase, throwable) else null
                TerminalSnapshot(
                    contextSnapshot = contextSnapshot,
                    requestLabel = requestLabel,
                    durationMs = elapsed(startedAt, endedAt),
                    dnsMs = dnsMs,
                    connectMs = connectMs,
                    ttfbMs = ttfbMs,
                    statusClass = statusClass,
                    responseBodyBytes = responseBodyBytes,
                    requestBodyBytes = requestBodyBytes,
                    flags = if (failed) flags or JankHunterNetworkEventFlags.HTTP_FAILED else flags,
                    connectionReleased = connectionReleased,
                    dnsAttemptCount = dnsAttemptCount,
                    connectAttemptCount = connectAttemptCount,
                    tlsAttemptCount = tlsAttemptCount,
                    routeKey = routeKey,
                    phaseFailure = phaseFailure,
                )
            }
        }

        private fun record(snapshot: TerminalSnapshot) {
            telemetry {
                telemetry.recordHttp(
                    snapshot.contextSnapshot,
                    snapshot.requestLabel,
                    snapshot.durationMs,
                    snapshot.dnsMs,
                    snapshot.connectMs,
                    snapshot.ttfbMs,
                    snapshot.statusClass,
                    snapshot.responseBodyBytes,
                    snapshot.requestBodyBytes,
                    snapshot.flags,
                )
            }
            recordGauge("network.request.duration_ms", snapshot.durationMs)
            recordGauge("network.request.dns_attempts", snapshot.dnsAttemptCount.toLong())
            recordGauge("network.request.connect_attempts", snapshot.connectAttemptCount.toLong())
            recordGauge("network.request.tls_attempts", snapshot.tlsAttemptCount.toLong())
            recordGauge(
                "network.request.connection_released",
                if (snapshot.connectionReleased) 1L else 0L,
            )
            if (
                snapshot.dnsAttemptCount > 1 ||
                snapshot.connectAttemptCount > 1 ||
                snapshot.tlsAttemptCount > 1
            ) {
                recordCounter("network.request.retry_or_reconnect.count")
                recordCounter("network.route.${snapshot.routeKey}.retry_or_reconnect.count")
            }
        }

        /** Must be called with [stateLock] held. */
        private fun claimPhaseFailure(failedPhase: String, throwable: Throwable?): PhaseFailure? {
            if (phaseFailureRecorded) return null
            phaseFailureRecorded = true
            return PhaseFailure(failedPhase, routeKey, throwable)
        }

        private fun recordPhaseFailure(failure: PhaseFailure?) {
            if (failure == null) return
            val phaseKey = NetworkMetricNames.owner(failure.phase)
            recordCounter("network.phase.$phaseKey.failure.count")
            recordCounter("network.route.${failure.routeKey}.phase.$phaseKey.failure.count")
            recordCounter(
                "network.phase.$phaseKey.failure.${NetworkMetricNames.throwable(failure.throwable)}.count",
            )
        }

        private fun recordCounter(name: String) {
            telemetry { telemetry.recordCounter(name, 1) }
        }

        private fun recordGauge(name: String, value: Long) {
            telemetry { telemetry.recordGauge(name, value) }
        }

        private fun metricRouteKey(request: Request): String {
            return NetworkMetricNames.route(request.method(), request.url().encodedPath())
        }

        private fun now(): Long {
            return EventListenerNonFatal.bestEffort(UNSET_TIME) {
                clock().takeIf { it >= 0L } ?: UNSET_TIME
            }
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

        private inline fun <T> state(block: () -> T): T = synchronized(stateLock, block)

        private inline fun telemetry(block: () -> Unit) {
            EventListenerNonFatal.bestEffort(Unit, block)
        }

        private companion object {
            private const val UNSET_TIME = -1L
            private const val HTTP_STATUS_CLASS_DIVISOR = 100
            private const val UNKNOWN = "unknown"
            private const val PHASE_CALL = "call"
            private const val PHASE_DNS = "dns"
            private const val PHASE_CONNECT = "connect"
            private const val PHASE_TLS = "tls"
            private const val PHASE_REQUEST = "request"
            private const val PHASE_RESPONSE = "response"
        }
    }

    private data class CallIdentity(
        val routeKey: String,
        val requestLabel: String,
    )

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
    )

    private data class PhaseFailure(
        val phase: String,
        val routeKey: String,
        val throwable: Throwable?,
    )

    @Suppress("LongParameterList")
    private data class TerminalSnapshot(
        val contextSnapshot: JankHunterContextSnapshot?,
        val requestLabel: String,
        val durationMs: Long,
        val dnsMs: Long,
        val connectMs: Long,
        val ttfbMs: Long,
        val statusClass: Int,
        val responseBodyBytes: Long,
        val requestBodyBytes: Long,
        val flags: Long,
        val connectionReleased: Boolean,
        val dnsAttemptCount: Int,
        val connectAttemptCount: Int,
        val tlsAttemptCount: Int,
        val routeKey: String,
        val phaseFailure: PhaseFailure?,
    )

    private enum class ConnectionClassification {
        UNKNOWN,
        NEW,
        REUSED,
    }

}

private object EventListenerNonFatal {
    inline fun <T> bestEffort(fallback: T, block: () -> T): T {
        return try {
            block()
        } catch (throwable: Throwable) {
            if (throwable is VirtualMachineError || throwable is ThreadDeath) throw throwable
            fallback
        }
    }
}
