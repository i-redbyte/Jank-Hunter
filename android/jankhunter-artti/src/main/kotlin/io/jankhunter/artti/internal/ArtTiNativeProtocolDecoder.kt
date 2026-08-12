package io.jankhunter.artti.internal

import java.nio.ByteBuffer
import java.nio.ByteOrder

internal fun interface ArtTiNativeRecordVisitor {
    fun onRecord(record: ArtTiNativeRecordView)
}

/** Mutable flyweight valid only for the duration of [ArtTiNativeRecordVisitor.onRecord]. */
internal class ArtTiNativeRecordView {
    var type: Int = 0
        internal set
    var schemaVersion: Int = 0
        internal set
    var flags: Int = 0
        internal set
    var producerSequence: Long = 0L
        internal set
    var monotonicNs: Long = 0L
        internal set
    var producerId: Long = 0L
        internal set
    var threadToken: Long = 0L
        internal set
    var contextToken: Long = 0L
        internal set
    var payload0: Long = 0L
        internal set
    var payload1: Long = 0L
        internal set
    var payload2: Long = 0L
        internal set
    var payload3: Long = 0L
        internal set
}

internal data class ArtTiNativeDecodeResult(
    val status: ArtTiNativeStatus,
    val bytesConsumed: Int = 0,
    val recordsSeen: Int = 0,
    val recordsDelivered: Int = 0,
    val unknownRecords: Int = 0,
    val firstSequence: Long = 0L,
    val lastSequence: Long = 0L,
    val error: String? = null,
)

internal class ArtTiNativeProtocolDecoder {
    private val recordView = ArtTiNativeRecordView()

    fun decode(
        source: ByteBuffer,
        bytesWritten: Int,
        visitor: ArtTiNativeRecordVisitor,
    ): ArtTiNativeDecodeResult {
        if (bytesWritten !in ArtTiNativeProtocol.BATCH_HEADER_SIZE..ArtTiNativeProtocol.MAX_BATCH_BYTES ||
            bytesWritten > source.capacity()
        ) {
            return invalid("Invalid native batch byte count: $bytesWritten")
        }
        val input = source.duplicate().order(ByteOrder.LITTLE_ENDIAN).apply {
            position(0)
            limit(bytesWritten)
        }
        if (input.int != ArtTiNativeProtocol.BATCH_MAGIC) return corrupt("Invalid native batch magic")
        val protocolVersion = input.short.toInt() and 0xFFFF
        if (protocolVersion != ArtTiNativeProtocol.PROTOCOL_VERSION) {
            return ArtTiNativeDecodeResult(
                status = ArtTiNativeStatus.UNSUPPORTED,
                error = "Unsupported native protocol version: $protocolVersion",
            )
        }
        val headerSize = input.short.toInt() and 0xFFFF
        val batchSize = input.int
        val recordCount = input.int
        val firstSequence = input.long
        val lastSequence = input.long
        if (headerSize !in ArtTiNativeProtocol.BATCH_HEADER_SIZE..batchSize ||
            batchSize !in headerSize..bytesWritten ||
            recordCount !in 0..ArtTiNativeProtocol.MAX_RECORDS_PER_BATCH
        ) {
            return corrupt("Invalid native batch header bounds")
        }
        val minimumBytes = headerSize.toLong() + recordCount.toLong() * Int.SIZE_BYTES
        if (minimumBytes > batchSize.toLong()) return corrupt("Native record count exceeds batch bounds")
        input.position(headerSize)
        input.limit(batchSize)

        var delivered = 0
        var unknown = 0
        var actualFirst = 0L
        var actualLast = 0L
        repeat(recordCount) { index ->
            if (input.remaining() < Int.SIZE_BYTES) return corrupt("Native record $index is truncated")
            val recordStart = input.position()
            val recordSize = input.int
            if (recordSize < ArtTiNativeProtocol.RECORD_SIZE ||
                recordSize > input.limit() - recordStart
            ) {
                return corrupt("Invalid native record $index size: $recordSize")
            }
            val recordEnd = recordStart + recordSize
            val type = input.short.toInt() and 0xFFFF
            val schemaVersion = input.short.toInt() and 0xFFFF
            if (schemaVersion == 0) return corrupt("Native record $index has schema version zero")
            val flags = input.int
            input.int // reserved
            val producerSequence = input.long
            if (index == 0) actualFirst = producerSequence
            actualLast = producerSequence
            if (type in MIN_KNOWN_EVENT_TYPE..MAX_KNOWN_EVENT_TYPE) {
                recordView.type = type
                recordView.schemaVersion = schemaVersion
                recordView.flags = flags
                recordView.producerSequence = producerSequence
                recordView.monotonicNs = input.long
                recordView.producerId = input.long
                recordView.threadToken = input.long
                recordView.contextToken = input.long
                recordView.payload0 = input.long
                recordView.payload1 = input.long
                recordView.payload2 = input.long
                recordView.payload3 = input.long
                visitor.onRecord(recordView)
                delivered++
            } else {
                unknown++
            }
            input.position(recordEnd)
        }
        if (input.position() != batchSize) return corrupt("Native batch has unaccounted record bytes")
        if ((recordCount == 0 && (firstSequence != 0L || lastSequence != 0L)) ||
            (recordCount > 0 && (firstSequence != actualFirst || lastSequence != actualLast))
        ) {
            return corrupt("Native batch sequence envelope does not match its records")
        }
        return ArtTiNativeDecodeResult(
            status = ArtTiNativeStatus.OK,
            bytesConsumed = batchSize,
            recordsSeen = recordCount,
            recordsDelivered = delivered,
            unknownRecords = unknown,
            firstSequence = firstSequence,
            lastSequence = lastSequence,
        )
    }

    private fun invalid(message: String) = ArtTiNativeDecodeResult(
        status = ArtTiNativeStatus.INVALID_ARGUMENT,
        error = message,
    )

    private fun corrupt(message: String) = ArtTiNativeDecodeResult(
        status = ArtTiNativeStatus.CORRUPT_INPUT,
        error = message,
    )

    private companion object {
        const val MIN_KNOWN_EVENT_TYPE = 1
        const val MAX_KNOWN_EVENT_TYPE = 11
    }
}
