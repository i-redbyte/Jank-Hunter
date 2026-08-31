package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterHttpEvent
import io.jankhunter.runtime.JankHunterWebSocketEvent

/** Encodes network domains while the sink owns synchronization, symbols, and transport. */
internal class NetworkBinaryRecordEncoder(
    private val sink: BinaryEncodingSink,
) {
    fun http(owner: String?, route: String?, event: JankHunterHttpEvent, flags: Long) {
        val safeDurationMs = nonNegative(event.durationMs)
        val initiator = event.contextSnapshot
        if (initiator?.initiatorPresent == true) {
            sink.defineStableSymbol(initiator.initiatorId, initiator.initiatorName)
        }
        val payload = sink.payload()
            .symbolRef(sink.optionalSymbolId(BinaryLogWriter.DICT_ROUTE, route))
            .symbolRef(sink.optionalSymbolId(BinaryLogWriter.DICT_GENERIC, event.serviceAlias))
        if (initiator?.initiatorPresent == true) {
            payload.stableSymbolRef(initiator.initiatorId)
        } else {
            payload.symbolRef(0L)
        }
        val safeAttempts = event.attempts.coerceIn(0, MAX_HTTP_COUNT)
        val safeConnectAttempts = event.connectAttempts.coerceIn(0, MAX_HTTP_COUNT)
        val safeTlsAttempts = event.tlsAttempts.coerceIn(0, MAX_HTTP_COUNT)
        payload
            .uvarint(safeDurationMs)
            .uvarint(clampDuration(event.queueMs, safeDurationMs))
            .uvarint(clampDuration(event.dnsMs, safeDurationMs))
            .uvarint(clampDuration(event.connectMs, safeDurationMs))
            .uvarint(clampDuration(event.tlsMs, safeDurationMs))
            .uvarint(clampDuration(event.requestMs, safeDurationMs))
            .uvarint(clampDuration(event.ttfbMs, safeDurationMs))
            .uvarint(clampDuration(event.responseMs, safeDurationMs))
            .uvarint(event.statusCode.takeIf { it in MIN_HTTP_STATUS..MAX_HTTP_STATUS }?.toLong() ?: 0L)
            .uvarint(event.failurePhase.coerceIn(0, Jhlog.HTTP_FAILURE_PHASE_CANCELLED.toInt()).toLong())
            .uvarint(event.failureKind.coerceIn(0, Jhlog.HTTP_FAILURE_KIND_OTHER.toInt()).toLong())
            .uvarint(event.protocol.coerceIn(0, Jhlog.HTTP_PROTOCOL_3.toInt()).toLong())
            .uvarint(nonNegative(event.responseBodyBytes))
            .uvarint(nonNegative(event.requestBodyBytes))
            .uvarint(safeAttempts.toLong())
            .uvarint(event.dnsAttempts.coerceIn(0, MAX_HTTP_COUNT).toLong())
            .uvarint(safeConnectAttempts.toLong())
            .uvarint(safeTlsAttempts.toLong())
            .uvarint(event.connectFailures.coerceIn(0, safeConnectAttempts).toLong())
            .uvarint(event.tlsFailures.coerceIn(0, safeTlsAttempts).toLong())
            .uvarint(event.redirects.coerceIn(0, safeAttempts).toLong())
        sink.emit(Jhlog.TYPE_HTTP, flags, payload, sink.producerContext(owner))
    }

    fun webSocket(owner: String?, event: JankHunterWebSocketEvent) {
        val payload = sink.payload()
            .symbolRef(sink.optionalSymbolId(BinaryLogWriter.DICT_ROUTE, event.route))
            .uvarint(nonNegative(event.connectionId))
            .uvarint(
                event.stage.coerceIn(
                    Jhlog.WEBSOCKET_STAGE_OPENED.toInt(),
                    Jhlog.WEBSOCKET_STAGE_FAILED.toInt(),
                ).toLong(),
            )
            .uvarint(nonNegative(event.durationMs))
            .uvarint(event.statusCode.takeIf { it in MIN_HTTP_STATUS..MAX_HTTP_STATUS }?.toLong() ?: 0L)
            .uvarint(
                event.closeCode.takeIf {
                    it in MIN_WEBSOCKET_CLOSE_CODE..MAX_WEBSOCKET_CLOSE_CODE
                }?.toLong() ?: 0L,
            )
            .uvarint(
                event.failureKind.coerceIn(
                    Jhlog.WEBSOCKET_FAILURE_UNKNOWN.toInt(),
                    Jhlog.WEBSOCKET_FAILURE_OTHER.toInt(),
                ).toLong(),
            )
            .uvarint(nonNegative(event.textMessages))
            .uvarint(nonNegative(event.binaryMessages))
            .uvarint(nonNegative(event.receivedBytes))
            .uvarint(event.reconnectOrdinal.coerceAtLeast(0).toLong())
        sink.emit(Jhlog.TYPE_WEBSOCKET, 0L, payload, sink.producerContext(owner))
    }

    private fun nonNegative(value: Long): Long = value.coerceAtLeast(0L)

    private fun clampDuration(value: Long, durationMs: Long): Long = nonNegative(value).coerceAtMost(durationMs)

    private companion object {
        const val MIN_HTTP_STATUS = 100
        const val MAX_HTTP_STATUS = 599
        const val MAX_HTTP_COUNT = 65_535
        const val MIN_WEBSOCKET_CLOSE_CODE = 1_000
        const val MAX_WEBSOCKET_CLOSE_CODE = 4_999
    }
}
