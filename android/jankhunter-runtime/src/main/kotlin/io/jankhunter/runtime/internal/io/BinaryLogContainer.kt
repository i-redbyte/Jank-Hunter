package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterBinaryWriter
import java.io.BufferedOutputStream
import java.io.Closeable
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.io.OutputStream
import java.security.MessageDigest

internal interface BinaryLogContainer : Closeable {
    val file: File?
    val path: String

    fun retainedBytes(): Long

    fun stats(): LogContainerStats

    fun canCommitRaw(rawBytes: Int, reservedBytes: Long): Boolean

    fun writeFileHeader(payload: ByteArray)

    fun commitChunk(
        flags: Int,
        sequence: Long,
        stored: ByteArray,
        storedSize: Int,
        rawSize: Int,
        recordCount: Int,
        rawCrc: Long,
        terminalReserveBytes: Long,
    ): Long

    fun flush()

    fun finishDigest(): ByteArray
}

internal class SequentialJhlogContainer private constructor(
    override val file: File?,
    override val path: String,
    output: OutputStream,
    initialBytesWritten: Long,
    maxPhysicalBytes: Long,
    private val archiveBudget: RunArchiveBudget?,
) : BinaryLogContainer {
    constructor(file: File, maxPhysicalBytes: Long, archiveBudget: RunArchiveBudget? = null) : this(
        file = file,
        path = file.absolutePath,
        output = FileOutputStream(file, false),
        initialBytesWritten = 0L,
        maxPhysicalBytes = maxPhysicalBytes,
        archiveBudget = archiveBudget,
    )

    constructor(writer: JankHunterBinaryWriter, maxPhysicalBytes: Long, archiveBudget: RunArchiveBudget? = null) : this(
        file = null,
        path = writer.path,
        output = ExternalBinaryOutputStream(writer),
        initialBytesWritten = writer.bytesWritten(),
        maxPhysicalBytes = maxPhysicalBytes,
        archiveBudget = archiveBudget,
    )

    private val output = BufferedOutputStream(output, IO_BUFFER_BYTES)
    private val digest = MessageDigest.getInstance("SHA-256")
    private val physicalByteLimit = maxPhysicalBytes.takeIf { it > 0L } ?: Long.MAX_VALUE
    private val chunkHeader = ByteArray(Jhlog.CHUNK_HEADER_BYTES).also { header ->
        System.arraycopy(Jhlog.CHUNK_MAGIC, 0, header, 0, Jhlog.CHUNK_MAGIC.size)
    }
    private val commitTrailer = ByteArray(Jhlog.COMMIT_TRAILER_BYTES).also { trailer ->
        System.arraycopy(Jhlog.COMMIT_MAGIC, 0, trailer, 0, Jhlog.COMMIT_MAGIC.size)
    }
    private var bytesWritten = initialBytesWritten

    init {
        if (initialBytesWritten != 0L) {
            runCatching { this.output.close() }
            throw IOException("JHLOG ${Jhlog.FORMAT_VERSION} segments must be opened empty: $path")
        }
    }

    override fun retainedBytes(): Long = bytesWritten

    override fun stats(): LogContainerStats = LogContainerStats(
        retainedBytes = bytesWritten,
        generatedBytes = bytesWritten,
        limitReachedCount = 0L,
        segmentRotationCount = 0L,
        archiveEvictedBytes = 0L,
    )

    override fun canCommitRaw(rawBytes: Int, reservedBytes: Long): Boolean {
        if (rawBytes < 0 || reservedBytes < 0L) return false
        if (physicalByteLimit == Long.MAX_VALUE) return true
        val maximumStoredBytes = gzipMaximumBytes(rawBytes.toLong())
        val physicalBytes = Jhlog.CHUNK_HEADER_BYTES.toLong() + maximumStoredBytes +
            Jhlog.COMMIT_TRAILER_BYTES.toLong()
        val remaining = physicalByteLimit - bytesWritten
        return remaining >= 0L && physicalBytes <= remaining - reservedBytes
    }

    override fun writeFileHeader(payload: ByteArray) {
        if (payload.size > Jhlog.MAX_FILE_HEADER_BYTES) {
            throw IOException("JHLOG ${Jhlog.FORMAT_VERSION} file header exceeds ${Jhlog.MAX_FILE_HEADER_BYTES} bytes")
        }
        val headerBytes = Jhlog.FILE_MAGIC.size.toLong() + 8L + payload.size.toLong()
        ensureCapacity(headerBytes, TERMINAL_RESERVE_BYTES)
        val fixed = ByteArray(Int.SIZE_BYTES * 2)
        putUInt32Le(fixed, 0, payload.size.toLong())
        putUInt32Le(fixed, Int.SIZE_BYTES, crc32(payload))
        archiveBudget?.claim(headerBytes, terminal = false)
        output.write(Jhlog.FILE_MAGIC)
        output.write(fixed)
        output.write(payload)
        output.flush()
        digest.update(Jhlog.FILE_MAGIC)
        digest.update(fixed)
        digest.update(payload)
        bytesWritten = headerBytes
    }

    override fun commitChunk(
        flags: Int,
        sequence: Long,
        stored: ByteArray,
        storedSize: Int,
        rawSize: Int,
        recordCount: Int,
        rawCrc: Long,
        terminalReserveBytes: Long,
    ): Long {
        val header = chunkHeader
        putUInt16Le(header, 4, Jhlog.CHUNK_HEADER_BYTES)
        putUInt16Le(header, 6, flags)
        putUInt32Le(header, 8, sequence)
        require(storedSize in 0..stored.size)
        putUInt32Le(header, 12, storedSize.toLong())
        putUInt32Le(header, 16, rawSize.toLong())
        putUInt32Le(header, 20, recordCount.toLong())
        putUInt32Le(header, 24, rawCrc)
        putUInt32Le(header, 28, crc32(header, 0, 28))

        val trailer = commitTrailer
        putUInt32Le(trailer, 4, sequence)
        putUInt32Le(trailer, 8, storedSize.toLong())
        putUInt32Le(trailer, 12, rawSize.toLong())
        putUInt32Le(trailer, 16, rawCrc)

        val physicalBytes = header.size.toLong() + storedSize.toLong() + trailer.size.toLong()
        ensureCapacity(physicalBytes, terminalReserveBytes)
        archiveBudget?.claim(physicalBytes, terminal = terminalReserveBytes == 0L)
        output.write(header)
        output.write(stored, 0, storedSize)
        output.write(trailer)
        output.flush()
        digest.update(header)
        digest.update(stored, 0, storedSize)
        digest.update(trailer)
        bytesWritten += physicalBytes
        return physicalBytes
    }

    override fun flush() = output.flush()

    override fun finishDigest(): ByteArray = digest.digest()

    override fun close() {
        try {
            output.close()
        } finally {
            archiveBudget?.close()
        }
    }

    private fun ensureCapacity(bytes: Long, reservedBytes: Long) {
        if (physicalByteLimit == Long.MAX_VALUE) return
        val remaining = physicalByteLimit - bytesWritten
        if (bytes < 0L || reservedBytes < 0L || remaining < 0L || bytes > remaining - reservedBytes) {
            throw LogSizeLimitReachedException(
                "JHLOG ${Jhlog.FORMAT_VERSION} physical size limit reached: " +
                    "path=$path limit=$physicalByteLimit written=$bytesWritten",
            )
        }
    }

    private class ExternalBinaryOutputStream(
        private val writer: JankHunterBinaryWriter,
    ) : OutputStream() {
        override fun write(oneByte: Int) = writeExternal { writer.writeByte(oneByte.toByte()) }

        override fun write(buffer: ByteArray, offset: Int, length: Int) {
            writeExternal { writer.writeBytes(buffer, offset, length) }
        }

        override fun flush() = writeExternal(writer::flush)

        override fun close() = writeExternal(writer::close)

        private inline fun writeExternal(action: () -> Unit) {
            try {
                action()
            } catch (error: IOException) {
                throw error
            } catch (error: Throwable) {
                throw IOException("External Jank Hunter binary writer failed", error)
            }
        }
    }

    private companion object {
        const val IO_BUFFER_BYTES = 32 * 1024
        const val TERMINAL_RESERVE_BYTES = 8L * 1024L

        fun gzipMaximumBytes(rawBytes: Long): Long {
            if (rawBytes <= 0L) return 64L
            val blocks = (rawBytes + DEFLATE_STORED_BLOCK_BYTES - 1L) / DEFLATE_STORED_BLOCK_BYTES
            return rawBytes + blocks * DEFLATE_STORED_BLOCK_OVERHEAD + GZIP_FIXED_OVERHEAD
        }

        const val DEFLATE_STORED_BLOCK_BYTES = 16_383L
        const val DEFLATE_STORED_BLOCK_OVERHEAD = 5L
        const val GZIP_FIXED_OVERHEAD = 64L
    }
}
