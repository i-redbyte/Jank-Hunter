package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import kotlin.random.Random

class RansSectionCodecTest {
    @Test
    fun compressibleDistributionsRoundTrip() {
        val inputs = listOf(
            ByteArray(4_096),
            ByteArray(8_192) { if (it % 4 == 3) 1 else 0 },
            "jank-hunter-column".repeat(512).encodeToByteArray(),
        )
        val codec = RansSectionCodec()
        val encoded = BinaryPayload()
        inputs.forEach { input ->
            assertTrue(codec.encode(input, input.size, encoded))
            assertArrayEquals(input, decodeRansForTest(encoded.copyBytes(), input.size))
        }
    }

    @Test
    fun highEntropyInputUsesRawFallback() {
        val input = Random(32_815).nextBytes(4_096)
        assertFalse(RansSectionCodec().encode(input, input.size, BinaryPayload()))
    }
}

internal fun decodeRansForTest(frame: ByteArray, decodedLength: Int): ByteArray {
    var cursor = 0
    fun uvarint(): Long {
        var value = 0L
        var shift = 0
        while (cursor < frame.size && shift < 64) {
            val current = frame[cursor++].toInt() and 0xff
            value = value or ((current and 0x7f).toLong() shl shift)
            if (current < 0x80) return value
            shift += 7
        }
        error("invalid rANS varint")
    }

    val symbolCount = uvarint().toInt()
    require(symbolCount in 1..256)
    val frequencies = IntArray(256)
    val starts = IntArray(256)
    val table = ByteArray(1 shl 12)
    var cumulative = 0
    var previous = -1
    repeat(symbolCount) {
        val symbol = frame[cursor++].toInt() and 0xff
        require(symbol > previous)
        val frequency = uvarint().toInt()
        require(frequency > 0 && cumulative + frequency <= table.size)
        starts[symbol] = cumulative
        frequencies[symbol] = frequency
        table.fill(symbol.toByte(), cumulative, cumulative + frequency)
        cumulative += frequency
        previous = symbol
    }
    require(cumulative == table.size && cursor + Int.SIZE_BYTES <= frame.size)
    var state = 0L
    repeat(Int.SIZE_BYTES) { index ->
        state = state or ((frame[cursor++].toLong() and 0xffL) shl (index * Byte.SIZE_BITS))
    }
    val output = ByteArray(decodedLength)
    for (index in output.indices) {
        val slot = (state and (table.size - 1).toLong()).toInt()
        val symbol = table[slot].toInt() and 0xff
        output[index] = symbol.toByte()
        state = frequencies[symbol].toLong() * (state ushr 12) + slot - starts[symbol]
        while (state < 1L shl 23) state = (state shl 8) or (frame[cursor++].toLong() and 0xffL)
    }
    require(state == 1L shl 23 && cursor == frame.size)
    return output
}
