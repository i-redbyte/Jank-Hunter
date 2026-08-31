package io.jankhunter.sql

/** Privacy-safe, bounded SQL shape normalization shared by build-time and runtime instrumentation. */
internal object SqlNormalizer {
    const val MAX_TEMPLATE_LENGTH = 320

    fun normalize(value: String?): String? {
        if (value == null) return null
        val end = minOf(value.length, MAX_INPUT_LENGTH)
        if (!looksLikeSql(value, end)) return null

        val result = StringBuilder(minOf(end, MAX_TEMPLATE_LENGTH))
        var index = 0
        var pendingSpace = false
        var listMode = LIST_NONE
        while (index < end && result.length < MAX_TEMPLATE_LENGTH) {
            val character = value[index]
            when {
                character.isWhitespace() -> {
                    pendingSpace = result.isNotEmpty()
                    index++
                }
                isLineComment(value, index, end) -> {
                    index = skipLineComment(value, index + 2, end)
                    pendingSpace = result.isNotEmpty()
                }
                isBlockComment(value, index, end) -> {
                    index = skipBlockComment(value, index + 2, end)
                    pendingSpace = result.isNotEmpty()
                }
                character == '\'' -> {
                    appendSpace(result, pendingSpace)
                    appendPlaceholder(result)
                    pendingSpace = false
                    listMode = LIST_NONE
                    index = skipQuoted(value, index + 1, end, '\'', '\'')
                }
                character == '"' -> {
                    appendSpace(result, pendingSpace)
                    pendingSpace = false
                    listMode = LIST_NONE
                    index = appendQuotedIdentifier(result, value, index, end, '"', '"')
                }
                character == '`' -> {
                    appendSpace(result, pendingSpace)
                    pendingSpace = false
                    listMode = LIST_NONE
                    index = appendQuotedIdentifier(result, value, index, end, '`', '`')
                }
                character == '[' -> {
                    appendSpace(result, pendingSpace)
                    pendingSpace = false
                    listMode = LIST_NONE
                    index = appendQuotedIdentifier(result, value, index, end, '[', ']')
                }
                isPlaceholderStart(value, index, end) -> {
                    appendSpace(result, pendingSpace)
                    appendPlaceholder(result)
                    pendingSpace = false
                    listMode = LIST_NONE
                    index = skipPlaceholder(value, index, end)
                }
                isNumberStart(value, index, end) -> {
                    appendSpace(result, pendingSpace)
                    appendPlaceholder(result)
                    pendingSpace = false
                    listMode = LIST_NONE
                    index = skipNumber(value, index, end)
                }
                isWordCharacter(character) -> {
                    val wordEnd = skipWord(value, index, end)
                    appendSpace(result, pendingSpace)
                    appendRange(result, value, index, wordEnd)
                    listMode = when {
                        value.regionMatches(index, "IN", 0, 2, ignoreCase = true) && wordEnd - index == 2 -> LIST_IN
                        value.regionMatches(index, "VALUES", 0, 6, ignoreCase = true) && wordEnd - index == 6 -> LIST_VALUES
                        else -> LIST_NONE
                    }
                    pendingSpace = false
                    index = wordEnd
                }
                character == '(' && listMode != LIST_NONE &&
                    (listMode == LIST_VALUES || !parenthesizedStartsWithQuery(value, index, end)) -> {
                    appendSpace(result, pendingSpace)
                    appendRange(result, COLLAPSED_LIST, 0, COLLAPSED_LIST.length)
                    pendingSpace = false
                    index = skipParenthesized(value, index, end)
                    if (listMode == LIST_VALUES) index = skipAdditionalValueGroups(value, index, end)
                    listMode = LIST_NONE
                }
                else -> {
                    appendSpace(result, pendingSpace)
                    appendCharacter(result, character)
                    pendingSpace = false
                    listMode = LIST_NONE
                    index++
                }
            }
        }
        return result.toString().trimEnd().takeIf(String::isNotEmpty)
    }

    fun operation(normalizedTemplate: String?, fallback: Int): Int {
        val query = normalizedTemplate ?: return fallback
        return when {
            startsWithKeyword(query, 0, query.length, "SELECT") ||
                startsWithKeyword(query, 0, query.length, "PRAGMA") ||
                startsWithKeyword(query, 0, query.length, "EXPLAIN") -> OPERATION_QUERY
            startsWithKeyword(query, 0, query.length, "WITH") -> operationAfterCommonTableExpressions(query, fallback)
            startsWithKeyword(query, 0, query.length, "INSERT") ||
                startsWithKeyword(query, 0, query.length, "REPLACE") -> OPERATION_INSERT
            startsWithKeyword(query, 0, query.length, "UPDATE") -> OPERATION_UPDATE
            startsWithKeyword(query, 0, query.length, "DELETE") -> OPERATION_DELETE
            else -> OPERATION_EXECUTE
        }
    }

