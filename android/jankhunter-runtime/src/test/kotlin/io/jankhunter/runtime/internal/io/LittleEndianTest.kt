package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertEquals
import org.junit.Test

class LittleEndianTest {
    @Test
    fun readsValuesWrittenAtNonZeroOffset() {
        val raw = ByteArray(24) { 0x55.toByte() }

        putUInt16Le(raw, 1, 0xfedc)
        putUInt32Le(raw, 4, 0xfedcba98L)
        putUInt64Le(raw, 9, 0x76543210fedcba98L)

        assertEquals(0xfedc, uint16Le(raw, 1))
        assertEquals(0xfedcba98L, uint32Le(raw, 4))
        assertEquals(0x76543210fedcba98L, uint64Le(raw, 9))
        assertEquals(listOf(0xdc, 0xfe), raw.copyOfRange(1, 3).map { it.toInt() and 0xff })
        assertEquals(listOf(0x98, 0xba, 0xdc, 0xfe), raw.copyOfRange(4, 8).map { it.toInt() and 0xff })
        assertEquals(
            listOf(0x98, 0xba, 0xdc, 0xfe, 0x10, 0x32, 0x54, 0x76),
            raw.copyOfRange(9, 17).map { it.toInt() and 0xff },
        )
        assertEquals(0x55, raw[0].toInt())
        assertEquals(0x55, raw[17].toInt())
    }

    @Test
    fun preservesAllBitsOfSignedLong() {
        val raw = ByteArray(Long.SIZE_BYTES)

        putUInt64Le(raw, 0, -1L)

        assertEquals(-1L, uint64Le(raw, 0))
    }

    @Test
    fun checksumHonorsRequestedRange() {
        val raw = byteArrayOf(9, 1, 2, 3, 9)

        assertEquals(crc32(byteArrayOf(1, 2, 3)), crc32(raw, 1, 3))
    }
}
