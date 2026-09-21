package io.jankhunter.runtime.internal.io

/** Adaptive allocation-free numeric column codec for one bounded runtime-call block. */
internal class RuntimeNumericColumnCodec {
    private var mode = MODE_UVARINT
    private var base = 0L
    private var width = 0
    private var runCount = 0

    fun encode(
        values: LongArray,
        count: Int,
        destination: BinaryPayload,
        sparse: Boolean = false,
        defaultValue: Long = 0L,
    ) {
        require(count in 1..Jhlog.MAX_RUNTIME_CALL_BLOCK_ROWS)
        plan(values, count, sparse, defaultValue)
        destination.uvarint(mode)
        when (mode) {
            MODE_CONSTANT -> destination.uvarint(base)
            MODE_FOR -> {
                destination.uvarint(base).uvarint(width.toLong())
                PackedLongs.writeFrame(values, 0, count, base, width, destination)
            }
            MODE_DELTA -> {
                destination.uvarint(values[0]).uvarint(width.toLong())
                PackedLongs.writeDelta(values, 0, count, width, destination)
            }
            MODE_RLE -> writeRuns(values, count, destination)
            MODE_UVARINT -> {
                for (index in 0 until count) destination.uvarint(values[index])
            }
            MODE_DEFAULT -> Unit
            MODE_SPARSE -> writeSparse(values, count, defaultValue, destination)
        }
    }

    private fun plan(values: LongArray, count: Int, sparse: Boolean, defaultValue: Long) {
        var minimum = values[0]
        var maximum = minimum
        var rawBytes = 1
        var runs = 1
        var runStart = 0
        var runBytes = 0
        var deltaWidth = 0
        var nonDefault = 0
        var sparseBytes = 1 + (count + 7) / 8
        for (index in 0 until count) {
            val value = values[index]
            require(value >= 0L)
            minimum = minOf(minimum, value)
            maximum = maxOf(maximum, value)
            rawBytes += PackedLongs.uvarintSize(value)
            if (value != defaultValue) {
                nonDefault++
                sparseBytes += PackedLongs.uvarintSize(value)
            }
            if (index == 0) continue
            if (values[index - 1] != value) {
                runBytes += PackedLongs.uvarintSize(values[runStart]) +
                    PackedLongs.uvarintSize((index - runStart).toLong())
                runs++
                runStart = index
            }
            deltaWidth = maxOf(deltaWidth, PackedLongs.bitWidth(PackedLongs.zigzag(value - values[index - 1])))
        }
        runBytes += PackedLongs.uvarintSize(values[runStart]) + PackedLongs.uvarintSize((count - runStart).toLong())
        if (sparse && nonDefault == 0) {
            mode = MODE_DEFAULT
            return
        }
        if (minimum == maximum) {
            mode = MODE_CONSTANT
            base = minimum
            width = 0
            runCount = 1
            return
        }
        mode = MODE_UVARINT
        var bestBytes = rawBytes
        val frameWidth = PackedLongs.bitWidth(maximum - minimum)
        val frameBytes = 1 + PackedLongs.uvarintSize(minimum) + PackedLongs.uvarintSize(frameWidth.toLong()) +
            (count * frameWidth + 7) / 8
        if (frameBytes < bestBytes) {
            mode = MODE_FOR
            base = minimum
            width = frameWidth
            bestBytes = frameBytes
        }
        val deltaBytes = 1 + PackedLongs.uvarintSize(values[0]) + PackedLongs.uvarintSize(deltaWidth.toLong()) +
            ((count - 1) * deltaWidth + 7) / 8
        if (deltaWidth != 0 && deltaBytes < bestBytes) {
            mode = MODE_DELTA
            width = deltaWidth
            bestBytes = deltaBytes
        }
        runBytes += 1 + PackedLongs.uvarintSize(runs.toLong())
        if (runBytes < bestBytes) {
            mode = MODE_RLE
            runCount = runs
            bestBytes = runBytes
        }
        if (sparse && sparseBytes < bestBytes) {
            mode = MODE_SPARSE
        }
    }

    private fun writeRuns(values: LongArray, count: Int, destination: BinaryPayload) {
        destination.uvarint(runCount.toLong())
        var start = 0
        while (start < count) {
            var end = start + 1
            while (end < count && values[end] == values[start]) end++
            destination.uvarint(values[start]).uvarint((end - start).toLong())
            start = end
        }
    }

    private fun writeSparse(values: LongArray, count: Int, defaultValue: Long, destination: BinaryPayload) {
        val bitmapBytes = (count + 7) / 8
        val bitmapOffset = destination.size
        repeat(bitmapBytes) { destination.byte(0) }
        val bytes = destination.prefixBuffer()
        for (index in 0 until count) {
            if (values[index] == defaultValue) continue
            val byteIndex = bitmapOffset + index / Byte.SIZE_BITS
            bytes[byteIndex] = (bytes[byteIndex].toInt() or (1 shl (index % Byte.SIZE_BITS))).toByte()
        }
        for (index in 0 until count) {
            if (values[index] != defaultValue) destination.uvarint(values[index])
        }
    }

    internal companion object {
        const val MODE_CONSTANT = 0L
        const val MODE_FOR = 1L
        const val MODE_DELTA = 2L
        const val MODE_RLE = 3L
        const val MODE_UVARINT = 4L
        const val MODE_DEFAULT = 5L
        const val MODE_SPARSE = 6L
    }
}
