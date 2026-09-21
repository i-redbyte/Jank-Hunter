package io.jankhunter.gradle

internal fun escapeJsonString(value: String): String {
    val firstEscaped = value.indexOfFirst(::requiresJsonEscape)
    if (firstEscaped < 0) return value

    return buildString(value.length + JSON_ESCAPE_GROWTH) {
        append(value, 0, firstEscaped)
        for (index in firstEscaped until value.length) {
            val character = value[index]
            when (character) {
                '\\' -> append("\\\\")
                '"' -> append("\\\"")
                '\b' -> append("\\b")
                '\u000c' -> append("\\f")
                '\n' -> append("\\n")
                '\r' -> append("\\r")
                '\t' -> append("\\t")
                else -> if (character < ' ') appendControlCharacter(character) else append(character)
            }
        }
    }
}

private fun requiresJsonEscape(character: Char): Boolean = character == '\\' || character == '"' || character < ' '

private fun StringBuilder.appendControlCharacter(character: Char) {
    append("\\u00")
    append(HEX_DIGITS[character.code ushr 4])
    append(HEX_DIGITS[character.code and 0x0f])
}

private const val JSON_ESCAPE_GROWTH = 16
private const val HEX_DIGITS = "0123456789abcdef"
