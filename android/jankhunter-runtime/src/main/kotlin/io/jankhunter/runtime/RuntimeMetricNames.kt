package io.jankhunter.runtime

internal fun metricOwner(ownerName: String?): String {
    return ownerName
        ?.takeIf { it.isNotBlank() }
        ?.replace(METRIC_OWNER_WHITESPACE, "_")
        ?: "unknown"
}

private val METRIC_OWNER_WHITESPACE = Regex("\\s+")
