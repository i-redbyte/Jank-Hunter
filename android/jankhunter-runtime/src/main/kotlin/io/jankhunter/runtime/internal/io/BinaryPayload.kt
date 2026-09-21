package io.jankhunter.runtime.internal.io

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

    fun stableSymbolAlias(alias: Long): BinaryPayload {
        require(alias > 0L)
        return uvarint((alias shl 1) or 1L)
    }

    fun inlineStableSymbolRef(stableId: Long): BinaryPayload {
        uvarint(1L)
        return fixedLongLe(stableId)
    }

    fun fixedLongLe(value: Long): BinaryPayload {
        ensureCapacity(size + Long.SIZE_BYTES)
        repeat(Long.SIZE_BYTES) { byteIndex ->
            bytes[size++] = (value ushr (byteIndex * Byte.SIZE_BITS)).toByte()
        }
        return this
    }

    fun byte(value: Int): BinaryPayload {
        ensureCapacity(size + 1)
        bytes[size++] = value.toByte()
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

    fun bytes(value: ByteArray, offset: Int, length: Int): BinaryPayload {
        require(offset >= 0 && length >= 0 && offset + length <= value.size)
        ensureCapacity(size + length)
        value.copyInto(bytes, destinationOffset = size, startIndex = offset, endIndex = offset + length)
        size += length
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
        val limit = maxBytes.coerceAtLeast(0)
        var encodedSize = 0
        var charLimit = 0
        while (charLimit < value.length) {
            val first = value[charLimit]
            val pairedSurrogate = Character.isHighSurrogate(first) &&
                charLimit + 1 < value.length && Character.isLowSurrogate(value[charLimit + 1])
            val byteCount = when {
                pairedSurrogate -> 4
                Character.isSurrogate(first) || first.code <= 0x7f -> 1
                first.code <= 0x7ff -> 2
                else -> 3
            }
            if (encodedSize + byteCount > limit) break
            encodedSize += byteCount
            charLimit += if (pairedSurrogate) 2 else 1
        }

        uvarint(encodedSize.toLong())
        ensureCapacity(size + encodedSize)
        var offset = 0
        while (offset < charLimit) {
            val first = value[offset]
            when {
                Character.isHighSurrogate(first) &&
                    offset + 1 < charLimit && Character.isLowSurrogate(value[offset + 1]) -> {
                    val codePoint = Character.toCodePoint(first, value[offset + 1])
                    bytes[size++] = (0xf0 or (codePoint ushr 18)).toByte()
                    bytes[size++] = (0x80 or (codePoint ushr 12 and 0x3f)).toByte()
                    bytes[size++] = (0x80 or (codePoint ushr 6 and 0x3f)).toByte()
                    bytes[size++] = (0x80 or (codePoint and 0x3f)).toByte()
                    offset += 2
                }
                Character.isSurrogate(first) -> {
                    bytes[size++] = '?'.code.toByte()
                    offset++
                }
                first.code <= 0x7f -> {
                    bytes[size++] = first.code.toByte()
                    offset++
                }
                first.code <= 0x7ff -> {
                    bytes[size++] = (0xc0 or (first.code ushr 6)).toByte()
                    bytes[size++] = (0x80 or (first.code and 0x3f)).toByte()
                    offset++
                }
                else -> {
                    bytes[size++] = (0xe0 or (first.code ushr 12)).toByte()
                    bytes[size++] = (0x80 or (first.code ushr 6 and 0x3f)).toByte()
                    bytes[size++] = (0x80 or (first.code and 0x3f)).toByte()
                    offset++
                }
            }
        }
        return this
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

    /**
     * Borrows the backing buffer without allocation. Only the prefix `[0, size)` is initialized;
     * callers must not retain it across any operation that can grow this payload.
     */
    fun prefixBuffer(): ByteArray = bytes

    fun copyTo(destination: ByteArray, destinationOffset: Int): Int {
        require(destinationOffset >= 0 && destinationOffset + size <= destination.size)
        bytes.copyInto(destination, destinationOffset = destinationOffset, endIndex = size)
        return destinationOffset + size
    }

    private fun ensureCapacity(required: Int) {
        if (required <= bytes.size) return
        var capacity = bytes.size
        while (capacity < required) {
            val next = capacity shl 1
            capacity = if (next > capacity) next else required
        }
        bytes = bytes.copyOf(capacity)
    }

    private companion object {
        const val DEFAULT_CAPACITY = 64
        const val MAX_VARINT_BYTES = 10
    }
}
