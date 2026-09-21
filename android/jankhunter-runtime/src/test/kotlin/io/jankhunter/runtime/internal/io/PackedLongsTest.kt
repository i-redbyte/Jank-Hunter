package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Test

class PackedLongsTest {
    @Test
    fun frameAndDeltaWritersUseCanonicalLsbFirstPacking() {
        val frame = BinaryPayload()
        PackedLongs.writeFrame(longArrayOf(99L, 10L, 12L, 11L), 1, 3, 10L, 2, frame)
        assertArrayEquals(byteArrayOf(0x18), frame.copyBytes())

        val delta = BinaryPayload()
        PackedLongs.writeDelta(longArrayOf(10L, 11L, 9L), 0, 3, 3, delta)
        assertArrayEquals(byteArrayOf(0x1a), delta.copyBytes())
    }

    @Test
    fun bitAndVarintWidthsCoverTheFullUnsignedLongRange() {
        assertEquals(0, PackedLongs.bitWidth(0L))
        assertEquals(64, PackedLongs.bitWidth(-1L))
        assertEquals(1, PackedLongs.uvarintSize(0x7fL))
        assertEquals(2, PackedLongs.uvarintSize(0x80L))
        assertEquals(10, PackedLongs.uvarintSize(-1L))
    }
}
