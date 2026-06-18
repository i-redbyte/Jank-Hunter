package io.jankhunter.okhttp3

import android.os.SystemClock
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.internal.io.BinaryLogWriter
import okhttp3.Call
import okhttp3.Connection
import okhttp3.EventListener
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import java.io.IOException
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.Proxy
import java.util.concurrent.atomic.AtomicLong
import kotlin.math.max

class JankHunterEventListenerFactory(
    private val delegate: EventListener.Factory? = null,
) : EventListener.Factory {
    override fun create(call: Call): EventListener {
        val base = delegate?.create(call) ?: EventListener.NONE
        return Listener(base, nextCallId.incrementAndGet())
    }

    private class Listener(
        private val delegate: EventListener,
        private val callId: Long,
    ) : EventListener() {
        private var startedAt = 0L
        private var dnsStartedAt = 0L
        private var connectStartedAt = 0L
        private var requestFinishedAt = 0L
        private var dnsMs = 0L
        private var connectMs = 0L
        private var ttfbMs = 0L
        private var statusClass = 0
        private var flags = 0L
        private var connectionAcquired = false
        private var connectStarted = false
        private var connectionReleased = false
        private var dnsAttemptCount = 0
        private var connectAttemptCount = 0
        private var tlsAttemptCount = 0
        private var requestBodyBytes = 0L
        private var responseBodyBytes = 0L
        private var phase = "call"
        private var phaseFailureRecorded = false

        override fun callStart(call: Call) {
            startedAt = now()
            val request = call.request()
            val routeKey = routeKey(request)
            JankHunter.recordCounter("network.request.started.count", 1)
            JankHunter.recordCounter("network.route.$routeKey.started.count", 1)
            JankHunter.recordGauge("network.request.last_id", callId)
            delegate.callStart(call)
        }

        override fun dnsStart(call: Call, domainName: String) {
            phase = "dns"
            dnsAttemptCount += 1
            dnsStartedAt = now()
            val routeKey = routeKey(call.request())
            JankHunter.recordCounter("network.dns.lookup.count", 1)
            JankHunter.recordCounter("network.route.$routeKey.dns.lookup.count", 1)
            delegate.dnsStart(call, domainName)
        }

        override fun dnsEnd(call: Call, domainName: String, inetAddressList: List<InetAddress>) {
            dnsMs += elapsed(dnsStartedAt)
            dnsStartedAt = 0L
            delegate.dnsEnd(call, domainName, inetAddressList)
        }

        override fun connectStart(call: Call, inetSocketAddress: InetSocketAddress, proxy: Proxy) {
            phase = "connect"
            connectStarted = true
            connectAttemptCount += 1
            connectStartedAt = now()
            val routeKey = routeKey(call.request())
            JankHunter.recordCounter("network.connect.attempt.count", 1)
            JankHunter.recordCounter("network.route.$routeKey.connect.attempt.count", 1)
            delegate.connectStart(call, inetSocketAddress, proxy)
        }

        override fun secureConnectStart(call: Call) {
            phase = "tls"
            tlsAttemptCount += 1
            flags = flags or BinaryLogWriter.FLAG_HTTP_TLS
            JankHunter.recordCounter("network.tls.handshake.count", 1)
            delegate.secureConnectStart(call)
        }

        override fun connectEnd(
            call: Call,
            inetSocketAddress: InetSocketAddress,
            proxy: Proxy,
            protocol: Protocol?,
        ) {
            connectMs += elapsed(connectStartedAt)
            connectStartedAt = 0L
            delegate.connectEnd(call, inetSocketAddress, proxy, protocol)
        }

        override fun connectFailed(
            call: Call,
            inetSocketAddress: InetSocketAddress,
            proxy: Proxy,
            protocol: Protocol?,
            ioe: IOException,
        ) {
            phase = if (tlsAttemptCount > 0) "tls" else "connect"
            connectMs += elapsed(connectStartedAt)
            connectStartedAt = 0L
            recordPhaseFailure(call, phase, ioe)
            delegate.connectFailed(call, inetSocketAddress, proxy, protocol, ioe)
        }

        override fun connectionAcquired(call: Call, connection: Connection) {
            connectionAcquired = true
            if (!connectStarted) {
                flags = flags or BinaryLogWriter.FLAG_HTTP_REUSED_CONNECTION
                JankHunter.recordCounter("network.request.reused_connection.count", 1)
                JankHunter.recordCounter("network.route.${routeKey(call.request())}.reused_connection.count", 1)
            } else {
                JankHunter.recordCounter("network.request.new_connection.count", 1)
            }
            if (connection.handshake() != null) {
                flags = flags or BinaryLogWriter.FLAG_HTTP_TLS
            }
            delegate.connectionAcquired(call, connection)
        }

        override fun connectionReleased(call: Call, connection: Connection) {
            connectionReleased = true
            delegate.connectionReleased(call, connection)
        }

        override fun requestHeadersEnd(call: Call, request: Request) {
            phase = "request"
            requestFinishedAt = now()
            delegate.requestHeadersEnd(call, request)
        }

        override fun requestBodyEnd(call: Call, byteCount: Long) {
            phase = "request"
            requestBodyBytes = max(0L, byteCount)
            requestFinishedAt = now()
            delegate.requestBodyEnd(call, byteCount)
        }

        override fun responseBodyEnd(call: Call, byteCount: Long) {
            phase = "response"
            responseBodyBytes = max(0L, byteCount)
            delegate.responseBodyEnd(call, byteCount)
        }

        override fun responseHeadersStart(call: Call) {
            phase = "response"
            val responseStartedAt = now()
            val base = if (requestFinishedAt > 0L) requestFinishedAt else startedAt
            ttfbMs = elapsed(base, responseStartedAt)
            delegate.responseHeadersStart(call)
        }

        override fun responseHeadersEnd(call: Call, response: Response) {
            statusClass = response.code() / 100
            delegate.responseHeadersEnd(call, response)
        }

        override fun callEnd(call: Call) {
            record(call, failed = false)
            JankHunter.recordCounter("network.request.finished.count", 1)
            JankHunter.recordCounter("network.route.${routeKey(call.request())}.finished.count", 1)
            delegate.callEnd(call)
        }

        override fun callFailed(call: Call, ioe: IOException) {
            recordPhaseFailure(call, phase, ioe)
            record(call, failed = true)
            JankHunter.recordCounter("network.request.failed.count", 1)
            JankHunter.recordCounter("network.failure.${NetworkMetricNames.throwable(ioe)}.count", 1)
            JankHunter.recordCounter("network.route.${routeKey(call.request())}.failed.count", 1)
            delegate.callFailed(call, ioe)
        }

        private fun record(call: Call, failed: Boolean) {
            val request: Request = call.request()
            val routeKey = routeKey(request)
            val durationMs = elapsed(startedAt)
            var localFlags = flags
            if (connectionAcquired && !connectStarted) {
                localFlags = localFlags or BinaryLogWriter.FLAG_HTTP_REUSED_CONNECTION
            }
            if (failed) {
                localFlags = localFlags or BinaryLogWriter.FLAG_HTTP_FAILED
            }
            JankHunter.recordHttp(
                JankHunter.currentOwner(),
                "${request.method()} ${request.url().encodedPath()}",
                durationMs,
                dnsMs,
                connectMs,
                ttfbMs,
                statusClass,
                responseBodyBytes,
                requestBodyBytes,
                localFlags,
            )
            JankHunter.recordGauge("network.request.duration_ms", durationMs)
            JankHunter.recordGauge("network.request.dns_attempts", dnsAttemptCount.toLong())
            JankHunter.recordGauge("network.request.connect_attempts", connectAttemptCount.toLong())
            JankHunter.recordGauge("network.request.tls_attempts", tlsAttemptCount.toLong())
            JankHunter.recordGauge("network.request.connection_released", if (connectionReleased) 1L else 0L)
            if (dnsAttemptCount > 1 || connectAttemptCount > 1 || tlsAttemptCount > 1) {
                JankHunter.recordCounter("network.request.retry_or_reconnect.count", 1)
                JankHunter.recordCounter("network.route.$routeKey.retry_or_reconnect.count", 1)
            }
        }

        private fun recordPhaseFailure(call: Call, failedPhase: String, throwable: Throwable?) {
            if (phaseFailureRecorded) return
            phaseFailureRecorded = true
            val phaseKey = NetworkMetricNames.owner(failedPhase)
            val routeKey = routeKey(call.request())
            JankHunter.recordCounter("network.phase.$phaseKey.failure.count", 1)
            JankHunter.recordCounter("network.route.$routeKey.phase.$phaseKey.failure.count", 1)
            JankHunter.recordCounter(
                "network.phase.$phaseKey.failure.${NetworkMetricNames.throwable(throwable)}.count",
                1,
            )
        }

        private fun routeKey(request: Request): String {
            return NetworkMetricNames.route(request.method(), request.url().encodedPath())
        }

        private fun now(): Long = SystemClock.elapsedRealtime()

        private fun elapsed(start: Long): Long = elapsed(start, now())

        private fun elapsed(start: Long, end: Long): Long = if (start <= 0) 0 else max(0L, end - start)
    }

    private companion object {
        private val nextCallId = AtomicLong()
    }
}
