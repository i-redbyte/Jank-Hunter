package io.jankhunter.okhttp3

import android.os.SystemClock
import io.jankhunter.runtime.JankHunter
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString

class JankHunterWebSocketListener(
    private val owner: String? = null,
    private val route: String? = null,
    private val delegate: WebSocketListener? = null,
) : WebSocketListener() {
    private var openedAt = 0L
    private var openedCount = 0
    private var failureCount = 0

    override fun onOpen(webSocket: WebSocket, response: Response) {
        openedAt = now()
        openedCount += 1
        JankHunter.recordCounter(metric("open.count"), 1)
        JankHunter.recordCounter("websocket.open.count", 1)
        JankHunter.recordCounter(metric("response_code.${NetworkMetricNames.statusCode(response.code())}.count"), 1)
        if (openedCount > 1 || failureCount > 0) {
            JankHunter.recordCounter(metric("reconnect.count"), 1)
            JankHunter.recordCounter("websocket.reconnect.count", 1)
        }
        delegate?.onOpen(webSocket, response)
    }

    override fun onMessage(webSocket: WebSocket, text: String) {
        JankHunter.recordCounter(metric("message.text.count"), 1)
        JankHunter.recordGauge(metric("message.text.bytes"), text.toByteArray(Charsets.UTF_8).size.toLong())
        delegate?.onMessage(webSocket, text)
    }

    override fun onMessage(webSocket: WebSocket, bytes: ByteString) {
        JankHunter.recordCounter(metric("message.binary.count"), 1)
        JankHunter.recordGauge(metric("message.binary.bytes"), bytes.size().toLong())
        delegate?.onMessage(webSocket, bytes)
    }

    override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
        JankHunter.recordCounter(metric("closing.count"), 1)
        JankHunter.recordCounter(metric("close_code.${NetworkMetricNames.closeCode(code)}.count"), 1)
        delegate?.onClosing(webSocket, code, reason)
    }

    override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
        JankHunter.recordCounter(metric("closed.count"), 1)
        JankHunter.recordCounter(metric("close_code.${NetworkMetricNames.closeCode(code)}.count"), 1)
        recordLifetime()
        delegate?.onClosed(webSocket, code, reason)
    }

    override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
        failureCount += 1
        JankHunter.recordCounter(metric("failure.count"), 1)
        JankHunter.recordCounter("websocket.failure.count", 1)
        JankHunter.recordCounter(metric("failure.${NetworkMetricNames.throwable(t)}.count"), 1)
        response?.let {
            JankHunter.recordCounter(metric("response_code.${NetworkMetricNames.statusCode(it.code())}.count"), 1)
        }
        recordLifetime()
        delegate?.onFailure(webSocket, t, response)
    }

    private fun metric(name: String): String {
        val prefix = NetworkMetricNames.webSocket(owner, route)
        return "websocket.$prefix.$name"
    }

    private fun recordLifetime() {
        val start = openedAt
        if (start > 0) {
            JankHunter.recordGauge(metric("lifetime_ms"), (now() - start).coerceAtLeast(0L))
        }
    }

    private fun now(): Long = SystemClock.elapsedRealtime()
}
