package io.jankhunter.runtime.internal.io

/** Shared branch-specialized primitives for allocation-free packed unsigned-long columns. */
internal object PackedLongs {
    @JvmStatic
    fun writeFrame(
        values: LongArray,
        offset: Int,
        count: Int,
        minimum: Long,
        width: Int,
        destination: BinaryPayload,
    ) = writePacked(count, width, destination) { index -> values[offset + index] - minimum }

    @JvmStatic
    fun writeDelta(
        values: LongArray,
        offset: Int,
        count: Int,
        width: Int,
        destination: BinaryPayload,
    ) = writePacked(count - 1, width, destination) { index ->
        zigzag(values[offset + index + 1] - values[offset + index])
    }

    @JvmStatic
    fun bitWidth(value: Long): Int = Long.SIZE_BITS - java.lang.Long.numberOfLeadingZeros(value)

    @JvmStatic
    fun zigzag(value: Long): Long = (value shl 1) xor (value shr (Long.SIZE_BITS - 1))

    @JvmStatic
    fun uvarintSize(rawValue: Long): Int {
        var value = rawValue
        var size = 1
        while (value and 0x7fL.inv() != 0L) {
            value = value ushr 7
            size++
        }
        return size
    }

    private inline fun writePacked(
        count: Int,
        width: Int,
        destination: BinaryPayload,
        valueAt: (Int) -> Long,
    ) {
        var buffered = 0L
        var bufferedBits = 0
        for (index in 0 until count) {
            var value = valueAt(index)
            var remaining = width
            while (remaining > 0) {
                val take = minOf(remaining, Long.SIZE_BITS - bufferedBits)
                val mask = if (take == Long.SIZE_BITS) -1L else (1L shl take) - 1L
                buffered = buffered or ((value and mask) shl bufferedBits)
                bufferedBits += take
                value = value ushr take
                remaining -= take
                while (bufferedBits >= Byte.SIZE_BITS) {
                    destination.byte(buffered.toInt())
                    buffered = buffered ushr Byte.SIZE_BITS
                    bufferedBits -= Byte.SIZE_BITS
                }
            }
        }
        if (bufferedBits != 0) destination.byte(buffered.toInt())
    }
}
