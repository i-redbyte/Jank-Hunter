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
import kotlin.math.max

class JankHunterEventListenerFactory(
    private val delegate: EventListener.Factory? = null,
) : EventListener.Factory {
    override fun create(call: Call): EventListener {
        val base = delegate?.create(call) ?: EventListener.NONE
        return Listener(base)
    }

    private class Listener(
        private val delegate: EventListener,
    ) : EventListener() {
        private var startedAt = 0L
        private var dnsStartedAt = 0L
        private var connectStartedAt = 0L
        private var responseStartedAt = 0L
        private var dnsMs = 0L
        private var connectMs = 0L
        private var ttfbMs = 0L
        private var statusClass = 0
        private var flags = 0L
        private var connectionAcquired = false
        private var connectStarted = false
        private var requestBodyBytes = 0L
        private var responseBodyBytes = 0L

        override fun callStart(call: Call) {
            startedAt = now()
            delegate.callStart(call)
        }

        override fun dnsStart(call: Call, domainName: String) {
            dnsStartedAt = now()
            delegate.dnsStart(call, domainName)
        }

        override fun dnsEnd(call: Call, domainName: String, inetAddressList: List<InetAddress>) {
            dnsMs = elapsed(dnsStartedAt)
            delegate.dnsEnd(call, domainName, inetAddressList)
        }

        override fun connectStart(call: Call, inetSocketAddress: InetSocketAddress, proxy: Proxy) {
            connectStarted = true
            connectStartedAt = now()
            delegate.connectStart(call, inetSocketAddress, proxy)
        }

        override fun secureConnectStart(call: Call) {
            flags = flags or BinaryLogWriter.FLAG_HTTP_TLS
            delegate.secureConnectStart(call)
        }

        override fun connectEnd(
            call: Call,
            inetSocketAddress: InetSocketAddress,
            proxy: Proxy,
            protocol: Protocol?,
        ) {
            connectMs = elapsed(connectStartedAt)
            delegate.connectEnd(call, inetSocketAddress, proxy, protocol)
        }

        override fun connectionAcquired(call: Call, connection: Connection) {
            connectionAcquired = true
            if (!connectStarted) {
                flags = flags or BinaryLogWriter.FLAG_HTTP_REUSED_CONNECTION
            }
            if (connection.handshake() != null) {
                flags = flags or BinaryLogWriter.FLAG_HTTP_TLS
            }
            delegate.connectionAcquired(call, connection)
        }

        override fun requestBodyEnd(call: Call, byteCount: Long) {
            requestBodyBytes = max(0L, byteCount)
            delegate.requestBodyEnd(call, byteCount)
        }

        override fun responseBodyEnd(call: Call, byteCount: Long) {
            responseBodyBytes = max(0L, byteCount)
            delegate.responseBodyEnd(call, byteCount)
        }

        override fun responseHeadersStart(call: Call) {
            responseStartedAt = now()
            delegate.responseHeadersStart(call)
        }

        override fun responseHeadersEnd(call: Call, response: Response) {
            ttfbMs = elapsed(responseStartedAt)
            statusClass = response.code() / 100
            delegate.responseHeadersEnd(call, response)
        }

        override fun callEnd(call: Call) {
            record(call, failed = false)
            delegate.callEnd(call)
        }

        override fun callFailed(call: Call, ioe: IOException) {
            record(call, failed = true)
            delegate.callFailed(call, ioe)
        }

        private fun record(call: Call, failed: Boolean) {
            val request: Request = call.request()
            var localFlags = flags or BinaryLogWriter.FLAG_APP_FOREGROUND
            if (connectionAcquired && !connectStarted) {
                localFlags = localFlags or BinaryLogWriter.FLAG_HTTP_REUSED_CONNECTION
            }
            if (failed) {
                localFlags = localFlags or BinaryLogWriter.FLAG_HTTP_FAILED
            }
            JankHunter.recordHttp(
                JankHunter.currentOwner(),
                "${request.method()} ${request.url().encodedPath()}",
                elapsed(startedAt),
                dnsMs,
                connectMs,
                ttfbMs,
                statusClass,
                responseBodyBytes,
                requestBodyBytes,
                localFlags,
            )
        }

        private fun now(): Long = SystemClock.elapsedRealtime()

        private fun elapsed(start: Long): Long = if (start <= 0) 0 else max(0L, now() - start)
    }
}
