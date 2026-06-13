package io.jankhunter.runtime.internal.io

import java.nio.charset.StandardCharsets

internal class DictionaryIds(
    private val maxRegularEntries: Int = DEFAULT_MAX_REGULAR_ENTRIES,
    private val maxValueBytes: Int = DEFAULT_MAX_VALUE_BYTES,
) {
    private val ids = LinkedHashMap<String, Long>()
    private var nextId = 1L

    fun idFor(kind: Int, rawValue: String?): Result {
        val value = sanitizeValue(rawValue)
        val key = key(kind, value)
        ids[key]?.let { return Result(it, null) }

        if (ids.size >= maxRegularEntries.coerceAtLeast(0)) {
            return overflowId(kind)
        }

        return define(kind, value)
    }

    private fun overflowId(kind: Int): Result {
        val overflowKey = key(kind, OVERFLOW_VALUE)
        ids[overflowKey]?.let { return Result(it, null) }
        return define(kind, OVERFLOW_VALUE)
    }

    private fun define(kind: Int, value: String): Result {
        val id = nextId++
        ids[key(kind, value)] = id
        return Result(id, Definition(kind, id, value))
    }

    private fun sanitizeValue(rawValue: String?): String {
        val value = rawValue?.takeIf { it.isNotEmpty() } ?: UNKNOWN_VALUE
        val maxBytes = maxValueBytes.coerceAtLeast(0)
        if (maxBytes == 0) return OVERFLOW_VALUE
        if (value.toByteArray(StandardCharsets.UTF_8).size <= maxBytes) return value

        val builder = StringBuilder()
        var usedBytes = 0
        for (char in value) {
            val charBytes = char.toString().toByteArray(StandardCharsets.UTF_8).size
            if (usedBytes + charBytes > maxBytes) break
            builder.append(char)
            usedBytes += charBytes
        }
        return builder.toString().takeIf { it.isNotEmpty() } ?: OVERFLOW_VALUE
    }

    private fun key(kind: Int, value: String): String = "$kind:$value"

    data class Result(
        val id: Long,
        val definition: Definition?,
    )

    data class Definition(
        val kind: Int,
        val id: Long,
        val value: String,
    )

    companion object {
        const val DEFAULT_MAX_REGULAR_ENTRIES = 8192
        const val DEFAULT_MAX_VALUE_BYTES = 256
        const val OVERFLOW_VALUE = "__jh_dictionary_overflow__"
        private const val UNKNOWN_VALUE = "unknown"
    }
}
