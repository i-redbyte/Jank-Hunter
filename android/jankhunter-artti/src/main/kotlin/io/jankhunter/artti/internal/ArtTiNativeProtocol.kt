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
    const val METHOD_MAGIC = 0x444D484A
    const val METHOD_PROTOCOL_VERSION = 1
    const val METHOD_HEADER_SIZE = 32
    const val MAX_METHOD_DEFINITION_BYTES = 4 * 1024

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

internal data class ArtTiMethodDefinition(
    val methodId: Long,
    val classSignature: String,
    val methodName: String,
    val methodSignature: String,
) {
    companion object {
        fun allocateBuffer(): ByteBuffer = ByteBuffer
            .allocateDirect(ArtTiNativeProtocol.MAX_METHOD_DEFINITION_BYTES)
            .order(ByteOrder.LITTLE_ENDIAN)

        fun decode(source: ByteBuffer, bytesWritten: Int): Result<ArtTiMethodDefinition> = runCatching {
            require(bytesWritten in ArtTiNativeProtocol.METHOD_HEADER_SIZE..ArtTiNativeProtocol.MAX_METHOD_DEFINITION_BYTES) {
                "Invalid ART TI method definition size: $bytesWritten"
            }
            require(bytesWritten <= source.capacity()) { "ART TI method definition exceeds its buffer" }
            val input = source.duplicate().order(ByteOrder.LITTLE_ENDIAN).apply {
                position(0)
                limit(bytesWritten)
            }
            require(input.int == ArtTiNativeProtocol.METHOD_MAGIC) { "Invalid ART TI method definition magic" }
            val protocolVersion = input.short.toInt() and 0xFFFF
            require(protocolVersion == ArtTiNativeProtocol.METHOD_PROTOCOL_VERSION) {
                "Unsupported ART TI method definition protocol: $protocolVersion"
            }
            val headerSize = input.short.toInt() and 0xFFFF
            val totalSize = input.int
            input.int // flags
            val methodId = input.long
            val classLength = input.short.toInt() and 0xFFFF
            val nameLength = input.short.toInt() and 0xFFFF
            val signatureLength = input.short.toInt() and 0xFFFF
            input.short // reserved
            val payloadSize = classLength.toLong() + nameLength.toLong() + signatureLength.toLong()
            require(headerSize in ArtTiNativeProtocol.METHOD_HEADER_SIZE..totalSize) {
                "Invalid ART TI method header size: $headerSize"
            }
            require(totalSize == bytesWritten && payloadSize == totalSize.toLong() - headerSize) {
                "Invalid ART TI method definition bounds"
            }
            require(methodId != 0L) { "ART TI method definition has a zero method ID" }
            input.position(headerSize)
            val classBytes = ByteArray(classLength).also(input::get)
            val nameBytes = ByteArray(nameLength).also(input::get)
            val signatureBytes = ByteArray(signatureLength).also(input::get)
            ArtTiMethodDefinition(
                methodId = methodId,
                classSignature = classBytes.toString(Charsets.UTF_8),
                methodName = nameBytes.toString(Charsets.UTF_8),
                methodSignature = signatureBytes.toString(Charsets.UTF_8),
            )
        }
    }
}

internal data class ArtTiNativeHandshake(
    val abiVersion: Int,
    val protocolVersion: Int,
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
            val abiVersion = input.int
            val protocolVersion = input.int
            input.int // native event size is diagnostic-only and not part of compatibility checks
            ArtTiNativeHandshake(
                abiVersion = abiVersion,
                protocolVersion = protocolVersion,
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
