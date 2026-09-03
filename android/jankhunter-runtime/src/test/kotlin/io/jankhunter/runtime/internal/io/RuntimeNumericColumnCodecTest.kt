package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeNumericColumnCodecTest {
    @Test
    fun selectsConstantFrameDeltaRleAndRawModes() {
        val constant = LongArray(Jhlog.MAX_RUNTIME_CALL_BLOCK_ROWS) { 1L }
        val frame = LongArray(Jhlog.MAX_RUNTIME_CALL_BLOCK_ROWS) { 1_000L + (it and 7) }
        val delta = LongArray(Jhlog.MAX_RUNTIME_CALL_BLOCK_ROWS) { 1_000_000L + it }
        val rle = LongArray(Jhlog.MAX_RUNTIME_CALL_BLOCK_ROWS) {
            if (it < Jhlog.MAX_RUNTIME_CALL_BLOCK_ROWS / 2) 7L else 9L
        }
        val raw = LongArray(Jhlog.MAX_RUNTIME_CALL_BLOCK_ROWS) { if (it and 1 == 0) Long.MAX_VALUE else 0L }

        assertMode(constant, RuntimeNumericColumnCodec.MODE_CONSTANT)
        assertMode(frame, RuntimeNumericColumnCodec.MODE_FOR)
        assertMode(delta, RuntimeNumericColumnCodec.MODE_DELTA)
        assertMode(rle, RuntimeNumericColumnCodec.MODE_RLE)
        assertMode(raw, RuntimeNumericColumnCodec.MODE_UVARINT)
    }

    @Test
    fun packedModesBeatRawUvarintsOnRepresentativeColumns() {
        val values = LongArray(Jhlog.MAX_RUNTIME_CALL_BLOCK_ROWS) { 10_000L + it }
        val encoded = BinaryPayload()
        RuntimeNumericColumnCodec().encode(values, values.size, encoded)
        val rawBytes = 1 + values.sumOf(::uvarintSize)

        assertEquals(RuntimeNumericColumnCodec.MODE_DELTA.toInt(), encoded.prefixBuffer()[0].toInt())
        assertTrue("encoded=${encoded.size} raw=$rawBytes", encoded.size < rawBytes)
    }

    @Test
    fun preservesAllDefaultAndSparseBitmapModes() {
        val defaults = LongArray(Jhlog.MAX_RUNTIME_CALL_BLOCK_ROWS)
        val sparse = LongArray(Jhlog.MAX_RUNTIME_CALL_BLOCK_ROWS).also { values ->
            for (index in values.indices step 16) values[index] = 12L
        }
        val counts = LongArray(Jhlog.MAX_RUNTIME_CALL_BLOCK_ROWS) { 1L }

        assertMode(defaults, RuntimeNumericColumnCodec.MODE_DEFAULT, sparse = true)
        assertMode(sparse, RuntimeNumericColumnCodec.MODE_SPARSE, sparse = true)
        assertMode(counts, RuntimeNumericColumnCodec.MODE_DEFAULT, sparse = true, defaultValue = 1L)
    }

    private fun assertMode(
        values: LongArray,
        expected: Long,
        sparse: Boolean = false,
        defaultValue: Long = 0L,
    ) {
        val encoded = BinaryPayload()
        RuntimeNumericColumnCodec().encode(values, values.size, encoded, sparse, defaultValue)
        assertEquals(expected.toInt(), encoded.prefixBuffer()[0].toInt() and 0xff)
    }

    private fun uvarintSize(rawValue: Long): Int {
        var value = rawValue
        var size = 1
        while (value and 0x7fL.inv() != 0L) {
            value = value ushr 7
            size++
        }
        return size
    }
}