    private fun operationAfterCommonTableExpressions(query: String, fallback: Int): Int {
        val end = query.length
        var index = WITH_KEYWORD_LENGTH
        var depth = 0
        var closedTopLevelGroup = false
        while (index < end) {
            when (query[index]) {
                '\'', '"', '`' -> index = skipQuoted(query, index + 1, end, query[index], query[index])
                '[' -> index = skipQuoted(query, index + 1, end, ']', ']')
                '(' -> {
                    depth++
                    index++
                }
                ')' -> {
                    if (depth > 0) {
                        depth--
                        if (depth == 0) closedTopLevelGroup = true
                    }
                    index++
                }
                else -> {
                    if (depth == 0 && closedTopLevelGroup && isWordCharacter(query[index])) {
                        val operation = operationAt(query, index, end)
                        if (operation != OPERATION_UNKNOWN) return operation
                        index = skipWord(query, index, end)
                    } else {
                        index++
                    }
                }
            }
        }
        return fallback
    }

    private fun operationAt(query: String, start: Int, end: Int): Int = when {
        startsWithKeyword(query, start, end, "SELECT") -> OPERATION_QUERY
        startsWithKeyword(query, start, end, "INSERT") ||
            startsWithKeyword(query, start, end, "REPLACE") -> OPERATION_INSERT
        startsWithKeyword(query, start, end, "UPDATE") -> OPERATION_UPDATE
        startsWithKeyword(query, start, end, "DELETE") -> OPERATION_DELETE
        else -> OPERATION_UNKNOWN
    }

    fun fingerprint(normalizedTemplate: String?): Long {
        if (normalizedTemplate == null) return 0L
        var hash = FNV_OFFSET_BASIS
        var index = 0
        while (index < normalizedTemplate.length) {
            val first = normalizedTemplate[index].code
            val codePoint = if (first in HIGH_SURROGATE_START..HIGH_SURROGATE_END &&
                index + 1 < normalizedTemplate.length
            ) {
                val second = normalizedTemplate[index + 1].code
                if (second in LOW_SURROGATE_START..LOW_SURROGATE_END) {
                    index++
                    SUPPLEMENTARY_START + ((first - HIGH_SURROGATE_START) shl 10) + (second - LOW_SURROGATE_START)
                } else {
                    REPLACEMENT_CHARACTER
                }
            } else if (first in LOW_SURROGATE_START..LOW_SURROGATE_END) {
                REPLACEMENT_CHARACTER
            } else {
                first
            }
            hash = updateFingerprint(hash, codePoint)
            index++
        }
        return if (hash == 0L) 1L else hash
    }

    private fun updateFingerprint(initial: Long, codePoint: Int): Long {
        var hash = initial
        when {
            codePoint <= 0x7f -> hash = updateFingerprintByte(hash, codePoint)
            codePoint <= 0x7ff -> {
                hash = updateFingerprintByte(hash, 0xc0 or (codePoint ushr 6))
                hash = updateFingerprintByte(hash, 0x80 or (codePoint and 0x3f))
            }
            codePoint <= 0xffff -> {
                hash = updateFingerprintByte(hash, 0xe0 or (codePoint ushr 12))
                hash = updateFingerprintByte(hash, 0x80 or (codePoint ushr 6 and 0x3f))
                hash = updateFingerprintByte(hash, 0x80 or (codePoint and 0x3f))
            }
            else -> {
                hash = updateFingerprintByte(hash, 0xf0 or (codePoint ushr 18))
                hash = updateFingerprintByte(hash, 0x80 or (codePoint ushr 12 and 0x3f))
                hash = updateFingerprintByte(hash, 0x80 or (codePoint ushr 6 and 0x3f))
                hash = updateFingerprintByte(hash, 0x80 or (codePoint and 0x3f))
            }
        }
        return hash
    }

    private fun updateFingerprintByte(hash: Long, value: Int): Long {
        return (hash xor value.toLong()) * FNV_PRIME
    }

