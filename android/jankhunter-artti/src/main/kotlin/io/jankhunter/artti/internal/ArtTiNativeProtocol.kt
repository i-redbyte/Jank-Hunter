package io.jankhunter.artti.internal

import java.nio.ByteBuffer
import java.nio.ByteOrder

internal object ArtTiNativeProtocol {
    const val ABI_VERSION = 1
    const val PROTOCOL_VERSION = 1
    const val BATCH_MAGIC = 0x424E484A
    const val BATCH_HEADER_SIZE = 32
    const val RECORD_SIZE = 88
    const val CONFIG_WIRE_SIZE = 72
    const val HANDSHAKE_WIRE_SIZE = 64
    const val MAX_BATCH_BYTES = 256 * 1024
    const val MAX_RECORDS_PER_BATCH = (MAX_BATCH_BYTES - BATCH_HEADER_SIZE) / RECORD_SIZE

    const val FEATURE_LENGTH_DELIMITED_RECORDS = 1L shl 0
    const val FEATURE_DIRECT_BUFFER_DRAIN = 1L shl 1
    const val FEATURE_PRODUCER_SEQUENCE = 1L shl 2
    const val FEATURE_UNKNOWN_RECORD_SKIP = 1L shl 3
    const val REQUIRED_FEATURES = FEATURE_LENGTH_DELIMITED_RECORDS or
        FEATURE_DIRECT_BUFFER_DRAIN or FEATURE_PRODUCER_SEQUENCE or FEATURE_UNKNOWN_RECORD_SKIP

    fun allocateHandshakeBuffer(): ByteBuffer = ByteBuffer.allocateDirect(HANDSHAKE_WIRE_SIZE)
        .order(ByteOrder.LITTLE_ENDIAN)

    fun allocateDrainBuffer(batchSize: Int): ByteBuffer {
        require(batchSize in 1..MAX_RECORDS_PER_BATCH)
        return ByteBuffer.allocateDirect(BATCH_HEADER_SIZE + batchSize * RECORD_SIZE)
            .order(ByteOrder.LITTLE_ENDIAN)
    }
}

internal data class ArtTiNativeHandshake(
    val abiVersion: Int,
    val protocolVersion: Int,
    val nativeEventSize: Int,
    val featureBits: Long,
    val batchHeaderSize: Int,
    val recordSize: Int,
    val maxBatchBytes: Int,
    val configWireSize: Int,
    val nativeMemoryBytes: Long,
    val configHash: Long,
) {
    fun isCompatible(): Boolean = abiVersion == ArtTiNativeProtocol.ABI_VERSION &&
        protocolVersion == ArtTiNativeProtocol.PROTOCOL_VERSION &&
        batchHeaderSize >= ArtTiNativeProtocol.BATCH_HEADER_SIZE &&
        recordSize >= ArtTiNativeProtocol.RECORD_SIZE &&
        maxBatchBytes in ArtTiNativeProtocol.BATCH_HEADER_SIZE..ArtTiNativeProtocol.MAX_BATCH_BYTES &&
        configWireSize >= ArtTiNativeProtocol.CONFIG_WIRE_SIZE &&
        featureBits and ArtTiNativeProtocol.REQUIRED_FEATURES == ArtTiNativeProtocol.REQUIRED_FEATURES

    companion object {
        fun decode(source: ByteBuffer): Result<ArtTiNativeHandshake> = runCatching {
            val input = source.duplicate().order(ByteOrder.LITTLE_ENDIAN)
            require(input.remaining() >= ArtTiNativeProtocol.HANDSHAKE_WIRE_SIZE) {
                "ART TI handshake is truncated"
            }
            val structSize = input.int
            require(structSize in ArtTiNativeProtocol.HANDSHAKE_WIRE_SIZE..input.remaining() + Int.SIZE_BYTES) {
                "Invalid ART TI handshake struct size: $structSize"
            }
            ArtTiNativeHandshake(
                abiVersion = input.int,
                protocolVersion = input.int,
                nativeEventSize = input.int,
                featureBits = input.long,
                batchHeaderSize = input.int,
                recordSize = input.int,
                maxBatchBytes = input.int,
                configWireSize = input.int,
                nativeMemoryBytes = input.long,
                configHash = input.long,
            )
        }
    }
}
