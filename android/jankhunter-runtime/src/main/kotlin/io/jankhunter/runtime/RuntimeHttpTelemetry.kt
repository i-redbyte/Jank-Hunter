package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter

internal class RuntimeHttpTelemetry(
    private val access: RuntimeTelemetryAccess,
) {
    fun record(activeWriter: AsyncLogWriter, event: JankHunterHttpEvent, config: JankHunterConfig? = access.config) {
        val context = event.contextSnapshot?.asRuntimeContext() ?: access.captureContext()
        val classifiedFlags = event.flags or JankHunterNetworkEventFlags.HTTP_CLASSIFIED
        val thresholdMs = config?.httpSlowThresholdMs() ?: DEFAULT_HTTP_SLOW_THRESHOLD_MS
        val flags = if (event.durationMs >= thresholdMs) {
            classifiedFlags or JankHunterNetworkEventFlags.HTTP_SLOW
        } else {
            classifiedFlags
        }
        activeWriter.http(
            context.screen,
            context.owner,
            config?.redactRoute(event.requestLabel) ?: event.requestLabel,
            event,
            flags or access.uiVisibleFlag(),
        )
        activeWriter.counter("network.request.started.count", 1)
        if (httpEventFailed(event, flags)) {
            activeWriter.counter("network.request.failed.count", 1)
        }
    }

    private fun httpEventFailed(event: JankHunterHttpEvent, flags: Long): Boolean {
        if (flags and JankHunterNetworkEventFlags.HTTP_FAILED != 0L) return true
        val status = event.statusCode
        return status in 500..599
    }

    private companion object {
        const val DEFAULT_HTTP_SLOW_THRESHOLD_MS = 1_000L
    }
}
