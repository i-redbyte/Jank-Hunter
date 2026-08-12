package io.jankhunter.sample

import java.io.ByteArrayInputStream
import java.io.File
import java.util.zip.GZIPInputStream

internal data class CommittedAgentEvent(
    val type: Int,
    val producerSequence: Long,
    val threadToken: Long,
    val contextToken: Long,
    val flags: Long,
    val payload0: Long,
    val payload1: Long,
    val payload2: Long,
    val payload3: Long,
)

/** Test-only structural reader: it accepts committed v9 chunks and never exports app payload. */
internal object CommittedAgentEventScanner {
    fun scan(file: File): List<CommittedAgentEvent> {
        val input = file.readBytes()
        require(input.size >= FILE_PREFIX_BYTES)
        val headerLength = uint32Le(input, FILE_MAGIC_BYTES).toInt()
        var offset = FILE_PREFIX_BYTES + headerLength
        val result = ArrayList<CommittedAgentEvent>()
        while (offset + CHUNK_HEADER_BYTES + COMMIT_TRAILER_BYTES <= input.size) {
            if (!input.regionMatches(offset, CHUNK_MAGIC)) break
            val flags = uint16Le(input, offset + 6)
            val storedLength = uint32Le(input, offset + 12).toInt()
            val rawLength = uint32Le(input, offset + 16).toInt()
            val recordCount = uint32Le(input, offset + 20).toInt()
            val payloadStart = offset + CHUNK_HEADER_BYTES
            val trailerStart = payloadStart + storedLength
            if (storedLength < 0 || rawLength < 0 || recordCount < 0 ||
                trailerStart + COMMIT_TRAILER_BYTES > input.size ||
                !input.regionMatches(trailerStart, COMMIT_MAGIC)
            ) {
                break
            }
            val stored = input.copyOfRange(payloadStart, trailerStart)
            val raw = if (flags and CHUNK_FLAG_GZIP != 0) {
                GZIPInputStream(ByteArrayInputStream(stored)).use { it.readBytes() }
            } else {
                stored
            }
            if (raw.size != rawLength) break
            scanChunk(raw, recordCount, result)
            offset = trailerStart + COMMIT_TRAILER_BYTES
        }
        return result
    }

    private fun scanChunk(raw: ByteArray, recordCount: Int, output: MutableList<CommittedAgentEvent>) {
        val cursor = Cursor(raw)
        repeat(recordCount) {
            val bodyLength = cursor.uvarint().toInt()
            val bodyEnd = cursor.offset + bodyLength
            if (bodyLength < 0 || bodyEnd !in cursor.offset..raw.size) return
            val recordType = cursor.uvarint().toInt()
            val envelope = cursor.uvarint()
            if (envelope and ENVELOPE_HAS_TIME != 0L) cursor.uvarint()
            if (envelope and ENVELOPE_HAS_THREAD != 0L) cursor.uvarint()
            if (envelope and ENVELOPE_HAS_CONTEXT != 0L && envelope and ENVELOPE_SAME_CONTEXT == 0L) {
                val presence = cursor.uvarint()
                repeat(4) { bit -> if (presence and (1L shl bit) != 0L) cursor.symbolRef() }
            }
            if (envelope and ENVELOPE_HAS_ATTRIBUTES != 0L) cursor.uvarint()
            if (recordType == TYPE_AGENT_EVENT && cursor.offset < bodyEnd) {
                val type = cursor.uvarint().toInt()
                cursor.uvarint() // schema version
                val sequence = cursor.uvarint()
                cursor.uvarint() // producer ID
                val threadToken = cursor.uvarint()
                val contextToken = cursor.uvarint()
                val eventFlags = cursor.uvarint()
                output += CommittedAgentEvent(
                    type = type,
                    producerSequence = sequence,
                    threadToken = threadToken,
                    contextToken = contextToken,
                    flags = eventFlags,
                    payload0 = cursor.uvarint(),
                    payload1 = cursor.uvarint(),
                    payload2 = cursor.uvarint(),
                    payload3 = cursor.uvarint(),
                )
            }
            cursor.offset = bodyEnd
        }
    }

    private fun ByteArray.regionMatches(offset: Int, expected: ByteArray): Boolean =
        offset >= 0 && offset + expected.size <= size && expected.indices.all { this[offset + it] == expected[it] }

    private fun uint16Le(bytes: ByteArray, offset: Int): Int =
        (bytes[offset].toInt() and 0xff) or ((bytes[offset + 1].toInt() and 0xff) shl 8)

    private fun uint32Le(bytes: ByteArray, offset: Int): Long {
        var value = 0L
        repeat(4) { index -> value = value or ((bytes[offset + index].toLong() and 0xffL) shl (index * 8)) }
        return value
    }

    private class Cursor(private val bytes: ByteArray) {
        var offset: Int = 0

        fun uvarint(): Long {
            var value = 0L
            var shift = 0
            while (offset < bytes.size && shift < 64) {
                val byte = bytes[offset++].toInt() and 0xff
                value = value or ((byte and 0x7f).toLong() shl shift)
                if (byte and 0x80 == 0) return value
                shift += 7
            }
            throw IllegalArgumentException("truncated uvarint")
        }

        fun symbolRef() {
            if (uvarint() == 1L) offset = (offset + Long.SIZE_BYTES).coerceAtMost(bytes.size)
        }
    }

    private const val FILE_MAGIC_BYTES = 8
    private const val FILE_PREFIX_BYTES = 16
    private const val CHUNK_HEADER_BYTES = 32
    private const val COMMIT_TRAILER_BYTES = 20
    private const val CHUNK_FLAG_GZIP = 1
    private const val TYPE_AGENT_EVENT = 17
    private const val ENVELOPE_HAS_TIME = 1L
    private const val ENVELOPE_HAS_THREAD = 1L shl 1
    private const val ENVELOPE_HAS_CONTEXT = 1L shl 2
    private const val ENVELOPE_SAME_CONTEXT = 1L shl 3
    private const val ENVELOPE_HAS_ATTRIBUTES = 1L shl 4
    private val CHUNK_MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'C'.code.toByte(), '9'.code.toByte())
    private val COMMIT_MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'C'.code.toByte(), 'M'.code.toByte())
}
