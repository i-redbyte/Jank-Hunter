package io.jankhunter.okhttp3

import android.os.SystemClock
import io.jankhunter.runtime.JankHunterNetworkRuntime
import io.jankhunter.runtime.JankHunterContextSnapshot
import io.jankhunter.runtime.JankHunterWebSocketEvent
import java.io.IOException
import java.net.ConnectException
import java.net.ProtocolException
import java.net.SocketTimeoutException
import java.net.UnknownHostException
import java.util.LinkedHashMap
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong
import javax.net.ssl.SSLException
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString

class JankHunterWebSocketListener private constructor(
    private val owner: String?,
    route: String?,
    private val delegate: WebSocketListener?,
    private val telemetrySink: NetworkTelemetry,
    private val clock: NetworkLongSource,
    telemetryEnabled: NetworkBooleanSource,
) : WebSocketListener() {
    constructor() : this(owner = null, route = null, delegate = null)

    constructor(
        owner: String? = null,
        route: String? = null,
        delegate: WebSocketListener? = null,
    ) : this(
        owner = owner,
        route = route,
        delegate = delegate,
        telemetrySink = RuntimeNetworkTelemetry.INSTANCE,
        clock = NetworkLongSource { SystemClock.elapsedRealtime() },
        telemetryEnabled = NetworkBooleanSource { JankHunterNetworkRuntime.isWebSocketActive() },
    )

    private val collectTelemetry = nonFatalBoolean(false, telemetryEnabled)
    private val connectionId = if (collectTelemetry) nextConnectionId.getAndIncrement().coerceAtLeast(1L) else 0L
    private val createdAt = if (collectTelemetry) now() else UNSET_TIME
    private val terminalRecorded = AtomicBoolean()
    private val listenerIdentity = System.identityHashCode(delegate ?: this)

    private var routeLabel: String? = route
    private var reconnectKey: ReconnectKey? = null
    private var contextSnapshot: JankHunterContextSnapshot? = null
    private var openedAt = UNSET_TIME
    private var statusCode = 0
    private var reconnectOrdinal = 0
    private var textMessages = 0L
    private var binaryMessages = 0L
    private var receivedBytes = 0L

    override fun onOpen(webSocket: WebSocket, response: Response) {
        if (collectTelemetry) {
            val opened = now()
            openedAt = opened
            statusCode = response.code().takeIf { it in 100..599 } ?: 0
            routeLabel = responseRoute(response) ?: routeLabel
            reconnectOrdinal = reconnectTracker.consumeFailure(currentReconnectKey()) ?: 0
            val ordinal = reconnectOrdinal
            record {
                val snapshot = contextSnapshot ?: telemetrySink.captureContextSnapshot().also { contextSnapshot = it }
                telemetrySink.recordWebSocket(
                    JankHunterWebSocketEvent(
                        snapshot,
                        routeLabel,
                        owner,
                        connectionId,
                        JankHunterWebSocketEvent.STAGE_OPENED,
                        elapsed(createdAt, opened),
                        statusCode,
                        0,
                        JankHunterWebSocketEvent.FAILURE_UNKNOWN,
                        0L,
                        0L,
                        0L,
                        ordinal,
                    ),
                )
            }
        }
        delegate?.onOpen(webSocket, response)
    }

    override fun onMessage(webSocket: WebSocket, text: String) {
        collect {
            textMessages = saturatedAdd(textMessages, 1L)
            receivedBytes = saturatedAdd(receivedBytes, utf8Size(text))
        }
        delegate?.onMessage(webSocket, text)
    }

    override fun onMessage(webSocket: WebSocket, bytes: ByteString) {
        collect {
            binaryMessages = saturatedAdd(binaryMessages, 1L)
            receivedBytes = saturatedAdd(receivedBytes, bytes.size().toLong())
        }
        delegate?.onMessage(webSocket, bytes)
    }

    override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
        delegate?.onClosing(webSocket, code, reason)
    }

    override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
        terminal(
            stage = JankHunterWebSocketEvent.STAGE_CLOSED,
            response = null,
            closeCode = code.takeIf { it in 1000..4999 } ?: 0,
            failureKind = JankHunterWebSocketEvent.FAILURE_UNKNOWN,
        )
        delegate?.onClosed(webSocket, code, reason)
    }

    override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
        terminal(
            stage = JankHunterWebSocketEvent.STAGE_FAILED,
            response = response,
            closeCode = 0,
            failureKind = failureKind(t),
        )
        delegate?.onFailure(webSocket, t, response)
    }

    private fun terminal(stage: Int, response: Response?, closeCode: Int, failureKind: Int) {
        if (!collectTelemetry || !terminalRecorded.compareAndSet(false, true)) return
        val endedAt = now()
        if (response != null) {
            statusCode = response.code().takeIf { it in 100..599 } ?: statusCode
            routeLabel = responseRoute(response) ?: routeLabel
        }
        if (stage == JankHunterWebSocketEvent.STAGE_FAILED) {
            reconnectTracker.markFailure(currentReconnectKey(), saturatedIncrement(reconnectOrdinal))
        }
        try {
            record {
                val snapshot = contextSnapshot ?: telemetrySink.captureContextSnapshot().also { contextSnapshot = it }
                telemetrySink.recordWebSocket(
                    JankHunterWebSocketEvent(
                        snapshot,
                        routeLabel,
                        owner,
                        connectionId,
                        stage,
                        elapsed(openedAt.takeIf { it != UNSET_TIME } ?: createdAt, endedAt),
                        statusCode,
                        closeCode,
                        failureKind,
                        textMessages,
                        binaryMessages,
                        receivedBytes,
                        reconnectOrdinal,
                    ),
                )
            }
        } finally {
            contextSnapshot = null
            reconnectKey = null
            routeLabel = null
        }
    }

    private fun currentReconnectKey(): ReconnectKey? {
        reconnectKey?.let { return it }
        val route = routeLabel ?: return null
        return ReconnectKey(owner, route, listenerIdentity).also { reconnectKey = it }
    }

    private fun responseRoute(response: Response): String? = nonFatalOr(null) {
        val request = response.request()
        NetworkMetricNames.route(request.method(), request.url().encodedPath())
    }

    private fun now(): Long {
        val value = nonFatalLong(UNSET_TIME, clock)
        return value.takeIf { it >= 0L } ?: UNSET_TIME
    }

    /** The connection snapshots collection state once, avoiding partial lifecycle events. */
    private inline fun collect(block: () -> Unit) {
        if (!collectTelemetry) return
        nonFatal(block)
    }

    private inline fun record(block: () -> Unit) {
        if (!collectTelemetry) return
        nonFatal(block)
    }

    private companion object {
        private const val UNSET_TIME = -1L
        private val nextConnectionId = AtomicLong(1L)
        private val reconnectTracker = ReconnectTracker()

        private fun elapsed(start: Long, end: Long): Long {
            if (start == UNSET_TIME || end == UNSET_TIME) return 0L
            return (end - start).coerceAtLeast(0L)
        }

        private fun saturatedIncrement(value: Int): Int = if (value == Int.MAX_VALUE) value else value + 1

        private fun saturatedAdd(current: Long, delta: Long): Long {
            if (delta <= 0L) return current
            return if (Long.MAX_VALUE - current < delta) Long.MAX_VALUE else current + delta
        }

        private fun failureKind(throwable: Throwable): Int {
            return when (throwable) {
                is SocketTimeoutException -> JankHunterWebSocketEvent.FAILURE_TIMEOUT
                is ConnectException, is UnknownHostException -> JankHunterWebSocketEvent.FAILURE_CONNECTION
                is SSLException -> JankHunterWebSocketEvent.FAILURE_TLS
                is ProtocolException -> JankHunterWebSocketEvent.FAILURE_PROTOCOL
                is IOException -> JankHunterWebSocketEvent.FAILURE_IO
                else -> JankHunterWebSocketEvent.FAILURE_OTHER
            }
        }

        private fun utf8Size(value: String): Long {
            var result = 0L
            var index = 0
            while (index < value.length) {
                val code = value[index].code
                when {
                    code < 0x80 -> result++
                    code < 0x800 -> result += 2L
                    code !in 0xd800..0xdfff -> result += 3L
                    code <= 0xdbff && index + 1 < value.length && value[index + 1].code in 0xdc00..0xdfff -> {
                        result += 4L
                        index++
                    }
                    else -> result++
                }
                index++
            }
            return result
        }

        private inline fun nonFatal(block: () -> Unit) {
            try {
                block()
            } catch (throwable: Throwable) {
                throwable.rethrowIfFatal()
            }
        }

        private inline fun <T> nonFatalOr(fallback: T, block: () -> T): T {
            return try {
                block()
            } catch (throwable: Throwable) {
                throwable.rethrowIfFatal()
                fallback
            }
        }

        private fun nonFatalLong(fallback: Long, supplier: NetworkLongSource): Long {
            return try {
                supplier.getAsLong()
            } catch (throwable: Throwable) {
                throwable.rethrowIfFatal()
                fallback
            }
        }

        private fun nonFatalBoolean(fallback: Boolean, supplier: NetworkBooleanSource): Boolean {
            return try {
                supplier.getAsBoolean()
            } catch (throwable: Throwable) {
                throwable.rethrowIfFatal()
                fallback
            }
        }

        private fun Throwable.rethrowIfFatal() {
            if (this is VirtualMachineError || this is ThreadDeath) throw this
        }
    }

    /** Незавершённые переподключения хранятся между обёртками и жёстко ограничены по количеству. */
    private class ReconnectTracker {
        private val pendingOrdinals = LinkedHashMap<ReconnectKey, Int>(16, 0.75f, true)

        fun markFailure(key: ReconnectKey?, nextOrdinal: Int) {
            if (key == null) return
            synchronized(pendingOrdinals) {
                pendingOrdinals[key] = maxOf(pendingOrdinals[key] ?: 0, nextOrdinal)
                while (pendingOrdinals.size > MAX_TRACKED_SOCKETS) {
                    val iterator = pendingOrdinals.entries.iterator()
                    if (!iterator.hasNext()) break
                    iterator.next()
                    iterator.remove()
                }
            }
        }

        fun consumeFailure(key: ReconnectKey?): Int? {
            if (key == null) return null
            return synchronized(pendingOrdinals) { pendingOrdinals.remove(key) }
        }

        private companion object {
            private const val MAX_TRACKED_SOCKETS = 256
        }
    }

    private data class ReconnectKey(
        val owner: String?,
        val route: String,
        val listenerIdentity: Int,
    )
}
