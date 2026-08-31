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
        activeWriter.webSocket(context.screen, context.owner, event.withRoute(route))
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
