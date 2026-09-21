package io.jankhunter.okhttp3

internal object NetworkMetricNames {
    fun serviceAlias(value: String?): String? {
        return value?.takeIf { it.isNotBlank() }?.let { segment(it, "service") }
    }

    fun route(method: String?, encodedPath: String?): String {
        val path = encodedPath?.takeIf { it.isNotBlank() } ?: "/"
        val route = StringBuilder(minOf(MAX_ROUTE_LENGTH, path.length + 16))
        appendMethod(route, method)
        route.appendBounded(' ', MAX_ROUTE_LENGTH)

        var pathSegments = 0
        var cursor = 0
        while (cursor < path.length && pathSegments < MAX_ROUTE_SEGMENTS && route.length < MAX_ROUTE_LENGTH) {
            while (cursor < path.length && path[cursor] == '/') cursor++
            if (cursor >= path.length || path[cursor] == '?' || path[cursor] == '#') break

            val start = cursor
            while (cursor < path.length && path[cursor] != '/' && path[cursor] != '?' && path[cursor] != '#') cursor++
            var trimmedStart = start
            var trimmedEnd = cursor
            while (trimmedStart < trimmedEnd && path[trimmedStart].isWhitespace()) trimmedStart++
            while (trimmedEnd > trimmedStart && path[trimmedEnd - 1].isWhitespace()) trimmedEnd--
            if (trimmedStart < trimmedEnd) {
                route.appendBounded('/', MAX_ROUTE_LENGTH)
                appendNormalizedPathSegment(route, path, trimmedStart, trimmedEnd, MAX_ROUTE_LENGTH)
                pathSegments++
            }
            if (cursor < path.length && (path[cursor] == '?' || path[cursor] == '#')) break
        }
        if (pathSegments == 0) {
            route.appendBounded('/', MAX_ROUTE_LENGTH)
        }
        return route.toString()
    }

    private fun appendMethod(target: StringBuilder, value: String?) {
        if (value.isNullOrBlank()) {
            target.append("REQUEST")
            return
        }
        value.forEach { character ->
            if (target.length >= MAX_METHOD_LENGTH) return
            when (character) {
                in 'a'..'z' -> target.append(character.uppercaseChar())
                in 'A'..'Z', in '0'..'9', '-', '_' -> target.append(character)
            }
        }
        if (target.isEmpty()) target.append("REQUEST")
    }

    private fun appendNormalizedPathSegment(
        target: StringBuilder,
        value: String,
        start: Int,
        end: Int,
        limit: Int,
    ) {
        when {
            isNumeric(value, start, end) || isUuid(value, start, end) || isLongHex(value, start, end) -> {
                target.appendBounded("{id}", limit)
            }
            end - start > MAX_PATH_VALUE_LENGTH && containsDigit(value, start, end) -> {
                target.appendBounded("{value}", limit)
            }
            else -> appendRouteSegment(target, value, start, end, limit)
        }
    }

    private fun appendRouteSegment(target: StringBuilder, value: String, start: Int, end: Int, limit: Int) {
        var index = start
        var pendingSeparator = false
        val initialLength = target.length
        while (index < end && target.length < limit) {
            val character = value[index]
            if (character.isLetterOrDigit() || character == '.' || character == '-' || character == '_') {
                if (pendingSeparator && target.length > initialLength) target.appendBounded('-', limit)
                target.appendBounded(character, limit)
                pendingSeparator = false
            } else {
                pendingSeparator = target.length > initialLength
            }
            index++
        }
        if (target.length == initialLength) target.appendBounded("value", limit)
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

    private fun StringBuilder.appendBounded(value: Char, limit: Int = MAX_METRIC_SEGMENT_LENGTH) {
        if (length < limit) append(value)
    }

    private fun StringBuilder.appendBounded(value: String, limit: Int = MAX_METRIC_SEGMENT_LENGTH) {
        val available = limit - length
        if (available <= 0) return
        append(value, 0, minOf(value.length, available))
    }

    private const val MAX_ROUTE_SEGMENTS = 8
    private const val MAX_ROUTE_LENGTH = 160
    private const val MAX_METHOD_LENGTH = 16
    private const val MAX_PATH_VALUE_LENGTH = 24
    private const val MAX_METRIC_SEGMENT_LENGTH = 96
    private const val UUID_LENGTH = 36
    private const val MIN_LONG_HEX_LENGTH = 16
}
