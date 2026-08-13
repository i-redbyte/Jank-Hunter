package io.jankhunter.okhttp3

internal object NetworkMetricNames {
    fun owner(owner: String?): String = segment(owner, "unknown")

    fun route(method: String?, encodedPath: String?): String {
        val path = encodedPath?.takeIf { it.isNotBlank() } ?: "/"
        val route = StringBuilder(MAX_METRIC_SEGMENT_LENGTH)
        appendSegment(route, method, 0, method?.length ?: 0, "request")

        var pathSegments = 0
        var cursor = 0
        while (cursor < path.length && pathSegments < MAX_ROUTE_SEGMENTS && route.length < MAX_METRIC_SEGMENT_LENGTH) {
            while (cursor < path.length && path[cursor] == '/') cursor++
            if (cursor >= path.length || path[cursor] == '?' || path[cursor] == '#') break

            val start = cursor
            while (cursor < path.length && path[cursor] != '/' && path[cursor] != '?' && path[cursor] != '#') cursor++
            var trimmedStart = start
            var trimmedEnd = cursor
            while (trimmedStart < trimmedEnd && path[trimmedStart].isWhitespace()) trimmedStart++
            while (trimmedEnd > trimmedStart && path[trimmedEnd - 1].isWhitespace()) trimmedEnd--
            if (trimmedStart < trimmedEnd) {
                route.appendBounded('_')
                appendNormalizedPathSegment(route, path, trimmedStart, trimmedEnd)
                pathSegments++
            }
            if (cursor < path.length && (path[cursor] == '?' || path[cursor] == '#')) break
        }
        if (pathSegments == 0) {
            route.appendBounded('_')
            route.appendBounded("root")
        }
        return route.toString()
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

    private fun appendNormalizedPathSegment(target: StringBuilder, value: String, start: Int, end: Int) {
        when {
            isNumeric(value, start, end) || isUuid(value, start, end) || isLongHex(value, start, end) -> {
                target.appendBounded("id")
            }
            end - start > MAX_PATH_VALUE_LENGTH && containsDigit(value, start, end) -> {
                target.appendBounded("value")
            }
            else -> appendSegment(target, value, start, end, "value")
        }
    }

    private fun segment(value: String?, fallback: String): String {
        if (value.isNullOrBlank()) return fallback
        val normalized = StringBuilder(minOf(value.length, MAX_METRIC_SEGMENT_LENGTH))
        appendSegment(normalized, value, 0, value.length, fallback)
        return normalized.toString()
    }

    private fun appendSegment(
        target: StringBuilder,
        value: String?,
        start: Int,
        end: Int,
        fallback: String,
    ) {
        val initialLength = target.length
        if (value != null) {
            var pendingSeparator = false
            var index = start
            while (index < end && target.length < MAX_METRIC_SEGMENT_LENGTH) {
                val character = value[index]
                val normalized = when (character) {
                    in 'a'..'z', in '0'..'9' -> character
                    in 'A'..'Z' -> character.lowercaseChar()
                    else -> null
                }
                if (normalized == null) {
                    pendingSeparator = target.length > initialLength
                } else {
                    if (pendingSeparator) target.appendBounded('_')
                    target.appendBounded(normalized)
                    pendingSeparator = false
                }
                index++
            }
        }
        if (target.length == initialLength) target.appendBounded(fallback)
    }

    private fun isNumeric(value: String, start: Int, end: Int): Boolean {
        if (start == end) return false
        for (index in start until end) {
            if (value[index] !in '0'..'9') return false
        }
        return true
    }

    private fun isUuid(value: String, start: Int, end: Int): Boolean {
        if (end - start != UUID_LENGTH) return false
        for (offset in 0 until UUID_LENGTH) {
            val character = value[start + offset]
            if (offset == 8 || offset == 13 || offset == 18 || offset == 23) {
                if (character != '-') return false
            } else if (!character.isAsciiHex()) {
                return false
            }
        }
        return true
    }

    private fun isLongHex(value: String, start: Int, end: Int): Boolean {
        if (end - start < MIN_LONG_HEX_LENGTH) return false
        for (index in start until end) {
            if (!value[index].isAsciiHex()) return false
        }
        return true
    }

    private fun containsDigit(value: String, start: Int, end: Int): Boolean {
        for (index in start until end) {
            if (value[index].isDigit()) return true
        }
        return false
    }

    private fun Char.isAsciiHex(): Boolean {
        return this in '0'..'9' || this in 'a'..'f' || this in 'A'..'F'
    }

    private fun StringBuilder.appendBounded(value: Char) {
        if (length < MAX_METRIC_SEGMENT_LENGTH) append(value)
    }

    private fun StringBuilder.appendBounded(value: String) {
        val available = MAX_METRIC_SEGMENT_LENGTH - length
        if (available <= 0) return
        append(value, 0, minOf(value.length, available))
    }

    private const val MAX_ROUTE_SEGMENTS = 8
    private const val MAX_PATH_VALUE_LENGTH = 24
    private const val MAX_METRIC_SEGMENT_LENGTH = 96
    private const val UUID_LENGTH = 36
    private const val MIN_LONG_HEX_LENGTH = 16
}