    private fun looksLikeSql(value: String, end: Int): Boolean {
        val start = skipWhitespaceAndComments(value, 0, end)
        if (start >= end) return false
        for (prefix in SQL_PREFIXES) {
            if (end - start < prefix.length ||
                !value.regionMatches(start, prefix, 0, prefix.length, ignoreCase = true)
            ) {
                continue
            }
            val boundary = start + prefix.length
            if (boundary == end || !isWordCharacter(value[boundary])) return true
        }
        return false
    }

    private fun skipWhitespaceAndComments(value: String, initial: Int, end: Int): Int {
        var index = initial
        while (index < end) {
            when {
                value[index].isWhitespace() -> index++
                isLineComment(value, index, end) -> index = skipLineComment(value, index + 2, end)
                isBlockComment(value, index, end) -> index = skipBlockComment(value, index + 2, end)
                else -> return index
            }
        }
        return index
    }

    private fun skipLineComment(value: String, initial: Int, end: Int): Int {
        var index = initial
        while (index < end && value[index] != '\n' && value[index] != '\r') index++
        return index
    }

    private fun skipBlockComment(value: String, initial: Int, end: Int): Int {
        var index = initial
        while (index + 1 < end) {
            if (value[index] == '*' && value[index + 1] == '/') return index + 2
            index++
        }
        return end
    }

    private fun skipQuoted(value: String, initial: Int, end: Int, quote: Char, escape: Char): Int {
        var index = initial
        while (index < end) {
            if (value[index] == quote) {
                if (index + 1 < end && value[index + 1] == escape) {
                    index += 2
                    continue
                }
                return index + 1
            }
            index++
        }
        return end
    }

    private fun appendQuotedIdentifier(
        target: StringBuilder,
        value: String,
        initial: Int,
        end: Int,
        opening: Char,
        closing: Char,
    ): Int {
        appendCharacter(target, opening)
        var index = initial + 1
        while (index < end) {
            val character = value[index]
            appendCharacter(target, character)
            if (character == closing) {
                if (index + 1 < end && value[index + 1] == closing) {
                    appendCharacter(target, closing)
                    index += 2
                    continue
                }
                return index + 1
            }
            index++
        }
        return end
    }

    private fun isPlaceholderStart(value: String, index: Int, end: Int): Boolean {
        return when (value[index]) {
            '?' -> true
            ':', '@', '$' -> index + 1 < end && isPlaceholderCharacter(value[index + 1])
            else -> false
        }
    }

    private fun skipPlaceholder(value: String, initial: Int, end: Int): Int {
        var index = initial + 1
        while (index < end && isPlaceholderCharacter(value[index])) index++
        return index
    }

    private fun isPlaceholderCharacter(character: Char): Boolean {
        return character == '_' || character.isLetterOrDigit()
    }

    private fun isNumberStart(value: String, index: Int, end: Int): Boolean {
        val character = value[index]
        val hasBoundary = index == 0 || !isWordCharacter(value[index - 1])
        if (!hasBoundary) return false
        return character.isDigit() ||
            character == '.' && index + 1 < end && value[index + 1].isDigit() ||
            (character == '+' || character == '-') && index + 1 < end &&
            (value[index + 1].isDigit() || value[index + 1] == '.' && index + 2 < end && value[index + 2].isDigit())
    }

    private fun skipNumber(value: String, initial: Int, end: Int): Int {
        var index = initial
        if (value[index] == '+' || value[index] == '-') index++
        if (index + 1 < end && value[index] == '0' && (value[index + 1] == 'x' || value[index + 1] == 'X')) {
            index += 2
            while (index < end && value[index].digitToIntOrNull(16) != null) index++
            return index
        }
        while (index < end && value[index].isDigit()) index++
        if (index < end && value[index] == '.') {
            index++
            while (index < end && value[index].isDigit()) index++
        }
        if (index < end && (value[index] == 'e' || value[index] == 'E')) {
            var exponent = index + 1
            if (exponent < end && (value[exponent] == '+' || value[exponent] == '-')) exponent++
            val digits = exponent
            while (exponent < end && value[exponent].isDigit()) exponent++
            if (exponent > digits) index = exponent
        }
        return index
    }

    private fun skipWord(value: String, initial: Int, end: Int): Int {
        var index = initial + 1
        while (index < end && isWordCharacter(value[index])) index++
        return index
    }

    private fun parenthesizedStartsWithQuery(value: String, opening: Int, end: Int): Boolean {
        val start = skipWhitespaceAndComments(value, opening + 1, end)
        return startsWithKeyword(value, start, end, "SELECT") || startsWithKeyword(value, start, end, "WITH")
    }

