package io.jankhunter.okhttp3

import java.util.Locale

internal object NetworkMetricNames {
    fun owner(owner: String?): String = segment(owner, "unknown")

    fun route(method: String?, encodedPath: String?): String {
        val normalizedMethod = segment(method, "request")
        val path = encodedPath?.takeIf { it.isNotBlank() } ?: "/"
        val normalizedPath = path
            .split('/')
            .asSequence()
            .filter { it.isNotBlank() }
            .take(MAX_ROUTE_SEGMENTS)
            .map(::normalizePathSegment)
            .joinToString("_")
            .ifBlank { "root" }
        return segment("${normalizedMethod}_$normalizedPath", "request")
    }

    fun webSocket(owner: String?, route: String?): String {
        val prefix = owner?.takeIf { it.isNotBlank() } ?: route
        return segment(prefix, "unknown")
    }

    fun throwable(throwable: Throwable?): String {
        return segment(throwable?.javaClass?.simpleName, "throwable")
    }

    fun statusCode(code: Int): String {
        return if (code in 100..599) code.toString() else "unknown"
    }

    fun closeCode(code: Int): String {
        return if (code in 1000..4999) code.toString() else "unknown"
    }

    private fun normalizePathSegment(raw: String): String {
        val value = raw
            .lowercase(Locale.US)
            .substringBefore('?')
            .substringBefore('#')
            .trim()
        return when {
            value.isBlank() -> "empty"
            value.matches(NUMERIC_SEGMENT) -> "id"
            value.matches(UUID_SEGMENT) -> "id"
            value.matches(LONG_HEX_SEGMENT) -> "id"
            value.length > MAX_PATH_VALUE_LENGTH && value.any(Char::isDigit) -> "value"
            else -> segment(value, "value")
        }
    }

    private fun segment(value: String?, fallback: String): String {
        val normalized = value
            ?.takeIf { it.isNotBlank() }
            ?.lowercase(Locale.US)
            ?.replace(NON_METRIC_CHAR, "_")
            ?.replace(REPEATED_UNDERSCORE, "_")
            ?.trim('_')
            ?.take(MAX_METRIC_SEGMENT_LENGTH)
        return normalized?.takeIf { it.isNotBlank() } ?: fallback
    }

    private const val MAX_ROUTE_SEGMENTS = 8
    private const val MAX_PATH_VALUE_LENGTH = 24
    private const val MAX_METRIC_SEGMENT_LENGTH = 96
    private val NON_METRIC_CHAR = Regex("[^a-z0-9]+")
    private val REPEATED_UNDERSCORE = Regex("_+")
    private val NUMERIC_SEGMENT = Regex("\\d+")
    private val UUID_SEGMENT = Regex("[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}")
    private val LONG_HEX_SEGMENT = Regex("[0-9a-f]{16,}")
}
