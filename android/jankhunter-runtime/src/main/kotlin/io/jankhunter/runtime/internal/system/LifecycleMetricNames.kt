package io.jankhunter.runtime.internal.system

import java.util.Locale

internal object LifecycleMetricNames {
    fun screen(screenName: String?): String {
        return segment(screenName, "unknown")
    }

    fun transition(from: String?, to: String?): String {
        return "${screen(from)}.to.${screen(to)}"
    }

    private fun segment(value: String?, fallback: String): String {
        val normalized = value
            ?.takeIf { it.isNotBlank() }
            ?.lowercase(Locale.US)
            ?.replace(NON_METRIC_CHAR, "_")
            ?.replace(REPEATED_UNDERSCORE, "_")
            ?.trim('_')
            ?.take(MAX_SEGMENT_LENGTH)
        return normalized?.takeIf { it.isNotBlank() } ?: fallback
    }

    private const val MAX_SEGMENT_LENGTH = 96
    private val NON_METRIC_CHAR = Regex("[^a-z0-9]+")
    private val REPEATED_UNDERSCORE = Regex("_+")
}
