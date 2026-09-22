package io.jankhunter.runtime

internal fun metricOwner(ownerName: String?): String {
    return ownerName
        ?.takeIf { it.isNotBlank() }
        ?.replace(METRIC_OWNER_WHITESPACE, "_")
        ?: "unknown"
}

/** Stable websocket counter owner segment used by e2e and network-loop analysis. */
internal fun websocketMetricOwnerKey(ownerName: String?): String {
    val owner = ownerName?.trim()?.takeIf { it.isNotEmpty() } ?: return "unknown_openwebsocket"
    val normalized = owner.replace('.', '_').lowercase()
    return if (normalized.endsWith("_openwebsocket")) normalized else "${normalized}_openwebsocket"
}

private val METRIC_OWNER_WHITESPACE = Regex("\\s+")
