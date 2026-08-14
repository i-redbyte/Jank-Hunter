package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterBinaryWriter
import io.jankhunter.runtime.JankHunterRandomAccessBinaryWriter
import java.io.BufferedOutputStream
import java.io.Closeable
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.io.OutputStream
import java.io.RandomAccessFile

internal interface BinaryLogContainer : Closeable {
    val file: File?
    val path: String
    val usesChunkLocalDictionary: Boolean

    fun retainedBytes(): Long

    fun stats(): LogContainerStats

    fun overflowCount(): Long

    fun writeGrowthHistory(payload: ByteArray): Boolean

    fun writeGrowthLive(payload: ByteArray): Boolean

    fun writeFileHeader(payload: ByteArray)

    fun commitChunk(
        flags: Int,
        sequence: Long,
        stored: ByteArray,
        rawSize: Int,
        recordCount: Int,
        rawCrc: Long,
        terminalReserveBytes: Long,
    ): Long

    fun flush()
}

internal class SequentialV9LogContainer private constructor(
    override val file: File?,
    override val path: String,
    output: OutputStream,
    initialBytesWritten: Long,
    maxPhysicalBytes: Long,
) : BinaryLogContainer {
    constructor(file: File, maxPhysicalBytes: Long) : this(
        file = file,
        path = file.absolutePath,
        output = FileOutputStream(file, false),
        initialBytesWritten = 0L,
        maxPhysicalBytes = maxPhysicalBytes,
    )

    constructor(writer: JankHunterBinaryWriter, maxPhysicalBytes: Long) : this(
        file = null,
        path = writer.path,
        output = ExternalBinaryOutputStream(writer),
        initialBytesWritten = writer.bytesWritten(),
        maxPhysicalBytes = maxPhysicalBytes,
    )

    override val usesChunkLocalDictionary = false
    private val output = BufferedOutputStream(output, IO_BUFFER_BYTES)
    private val physicalByteLimit = maxPhysicalBytes.takeIf { it > 0L } ?: Long.MAX_VALUE
    private var bytesWritten = initialBytesWritten

    init {
        if (initialBytesWritten != 0L) {
            runCatching { this.output.close() }
            throw IOException("JHLOG v9 segments must be opened empty: $path")
        }
    }

    override fun retainedBytes(): Long = bytesWritten

    override fun stats(): LogContainerStats = LogContainerStats(
        retainedBytes = bytesWritten,
        generatedBytes = bytesWritten,
        overflowCount = 0L,
        evictedChunkCount = 0L,
        evictedBytes = 0L,
    )

    override fun overflowCount(): Long = 0L

    override fun writeGrowthHistory(payload: ByteArray): Boolean = false

    override fun writeGrowthLive(payload: ByteArray): Boolean = false

    override fun writeFileHeader(payload: ByteArray) {
        if (payload.size > JhlogV9.MAX_FILE_HEADER_BYTES) {
            throw IOException("JHLOG v9 file header exceeds ${JhlogV9.MAX_FILE_HEADER_BYTES} bytes")
        }
        val headerBytes = JhlogV9.FILE_MAGIC.size.toLong() + 8L + payload.size.toLong()
        ensureCapacity(headerBytes, TERMINAL_RESERVE_BYTES)
        output.write(JhlogV9.FILE_MAGIC)
        writeUInt32Le(output, payload.size.toLong())
        writeUInt32Le(output, crc32(payload))
        output.write(payload)
        output.flush()
        bytesWritten = headerBytes
    }

    override fun commitChunk(
        flags: Int,
        sequence: Long,
        stored: ByteArray,
        rawSize: Int,
        recordCount: Int,
        rawCrc: Long,
        terminalReserveBytes: Long,
    ): Long {
        val header = ByteArray(JhlogV9.CHUNK_HEADER_BYTES)
        System.arraycopy(JhlogV9.CHUNK_MAGIC, 0, header, 0, JhlogV9.CHUNK_MAGIC.size)
        putUInt16Le(header, 4, JhlogV9.CHUNK_HEADER_BYTES)
        putUInt16Le(header, 6, flags)
        putUInt32Le(header, 8, sequence)
        putUInt32Le(header, 12, stored.size.toLong())
        putUInt32Le(header, 16, rawSize.toLong())
        putUInt32Le(header, 20, recordCount.toLong())
        putUInt32Le(header, 24, rawCrc)
        putUInt32Le(header, 28, crc32(header, 0, 28))

        val trailer = ByteArray(JhlogV9.COMMIT_TRAILER_BYTES)
        System.arraycopy(JhlogV9.COMMIT_MAGIC, 0, trailer, 0, JhlogV9.COMMIT_MAGIC.size)
        putUInt32Le(trailer, 4, sequence)
        putUInt32Le(trailer, 8, stored.size.toLong())
        putUInt32Le(trailer, 12, rawSize.toLong())
        putUInt32Le(trailer, 16, rawCrc)

        val physicalBytes = header.size.toLong() + stored.size.toLong() + trailer.size.toLong()
        ensureCapacity(physicalBytes, terminalReserveBytes)
        output.write(header)
        output.write(stored)
        output.write(trailer)
        output.flush()
        bytesWritten += physicalBytes
        return physicalBytes
    }

    override fun flush() = output.flush()

    override fun close() = output.close()

    private fun ensureCapacity(bytes: Long, reservedBytes: Long) {
        if (physicalByteLimit == Long.MAX_VALUE) return
        val remaining = physicalByteLimit - bytesWritten
        if (bytes < 0L || reservedBytes < 0L || remaining < 0L || bytes > remaining - reservedBytes) {
            throw LogSizeLimitReachedException(
                "JHLOG v9 physical size limit reached: path=$path limit=$physicalByteLimit written=$bytesWritten",
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
    }
}

internal class CircularV1LogContainer private constructor(
    override val file: File?,
    override val path: String,
    private val access: RandomAccessLog,
    maxPhysicalBytes: Long,
) : BinaryLogContainer {
    constructor(file: File, maxPhysicalBytes: Long) : this(
        file = file,
        path = file.absolutePath,
        access = FileRandomAccessLog(file),
        maxPhysicalBytes = maxPhysicalBytes,
    )

    constructor(writer: JankHunterRandomAccessBinaryWriter, maxPhysicalBytes: Long) : this(
        file = null,
        path = writer.path,
        access = ExternalRandomAccessLog(writer),
        maxPhysicalBytes = maxPhysicalBytes,
    )

    override val usesChunkLocalDictionary = true
    private val physicalByteLimit = maxPhysicalBytes.takeIf { it > 0L } ?: Long.MAX_VALUE
    private val arenaCapacity: Long
    private var generation = 0L
    private var head = 0L
    private var tail = 0L
    private var usedBytes = 0L
    private var firstSequence = 0L
    private var nextSequence = 0L
    private var chunkCount = 0L
    private var wraps = 0L
    private var evictedChunks = 0L
    private var evictedBytes = 0L
    private var generatedBytes = 0L
    private var retainedBytes = 0L
    private var closed = false

    init {
        if (access.size() != 0L) {
            runCatching { access.close() }
            throw IOException("JHLOG 1.0 files must be opened empty: $path")
        }
        if (physicalByteLimit == Long.MAX_VALUE) {
            runCatching { access.close() }
            throw IOException("JHLOG 1.0 requires a finite physical size limit: $path")
        }
        if (physicalByteLimit < JhlogV1.MIN_FILE_BYTES) {
            runCatching { access.close() }
            throw IOException(
                "JHLOG 1.0 size limit $physicalByteLimit is below ${JhlogV1.MIN_FILE_BYTES}: $path",
            )
        }
        arenaCapacity = physicalByteLimit - JhlogV1.ARENA_OFFSET
    }

    override fun retainedBytes(): Long = retainedBytes

    override fun stats(): LogContainerStats = LogContainerStats(
        retainedBytes = retainedBytes,
        generatedBytes = saturatedAdd(JhlogV1.ARENA_OFFSET, generatedBytes),
        overflowCount = wraps,
        evictedChunkCount = evictedChunks,
        evictedBytes = evictedBytes,
    )

    override fun overflowCount(): Long = wraps

    override fun writeGrowthHistory(payload: ByteArray): Boolean {
        checkOpen()
        if (payload.isEmpty() || payload.size > JhlogV1.HISTORY_MAX_BYTES) return false
        access.write(JhlogV1.HISTORY_OFFSET, payload, 0, payload.size)
        access.flush()
        return true
    }

    override fun writeGrowthLive(payload: ByteArray): Boolean {
        checkOpen()
        if (payload.size != LogGrowthWire.LIVE_BYTES) return false
        val generation = uint64Le(payload, 8)
        val offset = if (generation and 1L == 0L) {
            JhlogV1.LIVE_SUMMARY_A_OFFSET
        } else {
            JhlogV1.LIVE_SUMMARY_B_OFFSET
        }
        access.write(offset, payload, 0, payload.size)
        access.flush()
        return true
    }

    override fun writeFileHeader(payload: ByteArray) {
        checkOpen()
        if (payload.size > JhlogV1.MAX_FILE_HEADER_BYTES) {
            throw IOException("JHLOG 1.0 file header exceeds ${JhlogV1.MAX_FILE_HEADER_BYTES} bytes")
        }
        val prefix = ByteArray(JhlogV1.FILE_HEADER_OFFSET.toInt())
        System.arraycopy(JhlogV1.FILE_PREFIX, 0, prefix, 0, JhlogV1.FILE_PREFIX.size)
        access.write(0L, prefix, 0, prefix.size)

        val header = ByteArray(8 + payload.size)
        putUInt32Le(header, 0, payload.size.toLong())
        putUInt32Le(header, 4, crc32(payload))
        System.arraycopy(payload, 0, header, 8, payload.size)
        access.write(JhlogV1.FILE_HEADER_OFFSET, header, 0, header.size)

        retainedBytes = maxOf(JhlogV1.ARENA_OFFSET, JhlogV1.FILE_HEADER_OFFSET + header.size)
        access.truncate(retainedBytes)
        publishSuperblock(force = true)
    }

    override fun commitChunk(
        flags: Int,
        sequence: Long,
        stored: ByteArray,
        rawSize: Int,
        recordCount: Int,
        rawCrc: Long,
        terminalReserveBytes: Long,
    ): Long {
        checkOpen()
        // A circular arena never reserves terminal space: final records may evict the oldest
        // committed chunks while the physical file remains bounded. Sequential v9 cannot evict
        // and therefore accounts for terminalReserveBytes before accepting an ordinary chunk.
        if (sequence != nextSequence) {
            throw IOException("JHLOG 1.0 chunk sequence $sequence, expected $nextSequence")
        }
        val header = chunkHeader(flags, sequence, stored.size, rawSize, recordCount, rawCrc)
        val trailer = commitTrailer(sequence, stored.size, rawSize, rawCrc)
        val totalBytes = header.size.toLong() + stored.size.toLong() + trailer.size.toLong()
        if (totalBytes > arenaCapacity) {
            throw IOException("JHLOG 1.0 chunk $totalBytes exceeds arena $arenaCapacity: $path")
        }

        var evictedForWrite = false
        while (!fitsAtTail(totalBytes)) {
            if (chunkCount == 0L) {
                tail = 0L
                break
            }
            evictOldest()
            evictedForWrite = true
        }
        if (tail + totalBytes > arenaCapacity) {
            tail = 0L
            while (!fitsAtTail(totalBytes)) {
                evictOldest()
                evictedForWrite = true
            }
        }
        if (evictedForWrite) {
            wraps++
            publishSuperblock(force = false)
        }

        val chunkOffset = JhlogV1.ARENA_OFFSET + tail
        access.write(chunkOffset, header, 0, header.size)
        access.write(chunkOffset + header.size, stored, 0, stored.size)
        access.write(chunkOffset + header.size + stored.size, trailer, 0, trailer.size)

        if (chunkCount == 0L) {
            head = tail
            firstSequence = sequence
        }
        tail += totalBytes
        usedBytes += totalBytes
        chunkCount++
        nextSequence++
        generatedBytes += totalBytes
        retainedBytes = maxOf(retainedBytes, chunkOffset + totalBytes)
        publishSuperblock(force = false)
        return totalBytes
    }

    override fun flush() = access.flush()

    override fun close() {
        if (closed) return
        closed = true
        try {
            access.flush()
        } finally {
            access.close()
        }
    }

    private fun fitsAtTail(bytes: Long): Boolean {
        if (chunkCount == 0L) return bytes <= arenaCapacity
        return if (tail < head) {
            bytes <= head - tail
        } else {
            bytes <= arenaCapacity - tail || bytes <= head
        }
    }

    private fun evictOldest() {
        if (chunkCount <= 0L) throw IOException("JHLOG 1.0 cannot evict from an empty arena")
        var chunkBytes = chunkBytesAt(head)
        if (chunkBytes == null && head != 0L) {
            head = 0L
            chunkBytes = chunkBytesAt(head)
        }
        val removed = chunkBytes ?: throw IOException("JHLOG 1.0 cannot read oldest chunk at $head")
        head += removed
        if (head >= arenaCapacity) head = 0L
        usedBytes -= removed
        chunkCount--
        firstSequence++
        evictedChunks++
        evictedBytes += removed
        if (chunkCount == 0L) {
            head = tail
            usedBytes = 0L
            firstSequence = nextSequence
        }
    }

    private fun chunkBytesAt(relativeOffset: Long): Long? {
        if (relativeOffset < 0L || relativeOffset + JhlogV1.CHUNK_HEADER_BYTES > arenaCapacity) return null
        val raw = ByteArray(JhlogV1.CHUNK_HEADER_BYTES)
        try {
            access.readFully(JhlogV1.ARENA_OFFSET + relativeOffset, raw, 0, raw.size)
        } catch (_: IOException) {
            return null
        }
        if (!raw.startsWith(JhlogV1.CHUNK_MAGIC)) return null
        if (uint16Le(raw, 4) != JhlogV1.CHUNK_HEADER_BYTES) return null
        val storedHeaderCrc = uint32Le(raw, 32)
        if (storedHeaderCrc != crc32(raw, 0, 32)) return null
        val storedBytes = uint32Le(raw, 16)
        val total = JhlogV1.CHUNK_HEADER_BYTES.toLong() + storedBytes + JhlogV1.COMMIT_TRAILER_BYTES.toLong()
        return total.takeIf { it > 0L && relativeOffset + it <= arenaCapacity }
    }

    private fun publishSuperblock(force: Boolean) {
        generation++
        val block = ByteArray(JhlogV1.SUPERBLOCK_BYTES.toInt())
        System.arraycopy(JhlogV1.SUPERBLOCK_MAGIC, 0, block, 0, JhlogV1.SUPERBLOCK_MAGIC.size)
        putUInt16Le(block, 4, JhlogV1.SUPERBLOCK_SCHEMA)
        putUInt16Le(block, 6, 0)
        putUInt64Le(block, 8, generation)
        putUInt64Le(block, 16, arenaCapacity)
        putUInt64Le(block, 24, head)
        putUInt64Le(block, 32, tail)
        putUInt64Le(block, 40, usedBytes)
        putUInt64Le(block, 48, firstSequence)
        putUInt64Le(block, 56, nextSequence)
        putUInt64Le(block, 64, chunkCount)
        putUInt64Le(block, 72, wraps)
        putUInt64Le(block, 80, evictedChunks)
        putUInt64Le(block, 88, evictedBytes)
        putUInt64Le(block, 96, generatedBytes)
        putUInt64Le(block, 104, retainedBytes)
        putUInt32Le(block, block.size - Int.SIZE_BYTES, crc32(block, 0, block.size - Int.SIZE_BYTES))
        val offset = if (generation and 1L == 0L) JhlogV1.SUPERBLOCK_A_OFFSET else JhlogV1.SUPERBLOCK_B_OFFSET
        access.write(offset, block, 0, block.size)
        if (force) access.flush()
    }

    private fun chunkHeader(
        flags: Int,
        sequence: Long,
        storedSize: Int,
        rawSize: Int,
        records: Int,
        rawCrc: Long,
    ): ByteArray {
        val header = ByteArray(JhlogV1.CHUNK_HEADER_BYTES)
        System.arraycopy(JhlogV1.CHUNK_MAGIC, 0, header, 0, JhlogV1.CHUNK_MAGIC.size)
        putUInt16Le(header, 4, JhlogV1.CHUNK_HEADER_BYTES)
        putUInt16Le(header, 6, flags)
        putUInt64Le(header, 8, sequence)
        putUInt32Le(header, 16, storedSize.toLong())
        putUInt32Le(header, 20, rawSize.toLong())
        putUInt32Le(header, 24, records.toLong())
        putUInt32Le(header, 28, rawCrc)
        putUInt32Le(header, 32, crc32(header, 0, 32))
        return header
    }

    private fun commitTrailer(sequence: Long, storedSize: Int, rawSize: Int, rawCrc: Long): ByteArray {
        val trailer = ByteArray(JhlogV1.COMMIT_TRAILER_BYTES)
        System.arraycopy(JhlogV1.COMMIT_MAGIC, 0, trailer, 0, JhlogV1.COMMIT_MAGIC.size)
        putUInt64Le(trailer, 4, sequence)
        putUInt32Le(trailer, 12, storedSize.toLong())
        putUInt32Le(trailer, 16, rawSize.toLong())
        putUInt32Le(trailer, 20, rawCrc)
        return trailer
    }

    private fun checkOpen() {
        if (closed) throw IOException("JHLOG 1.0 container is closed")
    }
}

private interface RandomAccessLog : Closeable {
    fun size(): Long

    fun readFully(position: Long, target: ByteArray, offset: Int, length: Int)

    fun write(position: Long, source: ByteArray, offset: Int, length: Int)

    fun truncate(size: Long)

    fun flush()
}

private class FileRandomAccessLog(file: File) : RandomAccessLog {
    private val randomAccess = RandomAccessFile(file, "rw")

    override fun size(): Long = randomAccess.length()

    override fun readFully(position: Long, target: ByteArray, offset: Int, length: Int) {
        randomAccess.seek(position)
        randomAccess.readFully(target, offset, length)
    }

    override fun write(position: Long, source: ByteArray, offset: Int, length: Int) {
        randomAccess.seek(position)
        randomAccess.write(source, offset, length)
    }

    override fun truncate(size: Long) = randomAccess.setLength(size)

    override fun flush() = randomAccess.fd.sync()

    override fun close() = randomAccess.close()
}

private class ExternalRandomAccessLog(
    private val writer: JankHunterRandomAccessBinaryWriter,
) : RandomAccessLog {
    override fun size(): Long = writer.sizeBytes()

    override fun readFully(position: Long, target: ByteArray, offset: Int, length: Int) {
        writer.readBytes(position, target, offset, length)
    }

    override fun write(position: Long, source: ByteArray, offset: Int, length: Int) {
        writer.writeBytes(position, source, offset, length)
    }

    override fun truncate(size: Long) = writer.truncate(size)

    override fun flush() = writer.flush()

    override fun close() = writer.close()
}

private fun ByteArray.startsWith(prefix: ByteArray): Boolean {
    if (size < prefix.size) return false
    for (index in prefix.indices) {
        if (this[index] != prefix[index]) return false
    }
    return true
}

private fun writeUInt32Le(output: OutputStream, value: Long) {
    repeat(Int.SIZE_BYTES) { index -> output.write((value ushr (index * Byte.SIZE_BITS)).toInt() and 0xff) }
}
