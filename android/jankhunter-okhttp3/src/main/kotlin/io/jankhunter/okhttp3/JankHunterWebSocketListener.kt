package io.jankhunter.okhttp3

import android.os.SystemClock
import io.jankhunter.runtime.JankHunter
import java.util.LinkedHashMap
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString

class JankHunterWebSocketListener private constructor(
    owner: String?,
    route: String?,
    private val delegate: WebSocketListener?,
    private val telemetrySink: NetworkTelemetry,
    private val clock: () -> Long,
    private val telemetryEnabled: () -> Boolean,
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
        clock = SystemClock::elapsedRealtime,
        telemetryEnabled = { JankHunter.isStarted() && JankHunter.isRuntimeEnabled() },
    )

    private val metricPrefix = "websocket.${NetworkMetricNames.webSocket(owner, route)}."
    private val reconnectKey = ReconnectKey(
        metricPrefix = metricPrefix,
        listenerIdentity = System.identityHashCode(delegate ?: this),
    )
    private val openedCount = AtomicInteger()
    private val closeCodeRecorded = AtomicBoolean()

    @Volatile
    private var openedAt = UNSET_TIME

    override fun onOpen(webSocket: WebSocket, response: Response) {
        openedAt = now()
        closeCodeRecorded.set(false)
        val reopenedListener = openedCount.getAndIncrement() > 0
        val followsFailedSocket = reconnectTracker.consumeFailure(reconnectKey)
        telemetry {
            telemetrySink.recordCounter(metric("open.count"), 1)
            telemetrySink.recordCounter("websocket.open.count", 1)
            telemetrySink.recordCounter(
                metric("response_code.${NetworkMetricNames.statusCode(response.code())}.count"),
                1,
            )
            if (reopenedListener || followsFailedSocket) {
                telemetrySink.recordCounter(metric("reconnect.count"), 1)
                telemetrySink.recordCounter("websocket.reconnect.count", 1)
            }
        }
        delegate?.onOpen(webSocket, response)
    }

    override fun onMessage(webSocket: WebSocket, text: String) {
        telemetry {
            telemetrySink.recordCounter(metric("message.text.count"), 1)
            // Keep this inside the enabled boundary: walking a large String is avoidable host-app work.
            telemetrySink.recordGauge(metric("message.text.bytes"), utf8Size(text))
        }
        delegate?.onMessage(webSocket, text)
    }

    override fun onMessage(webSocket: WebSocket, bytes: ByteString) {
        telemetry {
            telemetrySink.recordCounter(metric("message.binary.count"), 1)
            telemetrySink.recordGauge(metric("message.binary.bytes"), bytes.size().toLong())
        }
        delegate?.onMessage(webSocket, bytes)
    }

    override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
        telemetry {
            telemetrySink.recordCounter(metric("closing.count"), 1)
            recordCloseCodeOnce(code)
        }
        delegate?.onClosing(webSocket, code, reason)
    }

    override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
        telemetry {
            telemetrySink.recordCounter(metric("closed.count"), 1)
            recordCloseCodeOnce(code)
            recordLifetime()
        }
        delegate?.onClosed(webSocket, code, reason)
    }

    override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
        // State is updated before fallible telemetry and before the delegate can start a replacement socket.
        reconnectTracker.markFailure(reconnectKey)
        telemetry {
            telemetrySink.recordCounter(metric("failure.count"), 1)
            telemetrySink.recordCounter("websocket.failure.count", 1)
            telemetrySink.recordCounter(metric("failure.${NetworkMetricNames.throwable(t)}.count"), 1)
            response?.let {
                telemetrySink.recordCounter(
                    metric("response_code.${NetworkMetricNames.statusCode(it.code())}.count"),
                    1,
                )
            }
            recordLifetime()
        }
        delegate?.onFailure(webSocket, t, response)
    }

    private fun metric(name: String): String = metricPrefix + name

    private fun recordCloseCodeOnce(code: Int) {
        if (closeCodeRecorded.compareAndSet(false, true)) {
            telemetrySink.recordCounter(metric("close_code.${NetworkMetricNames.closeCode(code)}.count"), 1)
        }
    }

    private fun recordLifetime() {
        val start = openedAt
        val end = now()
        if (start != UNSET_TIME && end != UNSET_TIME) {
            telemetrySink.recordGauge(metric("lifetime_ms"), (end - start).coerceAtLeast(0L))
        }
    }

    private fun now(): Long {
        return nonFatalOr(UNSET_TIME) {
            clock().takeIf { it >= 0L } ?: UNSET_TIME
        }
    }

    private inline fun telemetry(block: () -> Unit) {
        if (!nonFatalOr(false, telemetryEnabled)) return
        nonFatal(block)
    }

    private companion object {
        private const val UNSET_TIME = -1L
        private val reconnectTracker = ReconnectTracker()

        private fun utf8Size(value: String): Long {
            var result = 0L
            var index = 0
            while (index < value.length) {
                val code = value[index].code
                when {
                    code < 0x80 -> result++
                    code < 0x800 -> result += 2L
                    code !in 0xd800..0xdfff -> result += 3L
                    code <= 0xdbff &&
                        index + 1 < value.length &&
                        value[index + 1].code in 0xdc00..0xdfff -> {
                        result += 4L
                        index++
                    }
                    // Okio 1.x writes '?' (one byte) for an unpaired UTF-16 surrogate.
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

        private fun Throwable.rethrowIfFatal() {
            if (this is VirtualMachineError || this is ThreadDeath) throw this
        }
    }

    /**
     * Reconnects happen through a new listener wrapper, so a failure must outlive one wrapper.
     * Only pending failures are retained and the access-ordered map has a hard bound.
     */
    private class ReconnectTracker {
        private val pendingFailures = LinkedHashMap<ReconnectKey, Int>(16, 0.75f, true)

        fun markFailure(key: ReconnectKey) {
            synchronized(pendingFailures) {
                val current = pendingFailures[key] ?: 0
                pendingFailures[key] = if (current == Int.MAX_VALUE) current else current + 1
                while (pendingFailures.size > MAX_TRACKED_SOCKETS) {
                    val eldest = pendingFailures.entries.iterator()
                    if (!eldest.hasNext()) break
                    eldest.next()
                    eldest.remove()
                }
            }
        }

        fun consumeFailure(key: ReconnectKey): Boolean {
            synchronized(pendingFailures) {
                val current = pendingFailures[key] ?: return false
                if (current <= 1) {
                    pendingFailures.remove(key)
                } else {
                    pendingFailures[key] = current - 1
                }
                return true
            }
        }

        private companion object {
            private const val MAX_TRACKED_SOCKETS = 256
        }
    }

    private data class ReconnectKey(
        val metricPrefix: String,
        val listenerIdentity: Int,
    )
}