    private fun startsWithKeyword(value: String, start: Int, end: Int, keyword: String): Boolean {
        if (end - start < keyword.length ||
            !value.regionMatches(start, keyword, 0, keyword.length, ignoreCase = true)
        ) {
            return false
        }
        val boundary = start + keyword.length
        return boundary == end || !isWordCharacter(value[boundary])
    }

    private fun skipParenthesized(value: String, opening: Int, end: Int): Int {
        var depth = 1
        var index = opening + 1
        while (index < end) {
            when {
                isLineComment(value, index, end) -> index = skipLineComment(value, index + 2, end)
                isBlockComment(value, index, end) -> index = skipBlockComment(value, index + 2, end)
                value[index] == '\'' -> index = skipQuoted(value, index + 1, end, '\'', '\'')
                value[index] == '"' -> index = skipQuoted(value, index + 1, end, '"', '"')
                value[index] == '`' -> index = skipQuoted(value, index + 1, end, '`', '`')
                value[index] == '[' -> index = skipQuoted(value, index + 1, end, ']', ']')
                value[index] == '(' -> {
                    depth++
                    index++
                }
                value[index] == ')' -> {
                    depth--
                    index++
                    if (depth == 0) return index
                }
                else -> index++
            }
        }
        return end
    }

    private fun skipAdditionalValueGroups(value: String, initial: Int, end: Int): Int {
        var index = initial
        while (index < end) {
            val separator = skipWhitespaceAndComments(value, index, end)
            if (separator >= end || value[separator] != ',') return index
            val opening = skipWhitespaceAndComments(value, separator + 1, end)
            if (opening >= end || value[opening] != '(') return index
            index = skipParenthesized(value, opening, end)
        }
        return index
    }

    private fun isLineComment(value: String, index: Int, end: Int): Boolean {
        return index + 1 < end && value[index] == '-' && value[index + 1] == '-'
    }

    private fun isBlockComment(value: String, index: Int, end: Int): Boolean {
        return index + 1 < end && value[index] == '/' && value[index + 1] == '*'
    }

    private fun isWordCharacter(character: Char): Boolean {
        return character == '_' || character.isLetterOrDigit()
    }

    private fun appendSpace(target: StringBuilder, pending: Boolean) {
        if (pending && target.isNotEmpty() && target.last() != ' ') appendCharacter(target, ' ')
    }

    private fun appendPlaceholder(target: StringBuilder) {
        appendCharacter(target, '?')
    }

    private fun appendCharacter(target: StringBuilder, value: Char) {
        if (target.length < MAX_TEMPLATE_LENGTH) target.append(value)
    }

    private fun appendRange(target: StringBuilder, value: String, start: Int, end: Int) {
        val available = MAX_TEMPLATE_LENGTH - target.length
        if (available <= 0) return
        target.append(value, start, minOf(end, start + available))
    }

    private val SQL_PREFIXES = arrayOf(
        "SELECT", "INSERT", "UPDATE", "DELETE", "REPLACE", "WITH", "PRAGMA",
        "CREATE", "DROP", "ALTER", "VACUUM", "BEGIN", "COMMIT", "ROLLBACK",
        "EXPLAIN", "ANALYZE", "REINDEX", "ATTACH", "DETACH",
    )
    private const val COLLAPSED_LIST = "(?)"
    private const val MAX_INPUT_LENGTH = 32 * 1024
    private const val LIST_NONE = 0
    private const val LIST_IN = 1
    private const val LIST_VALUES = 2
    private const val OPERATION_UNKNOWN = 0
    private const val OPERATION_QUERY = 1
    private const val OPERATION_INSERT = 2
    private const val OPERATION_UPDATE = 3
    private const val OPERATION_DELETE = 4
    private const val OPERATION_EXECUTE = 5
    private const val WITH_KEYWORD_LENGTH = 4
    private const val FNV_OFFSET_BASIS = -0x340d631b7bdddcdbL
    private const val FNV_PRIME = 0x100000001b3L
    private const val HIGH_SURROGATE_START = 0xd800
    private const val HIGH_SURROGATE_END = 0xdbff
    private const val LOW_SURROGATE_START = 0xdc00
    private const val LOW_SURROGATE_END = 0xdfff
    private const val SUPPLEMENTARY_START = 0x10000
    private const val REPLACEMENT_CHARACTER = 0xfffd
}
