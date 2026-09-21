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

        payload.clear().inlineStableSymbolRef(0x0102_0304_0506_0708L)
        assertArrayEquals(
            byteArrayOf(1, 8, 7, 6, 5, 4, 3, 2, 1),
            payload.copyBytes(),
        )

        payload.clear().stableSymbolAlias(300L)
        assertArrayEquals(byteArrayOf(0xd9.toByte(), 0x04), payload.copyBytes())
    }

    @Test
    fun boundedBytesWritesPrefixWithoutGrowingThePayloadPastTheLimit() {
        val payload = BinaryPayload(initialCapacity = 1)

        payload.boundedBytes(byteArrayOf(1, 2, 3, 4), maxBytes = 2)

        assertEquals(3, payload.size)
        assertArrayEquals(byteArrayOf(2, 1, 2), payload.copyBytes())
    }

    @Test
    fun boundedStringKeepsOnlyCompleteUtf8CodePoints() {
        val payload = BinaryPayload()

        payload.boundedString("a😀б", maxBytes = 6)

        assertArrayEquals(
            byteArrayOf(5, 'a'.code.toByte(), 0xf0.toByte(), 0x9f.toByte(), 0x98.toByte(), 0x80.toByte()),
            payload.copyBytes(),
        )
    }
}
