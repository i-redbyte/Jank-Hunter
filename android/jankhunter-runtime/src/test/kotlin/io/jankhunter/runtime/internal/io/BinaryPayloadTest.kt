package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Test

class BinaryPayloadTest {
    @Test
    fun bufferEncodesWirePrimitivesAndReusesItsInstance() {
        val payload = BinaryPayload(initialCapacity = 2)

        assertSame(payload, payload.uvarint(300L))
        assertArrayEquals(byteArrayOf(0xac.toByte(), 0x02), payload.copyBytes())

        payload.clear().stableSymbolRef(0x0102_0304_0506_0708L)
        assertArrayEquals(
            byteArrayOf(1, 8, 7, 6, 5, 4, 3, 2, 1),
            payload.copyBytes(),
        )
    }

    @Test
    fun boundedBytesWritesPrefixWithoutGrowingThePayloadPastTheLimit() {
        val payload = BinaryPayload(initialCapacity = 1)

        payload.boundedBytes(byteArrayOf(1, 2, 3, 4), maxBytes = 2)

        assertEquals(3, payload.size)
        assertArrayEquals(byteArrayOf(2, 1, 2), payload.copyBytes())
    }
}
