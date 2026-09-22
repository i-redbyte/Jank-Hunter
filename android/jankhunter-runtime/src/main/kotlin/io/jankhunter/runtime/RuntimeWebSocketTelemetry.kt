package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter

internal class RuntimeWebSocketTelemetry(
    private val access: RuntimeTelemetryAccess,
) {
    fun record(activeWriter: AsyncLogWriter, event: JankHunterWebSocketEvent) {
        val snapshot = event.contextSnapshot
        val context = if (snapshot == null) {
            access.captureContext(ownerOverride = event.owner)
        } else {
            JankHunterContext(snapshot.screen, event.owner ?: snapshot.owner, snapshot.operationId)
        }
        val route = access.config?.redactRoute(event.route) ?: event.route
        val normalized = event.withRoute(route)
        activeWriter.webSocket(context.screen, context.owner, normalized)
        recordWebSocketCounters(activeWriter, context.owner, normalized)
    }

    private fun recordWebSocketCounters(
        activeWriter: AsyncLogWriter,
        owner: String?,
        event: JankHunterWebSocketEvent,
    ) {
        when (event.stage) {
            JankHunterWebSocketEvent.STAGE_OPENED ->
                activeWriter.counter("websocket.open.count", 1)
            JankHunterWebSocketEvent.STAGE_CLOSED -> {
                val closeCode = event.closeCode
                if (closeCode in MIN_WEBSOCKET_CLOSE_CODE..MAX_WEBSOCKET_CLOSE_CODE) {
                    val key = websocketMetricOwnerKey(owner)
                    activeWriter.counter("websocket.$key.close_code.$closeCode.count", 1)
                }
            }
        }
    }

    private companion object {
        const val MIN_WEBSOCKET_CLOSE_CODE = 1_000
        const val MAX_WEBSOCKET_CLOSE_CODE = 4_999
    }

    private fun JankHunterWebSocketEvent.withRoute(safeRoute: String?): JankHunterWebSocketEvent {
        if (safeRoute === route) return this
        return JankHunterWebSocketEvent(
            contextSnapshot,
            safeRoute,
            owner,
            connectionId,
            stage,
            durationMs,
            statusCode,
            closeCode,
            failureKind,
            textMessages,
            binaryMessages,
            receivedBytes,
            reconnectOrdinal,
        )
    }
}
