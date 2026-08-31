package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter

internal class RuntimeHttpTelemetry(
    private val access: RuntimeTelemetryAccess,
) {
    fun record(activeWriter: AsyncLogWriter, event: JankHunterHttpEvent) {
        val context = event.contextSnapshot?.asRuntimeContext() ?: access.captureContext()
        val classifiedFlags = event.flags or JankHunterNetworkEventFlags.HTTP_CLASSIFIED
        val thresholdMs = access.config?.httpSlowThresholdMs() ?: DEFAULT_HTTP_SLOW_THRESHOLD_MS
        val flags = if (event.durationMs >= thresholdMs) {
            classifiedFlags or JankHunterNetworkEventFlags.HTTP_SLOW
        } else {
            classifiedFlags
        }
        activeWriter.http(
            context.screen,
            context.owner,
            access.config?.redactRoute(event.requestLabel) ?: event.requestLabel,
            event,
            flags or access.uiVisibleFlag(),
        )
    }

    private companion object {
        const val DEFAULT_HTTP_SLOW_THRESHOLD_MS = 1_000L
    }
}
