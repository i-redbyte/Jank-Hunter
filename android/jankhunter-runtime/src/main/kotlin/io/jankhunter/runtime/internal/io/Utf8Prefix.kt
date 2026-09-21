package io.jankhunter.runtime.internal.io

/** Retains at most [maxBytes] without splitting a multi-byte UTF-8 sequence. */
internal fun utf8Prefix(encoded: ByteArray, maxBytes: Int): ByteArray {
    val limit = maxBytes.coerceAtLeast(0)
    if (encoded.size <= limit) return encoded
    if (limit == 0) return ByteArray(0)

    var prefixSize = limit
    while (prefixSize > 0 && encoded[prefixSize].toInt() and UTF8_CONTINUATION_MASK == UTF8_CONTINUATION_TAG) {
        prefixSize--
    }
    return encoded.copyOf(prefixSize)
}

private const val UTF8_CONTINUATION_MASK = 0xc0
private const val UTF8_CONTINUATION_TAG = 0x80
