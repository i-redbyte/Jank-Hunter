package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter

internal class RuntimeHttpTelemetry(
    private val access: RuntimeTelemetryAccess,
    private val concurrency: RuntimeConcurrencyTelemetry? = null,
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
            if (config == null) event.requestLabel else config.redactRoute(event.requestLabel),
            event,
            flags or access.uiVisibleFlag(),
        )
        concurrency?.recordCost(
            category = RuntimeCausalOperationKind.HTTP,
            durationNanos = httpCausalDurationNanos(event.durationMs),
            readBytes = httpCausalBytes(event.responseBodyBytes, event.flags,
                JankHunterNetworkEventFlags.HTTP_RESPONSE_BYTES_KNOWN),
            writeBytes = httpCausalBytes(event.requestBodyBytes, event.flags,
                JankHunterNetworkEventFlags.HTTP_REQUEST_BYTES_KNOWN),
            sourceSiteId = 0L,
            sourceMethod = null,
            sourceLine = 0,
            sourceOrigin = 0,
            parent = event.contextSnapshot?.causalContext,
        )
    }

    private companion object {
        const val DEFAULT_HTTP_SLOW_THRESHOLD_MS = 1_000L
    }
}

/** Older adapter totals may describe only the final exchange, rather than the whole Call. */
internal fun httpCausalBytes(bytes: Long, flags: Long, knownFlag: Long): Long =
    if (bytes >= 0L && flags and knownFlag != 0L &&
        flags and JankHunterNetworkEventFlags.HTTP_BODY_TOTALS != 0L) bytes else -1L

internal fun httpCausalDurationNanos(durationMs: Long): Long =
    if (durationMs in 0L..Long.MAX_VALUE / 1_000_000L) durationMs * 1_000_000L else -1L
