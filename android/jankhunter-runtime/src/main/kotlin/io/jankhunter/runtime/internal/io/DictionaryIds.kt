package io.jankhunter.runtime.internal.io

internal class DictionaryIds(
    private val maxRegularEntries: Int = DEFAULT_MAX_REGULAR_ENTRIES,
    private val maxValueBytes: Int = DEFAULT_MAX_VALUE_BYTES,
) {
    private val idsByKind = arrayOfNulls<MutableMap<String, Long>>(FAST_KIND_COUNT)
    private val uncommonKinds = HashMap<Int, MutableMap<String, Long>>()
    private var nextId = 1L
    private var regularEntryCount = 0

    fun idFor(kind: Int, rawValue: String?): Result {
        val sanitized = sanitizeValue(rawValue)
        val value = sanitized.value
        val ids = idsFor(kind)
        ids[value]?.let {
            return Result(
                id = it,
                definition = null,
                overflowed = sanitized.forcedOverflow,
                truncated = sanitized.truncated,
            )
        }

        if (sanitized.forcedOverflow) {
            return overflowId(kind, sanitized.truncated)
        }

        if (regularEntryCount >= maxRegularEntries.coerceAtLeast(0)) {
            return overflowId(kind, sanitized.truncated)
        }

        return define(kind, value, overflowed = false, truncated = sanitized.truncated).also {
            regularEntryCount++
        }
    }

    private fun overflowId(kind: Int, truncated: Boolean): Result {
        idsFor(kind)[OVERFLOW_VALUE]?.let {
            return Result(it, null, overflowed = true, truncated = truncated)
        }
        return define(kind, OVERFLOW_VALUE, overflowed = true, truncated = truncated)
    }

    private fun define(kind: Int, value: String, overflowed: Boolean, truncated: Boolean): Result {
        val id = nextId++
        idsFor(kind)[value] = id
        return Result(id, Definition(kind, id, value), overflowed, truncated)
    }

    private fun sanitizeValue(rawValue: String?): SanitizedValue {
        val value = rawValue?.takeIf { it.isNotEmpty() } ?: UNKNOWN_VALUE
        if (value == OVERFLOW_VALUE) {
            return SanitizedValue(value, truncated = false, forcedOverflow = true)
        }
        val maxBytes = maxValueBytes.coerceAtLeast(0)
        if (maxBytes == 0) {
            return SanitizedValue(
                value = OVERFLOW_VALUE,
                truncated = true,
                forcedOverflow = true,
            )
        }
        var usedBytes = 0
        var offset = 0
        while (offset < value.length) {
            val packedWidth = packedUtf8Width(value, offset)
            val byteCount = packedWidth and UTF8_WIDTH_BYTE_MASK
            if (usedBytes + byteCount > maxBytes) break
            usedBytes += byteCount
            offset += packedWidth ushr UTF8_WIDTH_CHAR_SHIFT
        }
        if (offset == value.length) {
            return SanitizedValue(value, truncated = false, forcedOverflow = false)
        }

        val truncatedValue = value.substring(0, offset)
        return SanitizedValue(
            value = truncatedValue.takeIf { it.isNotEmpty() } ?: OVERFLOW_VALUE,
            truncated = true,
            forcedOverflow = truncatedValue.isEmpty(),
        )
    }

    private fun idsFor(kind: Int): MutableMap<String, Long> {
        if (kind !in idsByKind.indices) {
            return uncommonKinds.getOrPut(kind) { HashMap() }
        }
        return idsByKind[kind] ?: HashMap<String, Long>().also { idsByKind[kind] = it }
    }

    /** Packs UTF-16 chars and UTF-8 bytes into one Int to keep dictionary lookup allocation-free. */
    private fun packedUtf8Width(value: String, offset: Int): Int {
        val first = value[offset]
        if (
            Character.isHighSurrogate(first) &&
            offset + 1 < value.length &&
            Character.isLowSurrogate(value[offset + 1])
        ) {
            return (2 shl UTF8_WIDTH_CHAR_SHIFT) or 4
        }
        val byteCount = when {
            // StandardCharsets.UTF_8 replaces an unpaired UTF-16 surrogate with the one-byte '?'.
            // This must match BinaryLogWriter's actual encoder, not the three-byte U+FFFD encoding.
            Character.isSurrogate(first) || first.code <= 0x7f -> 1
            first.code <= 0x7ff -> 2
            else -> 3
        }
        return (1 shl UTF8_WIDTH_CHAR_SHIFT) or byteCount
    }

    data class Result(
        val id: Long,
        val definition: Definition?,
        val overflowed: Boolean,
        val truncated: Boolean,
    )

    data class Definition(
        val kind: Int,
        val id: Long,
        val value: String,
    )

    private data class SanitizedValue(
        val value: String,
        val truncated: Boolean,
        val forcedOverflow: Boolean,
    )

    companion object {
        const val DEFAULT_MAX_REGULAR_ENTRIES = 8192
        const val DEFAULT_MAX_VALUE_BYTES = 1024
        const val OVERFLOW_VALUE = "__jh_dictionary_overflow__"
        private const val FAST_KIND_COUNT = 32
        private const val UTF8_WIDTH_CHAR_SHIFT = 3
        private const val UTF8_WIDTH_BYTE_MASK = (1 shl UTF8_WIDTH_CHAR_SHIFT) - 1
        private const val UNKNOWN_VALUE = "unknown"
    }
}
