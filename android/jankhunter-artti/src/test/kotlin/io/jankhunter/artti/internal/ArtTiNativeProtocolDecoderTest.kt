package io.jankhunter.artti.internal

import java.nio.ByteBuffer
import java.nio.ByteOrder
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ArtTiNativeProtocolDecoderTest {
    @Test
    fun decodesKnownRecordsAndSkipsUnknownTrailingFields() {
        val records = listOf(
            Record(type = 6, sequence = 7L, payload0 = 100L, payload1 = 20L),
            Record(type = 999, sequence = 8L, recordSize = 96),
            Record(type = 10, sequence = 9L, payload0 = 300L, payload1 = 400L),
        )
        val input = batch(records)
        val observed = mutableListOf<List<Long>>()

        val result = ArtTiNativeProtocolDecoder().decode(input, input.limit()) { record ->
            observed += listOf(
                record.type.toLong(),
                record.producerSequence,
                record.payload0,
                record.payload1,
            )
        }

        assertEquals(ArtTiNativeStatus.OK, result.status)
        assertEquals(3, result.recordsSeen)
        assertEquals(2, result.recordsDelivered)
        assertEquals(1, result.unknownRecords)
        assertEquals(7L, result.firstSequence)
        assertEquals(9L, result.lastSequence)
        assertEquals(listOf(6L, 7L, 100L, 20L), observed[0])
        assertEquals(listOf(10L, 9L, 300L, 400L), observed[1])
    }

    @Test
    fun rejectsTruncatedAndOversizedRecords() {
        val truncated = batch(listOf(Record(type = 6, sequence = 1L))).apply {
            putInt(ArtTiNativeProtocol.BATCH_HEADER_SIZE, ArtTiNativeProtocol.RECORD_SIZE + 64)
        }
        val result = ArtTiNativeProtocolDecoder().decode(truncated, truncated.limit()) { }
        assertEquals(ArtTiNativeStatus.CORRUPT_INPUT, result.status)
        assertTrue(result.error.orEmpty().contains("record"))

        val invalidBytes = ArtTiNativeProtocolDecoder().decode(
            truncated,
            ArtTiNativeProtocol.MAX_BATCH_BYTES + 1,
        ) { }
        assertEquals(ArtTiNativeStatus.INVALID_ARGUMENT, invalidBytes.status)
    }

    @Test
    fun rejectsSequenceEnvelopeMismatchAndUnsupportedProtocol() {
        val sequenceMismatch = batch(listOf(Record(type = 6, sequence = 2L))).apply {
            putLong(16, 1L)
        }
        assertEquals(
            ArtTiNativeStatus.CORRUPT_INPUT,
            ArtTiNativeProtocolDecoder().decode(sequenceMismatch, sequenceMismatch.limit()) { }.status,
        )

        val futureProtocol = batch(emptyList()).apply { putShort(4, 2) }
        assertEquals(
            ArtTiNativeStatus.UNSUPPORTED,
            ArtTiNativeProtocolDecoder().decode(futureProtocol, futureProtocol.limit()) { }.status,
        )
    }

    @Test
    fun configAndHandshakeAreBoundedAndVersioned() {
        val invalid = ArtTiNativeConfig(transportCapacity = 3)
        assertEquals(ArtTiNativeStatus.INVALID_ARGUMENT, invalid.validate())

        val config = ArtTiNativeConfig(configHash = 42L, requestedCapabilities = 7L)
        val encoded = config.encodeDirect()
        assertTrue(encoded.isDirect)
        assertEquals(ArtTiNativeProtocol.CONFIG_WIRE_SIZE, encoded.remaining())
        assertEquals(ArtTiNativeProtocol.CONFIG_WIRE_SIZE, encoded.int)
        assertEquals(1, encoded.int)
        encoded.position(56)
        assertEquals(1024, encoded.int)
        assertEquals(4096, encoded.int)
        assertEquals(250, encoded.int)
        assertEquals(120, encoded.int)

        val handshakeBuffer = ByteBuffer.allocate(ArtTiNativeProtocol.HANDSHAKE_WIRE_SIZE)
            .order(ByteOrder.LITTLE_ENDIAN)
            .apply {
                putInt(ArtTiNativeProtocol.HANDSHAKE_WIRE_SIZE)
                putInt(ArtTiNativeProtocol.ABI_VERSION)
                putInt(ArtTiNativeProtocol.PROTOCOL_VERSION)
                putInt(80)
                putLong(ArtTiNativeProtocol.REQUIRED_FEATURES)
                putInt(ArtTiNativeProtocol.BATCH_HEADER_SIZE)
                putInt(ArtTiNativeProtocol.RECORD_SIZE)
                putInt(ArtTiNativeProtocol.MAX_BATCH_BYTES)
                putInt(ArtTiNativeProtocol.CONFIG_WIRE_SIZE)
                putLong(524_288L)
                putLong(42L)
                putLong(0L)
                flip()
            }
        val handshake = ArtTiNativeHandshake.decode(handshakeBuffer).getOrThrow()
        assertTrue(handshake.isCompatible())
        assertEquals(42L, handshake.configHash)
        assertFalse(handshake.copy(protocolVersion = 2).isCompatible())
    }

    @Test
    fun decodesBoundedMethodDefinitionAndRejectsCorruption() {
        val classSignature = "Lio/jankhunter/sample/ImageCardBinder;".toByteArray()
        val methodName = "bind".toByteArray()
        val methodSignature = "(Landroid/view/View;)V".toByteArray()
        val totalSize = ArtTiNativeProtocol.METHOD_HEADER_SIZE +
            classSignature.size + methodName.size + methodSignature.size
        val input = ByteBuffer.allocate(totalSize).order(ByteOrder.LITTLE_ENDIAN).apply {
            putInt(ArtTiNativeProtocol.METHOD_MAGIC)
            putShort(ArtTiNativeProtocol.METHOD_PROTOCOL_VERSION.toShort())
            putShort(ArtTiNativeProtocol.METHOD_HEADER_SIZE.toShort())
            putInt(totalSize)
            putInt(0)
            putLong(42L)
            putShort(classSignature.size.toShort())
            putShort(methodName.size.toShort())
            putShort(methodSignature.size.toShort())
            putShort(0)
            put(classSignature)
            put(methodName)
            put(methodSignature)
            flip()
        }

        val definition = ArtTiMethodDefinition.decode(input, totalSize).getOrThrow()
        assertEquals(42L, definition.methodId)
        assertEquals("Lio/jankhunter/sample/ImageCardBinder;", definition.classSignature)
        assertEquals("bind", definition.methodName)
        assertEquals("(Landroid/view/View;)V", definition.methodSignature)

        input.putInt(8, totalSize + 1)
        assertTrue(ArtTiMethodDefinition.decode(input, totalSize).isFailure)
    }

    private fun batch(records: List<Record>): ByteBuffer {
        val batchSize = ArtTiNativeProtocol.BATCH_HEADER_SIZE + records.sumOf(Record::recordSize)
        return ByteBuffer.allocate(batchSize).order(ByteOrder.LITTLE_ENDIAN).apply {
            putInt(ArtTiNativeProtocol.BATCH_MAGIC)
            putShort(ArtTiNativeProtocol.PROTOCOL_VERSION.toShort())
            putShort(ArtTiNativeProtocol.BATCH_HEADER_SIZE.toShort())
            putInt(batchSize)
            putInt(records.size)
            putLong(records.firstOrNull()?.sequence ?: 0L)
            putLong(records.lastOrNull()?.sequence ?: 0L)
            records.forEach { record -> putRecord(record) }
            flip()
        }
    }

    private fun ByteBuffer.putRecord(record: Record) {
        val start = position()
        putInt(record.recordSize)
        putShort(record.type.toShort())
        putShort(1)
        putInt(0)
        putInt(0)
        putLong(record.sequence)
        putLong(1_000L + record.sequence)
        putLong(2L)
        putLong(3L)
        putLong(4L)
        putLong(record.payload0)
        putLong(record.payload1)
        putLong(0L)
        putLong(0L)
        while (position() < start + record.recordSize) put(0)
    }

    private data class Record(
        val type: Int,
        val sequence: Long,
        val payload0: Long = 0L,
        val payload1: Long = 0L,
        val recordSize: Int = ArtTiNativeProtocol.RECORD_SIZE,
    )
}
