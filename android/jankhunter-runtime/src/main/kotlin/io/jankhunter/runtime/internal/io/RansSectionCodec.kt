package io.jankhunter.runtime.internal.io

/** Reusable order-0 byte rANS encoder for independently distributed micro-page sections. */
internal class RansSectionCodec {
    private val counts = IntArray(ALPHABET_SIZE)
    private val frequencies = IntArray(ALPHABET_SIZE)
    private val starts = IntArray(ALPHABET_SIZE)
    private var reverse = ByteArray(INITIAL_REVERSE_BYTES)

    fun encode(input: ByteArray, length: Int, output: BinaryPayload): Boolean {
        output.clear()
        if (length < MIN_INPUT_BYTES) return false
        counts.fill(0)
        for (index in 0 until length) counts[input[index].toInt() and BYTE_MASK]++

        var symbols = 0
        for (count in counts) if (count != 0) symbols++
        normalize(length, symbols)
        ensureReverseCapacity(length * MAX_BYTES_PER_SYMBOL + Int.SIZE_BYTES)

        var pointer = reverse.size
        var state = RANS_LOWER_BOUND
        for (index in length - 1 downTo 0) {
            val symbol = input[index].toInt() and BYTE_MASK
            val frequency = frequencies[symbol].toLong()
            val maximum = RANS_RENORMALIZE_BASE * frequency
            while (state >= maximum) {
                reverse[--pointer] = state.toByte()
                state = state ushr Byte.SIZE_BITS
            }
            state = ((state / frequency) shl SCALE_BITS) + (state % frequency) + starts[symbol]
        }
        pointer -= Int.SIZE_BYTES
        for (index in 0 until Int.SIZE_BYTES) {
            reverse[pointer + index] = (state ushr (index * Byte.SIZE_BITS)).toByte()
        }

        output.uvarint(symbols.toLong())
        for (symbol in counts.indices) {
            val frequency = frequencies[symbol]
            if (frequency != 0) output.byte(symbol).uvarint(frequency.toLong())
        }
        output.bytes(reverse, pointer, reverse.size - pointer)
        return output.size + uvarintSize(output.size) < length
    }

    private fun normalize(length: Int, symbolCount: Int) {
        frequencies.fill(0)
        var remainingCount = length
        var remainingFrequency = SCALE
        var remainingSymbols = symbolCount
        var cumulative = 0
        for (symbol in counts.indices) {
            val count = counts[symbol]
            starts[symbol] = cumulative
            if (count == 0) continue
            val frequency = if (remainingSymbols == 1) {
                remainingFrequency
            } else {
                val proportional = (
                    count.toLong() * remainingFrequency.toLong() + remainingCount.toLong() / 2L
                    ) / remainingCount.toLong()
                proportional.toInt().coerceIn(1, remainingFrequency - remainingSymbols + 1)
            }
            frequencies[symbol] = frequency
            cumulative += frequency
            remainingCount -= count
            remainingFrequency -= frequency
            remainingSymbols--
        }
        check(cumulative == SCALE)
    }

    private fun ensureReverseCapacity(required: Int) {
        if (required <= reverse.size) return
        var capacity = reverse.size
        while (capacity < required) capacity = capacity shl 1
        reverse = ByteArray(capacity)
    }

    private fun uvarintSize(value: Int): Int = when {
        value < 1 shl 7 -> 1
        value < 1 shl 14 -> 2
        value < 1 shl 21 -> 3
        else -> 4
    }

    private companion object {
        const val ALPHABET_SIZE = 256
        const val BYTE_MASK = 0xff
        const val SCALE_BITS = 12
        const val SCALE = 1 shl SCALE_BITS
        const val RANS_LOWER_BOUND = 1L shl 23
        const val RANS_RENORMALIZE_BASE = (RANS_LOWER_BOUND ushr SCALE_BITS) shl Byte.SIZE_BITS
        const val MIN_INPUT_BYTES = 32
        const val MAX_BYTES_PER_SYMBOL = 2
        const val INITIAL_REVERSE_BYTES = 1024
    }
}
