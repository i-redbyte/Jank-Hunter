package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterHttpEvent
import io.jankhunter.runtime.JankHunterWebSocketEvent

internal class PendingHttpEvent(
    producerContext: LogEventContext?,
    private val owner: String?,
    private val route: String?,
    private val event: JankHunterHttpEvent,
    private val flags: Long,
) : PendingLogEvent(Jhlog.TYPE_HTTP, producerContext) {
    override fun writePayload(writer: BinaryLogWriter) {
        writer.http(owner, route, event, flags)
    }
}

internal class PendingWebSocketEvent(
    producerContext: LogEventContext?,
    private val owner: String?,
    private val event: JankHunterWebSocketEvent,
) : PendingLogEvent(Jhlog.TYPE_WEBSOCKET, producerContext) {
    override fun writePayload(writer: BinaryLogWriter) {
        writer.webSocket(owner, event)
    }
}
