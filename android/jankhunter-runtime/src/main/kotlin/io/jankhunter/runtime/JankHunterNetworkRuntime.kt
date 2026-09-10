package io.jankhunter.runtime

/** Narrow runtime port for optional network adapters. */
object JankHunterNetworkRuntime {
    @JvmStatic
    fun isActive(): Boolean = JankHunter.networkTelemetry().isActive()

    @JvmStatic
    fun isHttpActive(): Boolean = JankHunter.networkTelemetry().isHttpActive()

    @JvmStatic
    fun isWebSocketActive(): Boolean = JankHunter.networkTelemetry().isWebSocketActive()

    @JvmStatic
    fun captureContext(): JankHunterContextSnapshot = JankHunter.networkTelemetry().captureContext()

    @JvmStatic
    fun recordHttp(event: JankHunterHttpEvent) = JankHunter.networkTelemetry().recordHttp(event)

    @JvmStatic
    fun recordWebSocket(event: JankHunterWebSocketEvent) = JankHunter.networkTelemetry().recordWebSocket(event)

    @JvmStatic
    fun counter(name: String, delta: Long) = JankHunter.networkTelemetry().counter(name, delta)
}
