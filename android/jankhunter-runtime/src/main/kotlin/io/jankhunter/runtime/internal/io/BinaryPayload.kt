package io.jankhunter.runtime.internal.io

import java.nio.charset.StandardCharsets

/** Reusable growable byte sink for JHLOG payload and envelope encoding. */
internal class BinaryPayload(initialCapacity: Int = DEFAULT_CAPACITY) {
    private var bytes = ByteArray(initialCapacity.coerceAtLeast(1))

    var size: Int = 0
        private set

    fun clear(): BinaryPayload {
        size = 0
        return this
    }

    fun uvarint(rawValue: Long): BinaryPayload {
        var value = rawValue
        ensureCapacity(size + MAX_VARINT_BYTES)
        while (value and 0x7fL.inv() != 0L) {
            bytes[size++] = ((value and 0x7fL) or 0x80L).toByte()
            value = value ushr 7
        }
        bytes[size++] = value.toByte()
        return this
    }

    fun svarint(value: Long): BinaryPayload = uvarint((value shl 1) xor (value shr 63))

    fun symbolRef(localId: Long): BinaryPayload = uvarint(if (localId <= 0L) 0L else localId shl 1)

    fun stableSymbolRef(stableId: Long): BinaryPayload {
        uvarint(1L)
        ensureCapacity(size + Long.SIZE_BYTES)
        repeat(Long.SIZE_BYTES) { byteIndex ->
            bytes[size++] = (stableId ushr (byteIndex * Byte.SIZE_BITS)).toByte()
        }
        return this
    }

    fun bytes(value: ByteArray): BinaryPayload = bytes(value, value.size)

    fun bytes(value: ByteArray, length: Int): BinaryPayload {
        val count = length.coerceIn(0, value.size)
        ensureCapacity(size + count)
        value.copyInto(bytes, destinationOffset = size, endIndex = count)
        size += count
        return this
    }

    fun bytes(value: BinaryPayload): BinaryPayload {
        ensureCapacity(size + value.size)
        value.bytes.copyInto(bytes, destinationOffset = size, endIndex = value.size)
        size += value.size
        return this
    }

    fun fixedBytes(value: ByteArray): BinaryPayload = bytes(value)

    fun boundedString(value: String, maxBytes: Int): BinaryPayload {
        return boundedBytes(validUtf8Prefix(value, maxBytes), maxBytes)
    }

    fun boundedBytes(value: ByteArray, maxBytes: Int): BinaryPayload {
        val count = minOf(value.size, maxBytes.coerceAtLeast(0))
        uvarint(count.toLong())
        return bytes(value, count)
    }

    fun writeTo(destination: java.io.ByteArrayOutputStream) {
        destination.write(bytes, 0, size)
    }

    fun copyBytes(): ByteArray = bytes.copyOf(size)

    private fun ensureCapacity(required: Int) {
        if (required <= bytes.size) return
        var capacity = bytes.size
        while (capacity < required) {
            val next = capacity shl 1
            capacity = if (next > capacity) next else required
        }
        bytes = bytes.copyOf(capacity)
    }

    private fun validUtf8Prefix(value: String, maxBytes: Int): ByteArray {
        val limit = maxBytes.coerceAtLeast(0)
        val encoded = value.toByteArray(StandardCharsets.UTF_8)
        if (encoded.size <= limit) return encoded
        if (limit == 0) return ByteArray(0)

        val builder = StringBuilder()
        var usedBytes = 0
        var offset = 0
        while (offset < value.length) {
            val codePoint = value.codePointAt(offset)
            val charCount = Character.charCount(codePoint)
            val codePointBytes = value
                .substring(offset, offset + charCount)
                .toByteArray(StandardCharsets.UTF_8)
            if (usedBytes + codePointBytes.size > limit) break
            builder.appendCodePoint(codePoint)
            usedBytes += codePointBytes.size
            offset += charCount
        }
        return builder.toString().toByteArray(StandardCharsets.UTF_8)
    }

    private companion object {
        const val DEFAULT_CAPACITY = 64
        const val MAX_VARINT_BYTES = 10
    }
}
