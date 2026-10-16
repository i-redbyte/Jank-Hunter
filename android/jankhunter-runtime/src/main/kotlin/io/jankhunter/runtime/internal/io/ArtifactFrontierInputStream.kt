package io.jankhunter.runtime.internal.io

import java.io.EOFException
import java.io.File
import java.io.FileInputStream
import java.io.InputStream

/** Reads an immutable byte frontier of an append-only artifact without copying or buffering it. */
internal class ArtifactFrontierInputStream(file: File, size: Long) : InputStream() {
    private val input = FileInputStream(file)
    private var remaining = size

    init {
        if (size < 0L || input.channel.size() < size) {
            input.close()
            throw EOFException("Jank Hunter artifact is shorter than its captured frontier")
        }
    }

    override fun read(): Int {
        if (remaining == 0L) return -1
        val value = input.read()
        if (value < 0) throw EOFException("Jank Hunter artifact was truncated during export")
        remaining--
        return value
    }

    override fun read(bytes: ByteArray, offset: Int, length: Int): Int {
        if (offset < 0 || length < 0 || offset > bytes.size - length) throw IndexOutOfBoundsException()
        if (length == 0) return 0
        if (remaining == 0L) return -1
        val count = input.read(bytes, offset, minOf(length.toLong(), remaining).toInt())
        if (count < 0) throw EOFException("Jank Hunter artifact was truncated during export")
        remaining -= count
        return count
    }

    override fun close() = input.close()
}
