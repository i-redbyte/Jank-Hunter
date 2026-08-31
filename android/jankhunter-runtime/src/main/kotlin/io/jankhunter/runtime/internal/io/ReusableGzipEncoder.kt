package io.jankhunter.runtime.internal.io

import java.io.Closeable
import java.io.IOException
import java.util.zip.CRC32
import java.util.zip.Deflater

/** Reuses native deflater, scratch storage, and its output buffer across synchronously consumed chunks. */
internal class ReusableGzipEncoder : Closeable {
    private val deflater = Deflater(Deflater.DEFAULT_COMPRESSION, true)
    private val checksum = CRC32()
    private val scratch = ByteArray(DEFLATE_BUFFER_BYTES)
    private val trailer = ByteArray(GZIP_TRAILER_BYTES)
    private val output = ReusableByteArrayOutput(DEFAULT_FRAME_CAPACITY)
    private var closed = false

    val buffer: ByteArray
        get() = output.buffer

    val size: Int
        get() = output.length

    val rawCrc: Long
        get() = checksum.value

    fun encode(raw: ByteArray, length: Int) {
        check(!closed) { "gzip encoder is closed" }
        require(length in 0..raw.size)
        output.reset()
        output.write(GZIP_HEADER)
        checksum.reset()
        checksum.update(raw, 0, length)
        deflater.reset()
        deflater.setInput(raw, 0, length)
        deflater.finish()
        while (!deflater.finished()) {
            val written = deflater.deflate(scratch)
            if (written <= 0) throw IOException("gzip deflater made no progress")
            output.write(scratch, 0, written)
        }
        putUInt32Le(trailer, 0, checksum.value)
        putUInt32Le(trailer, Int.SIZE_BYTES, length.toLong())
        output.write(trailer)
    }

    override fun close() {
        if (closed) return
        closed = true
        deflater.end()
    }

    private companion object {
        const val DEFLATE_BUFFER_BYTES = 32 * 1024
        const val DEFAULT_FRAME_CAPACITY = 64 * 1024
        const val GZIP_TRAILER_BYTES = 8
        val GZIP_HEADER = byteArrayOf(
            0x1f,
            0x8b.toByte(),
            Deflater.DEFLATED.toByte(),
            0,
            0,
            0,
            0,
            0,
            0,
            0,
        )
    }
}
