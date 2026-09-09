package io.jankhunter.runtime

/** Process runtime port retained by optional network adapters, without exposing the full graph. */
internal class RuntimeNetworkAdapterTelemetry(
    private val access: RuntimeTelemetryAccess,
    private val context: RuntimeContextTelemetry,
    private val http: RuntimeHttpTelemetry,
    private val webSocket: RuntimeWebSocketTelemetry,
    private val system: RuntimeSystemTelemetry,
) {
    fun isActive(): Boolean = access.isActive()

    fun isHttpActive(): Boolean =
        access.isActive() && access.config?.isRuntimeFeatureEnabled(JankHunterRuntimeFeature.HTTP) == true

    fun isWebSocketActive(): Boolean =
        access.isActive() && access.config?.isRuntimeFeatureEnabled(JankHunterRuntimeFeature.WEBSOCKETS) == true

    fun captureContext(): JankHunterContextSnapshot = context.captureSnapshot()

    fun recordHttp(event: JankHunterHttpEvent) {
        http.record(access.writer ?: return, event)
    }

    fun recordWebSocket(event: JankHunterWebSocketEvent) {
        webSocket.record(access.writer ?: return, event)
    }

    fun counter(name: String, delta: Long) = system.recordCounter(name, delta)
}
