package io.jankhunter.runtime.internal.io

import java.util.zip.CRC32

internal fun crc32(bytes: ByteArray, offset: Int = 0, length: Int = bytes.size): Long {
    return CRC32().apply { update(bytes, offset, length) }.value
}

internal fun uint16Le(bytes: ByteArray, offset: Int): Int {
    return (bytes[offset].toInt() and 0xff) or
        ((bytes[offset + 1].toInt() and 0xff) shl Byte.SIZE_BITS)
}

internal fun uint32Le(bytes: ByteArray, offset: Int): Long {
    var value = 0L
    repeat(Int.SIZE_BYTES) { index ->
        value = value or ((bytes[offset + index].toLong() and 0xffL) shl (index * Byte.SIZE_BITS))
    }
    return value
}

internal fun uint64Le(bytes: ByteArray, offset: Int): Long {
    var value = 0L
    repeat(Long.SIZE_BYTES) { index ->
        value = value or ((bytes[offset + index].toLong() and 0xffL) shl (index * Byte.SIZE_BITS))
    }
    return value
}

internal fun putUInt16Le(bytes: ByteArray, offset: Int, value: Int) {
    bytes[offset] = value.toByte()
    bytes[offset + 1] = (value ushr Byte.SIZE_BITS).toByte()
}

internal fun putUInt32Le(bytes: ByteArray, offset: Int, value: Long) {
    repeat(Int.SIZE_BYTES) { index -> bytes[offset + index] = (value ushr (index * Byte.SIZE_BITS)).toByte() }
}

internal fun putUInt64Le(bytes: ByteArray, offset: Int, value: Long) {
    repeat(Long.SIZE_BYTES) { index -> bytes[offset + index] = (value ushr (index * Byte.SIZE_BITS)).toByte() }
}
