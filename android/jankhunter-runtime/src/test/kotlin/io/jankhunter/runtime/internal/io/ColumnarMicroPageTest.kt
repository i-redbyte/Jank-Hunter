package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ColumnarMicroPageTest {
    @Test
    fun packsTypesAndPresenceChangeMasksIntoIndependentSections() {
        val page = ColumnarMicroPage()
        val producer = ProducerMetadataBuffer().apply { set(100L, 7L, null) }
        val context = BinaryRecordContext().set(
            screenId = 1L,
            ownerId = 0L,
            stableOwnerAlias = 2L,
            hasStableOwner = true,
            operationId = 3L,
        )
        page.append(Jhlog.TYPE_COUNTER, 8L, BinaryPayload().uvarint(11L), context, producer, 1L)
        producer.set(105L, 7L, null)
        page.append(Jhlog.TYPE_COUNTER, 0L, BinaryPayload().uvarint(12L), context, producer, 1L)

        val encoded = page.encodeTo(BinaryPayload()).copyBytes()
        var cursor = 0
        val header = readUvarint(encoded, cursor).also { cursor = it.next }
        assertEquals(2L, header.value)
        val sections = ArrayList<ByteArray>(9)
        repeat(9) {
            val length = readUvarint(encoded, cursor).also { cursor = it.next }
            val end = cursor + length.value.toInt()
            sections += encoded.copyOfRange(cursor, end)
            cursor = end
        }

        assertArrayEquals(byteArrayOf(0x08, 0x01), sections[0])
        assertArrayEquals(
            byteArrayOf(
                0x03, // time present in both rows
                0x03, // thread present in both rows
                0x01, // one thread value
                0x03, // context present in both rows
                0x01, // one context value
                0x01, // attributes only in the first row
            ),
            sections[1],
        )
        assertArrayEquals(byteArrayOf(100, 10), sections[2])
        assertArrayEquals(byteArrayOf(7), sections[3])
        assertArrayEquals(byteArrayOf(7, 2, 5, 3), sections[4])
        assertArrayEquals(byteArrayOf(8), sections[5])
        assertArrayEquals(byteArrayOf(1, 1), sections[6])
        assertArrayEquals(byteArrayOf(11, 12), sections[7])
        assertArrayEquals(byteArrayOf(), sections[8])
        assertEquals(encoded.size, cursor)
    }

    @Test
    fun pageIsBoundedAndReusableWithoutRetainingRows() {
        val page = ColumnarMicroPage()
        val payload = BinaryPayload().uvarint(1L)
        repeat(Jhlog.MAX_MICRO_PAGE_ROWS) {
            assertTrue(page.canAppend(payload.size))
            page.append(Jhlog.TYPE_COUNTER, 0L, payload, null, null, 1L)
        }
        assertEquals(Jhlog.MAX_MICRO_PAGE_ROWS, page.size)
        page.reset()
        assertEquals(0, page.size)
        assertTrue(page.canAppend(payload.size))
    }

    @Test
    fun rawContainerModeSelectsRansAndEverySectionRoundTrips() {
        val page = ColumnarMicroPage()
        val payload = BinaryPayload().bytes(ByteArray(64) { if (it % 4 == 0) 1 else 0 })
        repeat(Jhlog.MAX_MICRO_PAGE_ROWS) {
            page.append(Jhlog.TYPE_COUNTER, 0L, payload, null, null, 1L)
        }

        val encoded = page.encodeTo(BinaryPayload(), entropyEnabled = true).copyBytes()
        var cursor = 0
        val marker = readUvarint(encoded, cursor).also { cursor = it.next }
        assertEquals(0L, marker.value)
        val rowCount = readUvarint(encoded, cursor).also { cursor = it.next }
        assertEquals(Jhlog.MAX_MICRO_PAGE_ROWS.toLong(), rowCount.value)
        val codecMask = readUvarint(encoded, cursor).also { cursor = it.next }.value
        assertTrue(codecMask != 0L)
        repeat(9) { section ->
            val decodedLength = readUvarint(encoded, cursor).also { cursor = it.next }.value.toInt()
            if (codecMask and (1L shl section) == 0L) {
                cursor += decodedLength
            } else {
                val encodedLength = readUvarint(encoded, cursor).also { cursor = it.next }.value.toInt()
                val decoded = decodeRansForTest(encoded.copyOfRange(cursor, cursor + encodedLength), decodedLength)
                assertEquals(decodedLength, decoded.size)
                cursor += encodedLength
            }
        }
        assertEquals(encoded.size, cursor)
    }

    @Test
    fun databaseTransactionsUseSpecializedBitPlanesAndPackedColumns() {
        val page = ColumnarMicroPage()
        repeat(Jhlog.MAX_MICRO_PAGE_ROWS) { index ->
            val transactionPayload = BinaryPayload()
                .stableSymbolAlias(1L)
                .svarint(if (index and 1 == 0) 1L else 0L)
                .uvarint(if (index and 1 == 0) Jhlog.DATABASE_TRANSACTION_BEGIN else Jhlog.DATABASE_TRANSACTION_TERMINAL)
                .uvarint(TRANSACTION_PRESENCE)
                .svarint(-1L)
                .uvarint(2L)
                .uvarint(1L)
                .uvarint(1_000L + index)
                .uvarint(4L)
                .uvarint(3L)
                .uvarint(1L)
            page.append(Jhlog.TYPE_DATABASE_TRANSACTION, 0L, transactionPayload, null, null, 1L)
        }

        val encoded = page.encodeTo(BinaryPayload()).copyBytes()
        var cursor = readUvarint(encoded, 0).next
        var payloadLengthBytes = 0
        var payloadBytes = 0
        var databaseBytes = 0
        repeat(9) { section ->
            val length = readUvarint(encoded, cursor).also { cursor = it.next }
            when (section) {
                6 -> payloadLengthBytes = length.value.toInt()
                7 -> payloadBytes = length.value.toInt()
                8 -> databaseBytes = length.value.toInt()
            }
            cursor += length.value.toInt()
        }

        assertEquals(Jhlog.MAX_MICRO_PAGE_ROWS, payloadLengthBytes)
        assertEquals(0, payloadBytes)
        assertTrue(databaseBytes > 0)
        assertTrue(databaseBytes + payloadLengthBytes < Jhlog.MAX_MICRO_PAGE_ROWS * 12)
        assertEquals(encoded.size, cursor)
    }

    @Test
    fun databaseColumnarLayoutFallsBackWhenCompleteWireFrameIsNotSmaller() {
        val page = ColumnarMicroPage()
        val payload = BinaryPayload()
            .stableSymbolAlias(1L)
            .svarint(1L)
            .uvarint(Jhlog.DATABASE_TRANSACTION_BEGIN)
            .uvarint(0L)
        page.append(Jhlog.TYPE_DATABASE_TRANSACTION, 0L, payload, null, null, 1L)

        val encoded = page.encodeTo(BinaryPayload()).copyBytes()
        var cursor = readUvarint(encoded, 0).next
        var lengths = ByteArray(0)
        var rowPayload = ByteArray(0)
        var database = ByteArray(0)
        repeat(9) { section ->
            val length = readUvarint(encoded, cursor).also { cursor = it.next }
            val end = cursor + length.value.toInt()
            when (section) {
                6 -> lengths = encoded.copyOfRange(cursor, end)
                7 -> rowPayload = encoded.copyOfRange(cursor, end)
                8 -> database = encoded.copyOfRange(cursor, end)
            }
            cursor = end
        }

        assertArrayEquals(byteArrayOf(payload.size.toByte()), lengths)
        assertArrayEquals(payload.copyBytes(), rowPayload)
        assertArrayEquals(ByteArray(0), database)
        assertEquals(encoded.size, cursor)
    }

    private fun readUvarint(bytes: ByteArray, start: Int): Varint {
        var value = 0L
        var shift = 0
        var cursor = start
        while (cursor < bytes.size) {
            val current = bytes[cursor++].toInt() and 0xff
            value = value or ((current and 0x7f).toLong() shl shift)
            if (current and 0x80 == 0) return Varint(value, cursor)
            shift += 7
        }
        error("truncated varint")
    }

    private data class Varint(val value: Long, val next: Int)

    private companion object {
        const val TRANSACTION_PRESENCE = 0xf7L
    }
}
