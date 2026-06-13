package io.jankhunter.okhttp3

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
    override fun onOpen(webSocket: WebSocket, response: Response) {
        JankHunter.recordCounter(metric("open.count"), 1)
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
        delegate?.onClosing(webSocket, code, reason)
    }

    override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
        JankHunter.recordCounter(metric("closed.count"), 1)
        delegate?.onClosed(webSocket, code, reason)
    }

    override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
        JankHunter.recordCounter(metric("failure.count"), 1)
        delegate?.onFailure(webSocket, t, response)
    }

    private fun metric(name: String): String {
        val prefix = owner?.takeIf { it.isNotEmpty() } ?: route?.takeIf { it.isNotEmpty() } ?: "unknown"
        return "websocket.$prefix.$name"
    }
}
