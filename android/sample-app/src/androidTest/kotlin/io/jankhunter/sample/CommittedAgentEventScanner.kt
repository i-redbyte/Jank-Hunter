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

/** Test-only structural reader for committed JHLOG 5.1 agent semantics (including micro-pages). */
internal object CommittedAgentEventScanner {
    fun scan(file: File): List<CommittedAgentEvent> {
        val input = file.readBytes()
        require(input.size >= FILE_PREFIX_BYTES)
        val headerLength = uint32Le(input, MAGIC_SIZE).toInt()
        var offset = FILE_PREFIX_BYTES + headerLength
        val result = ArrayList<CommittedAgentEvent>()
        while (offset + CHUNK_HEADER_BYTES + COMMIT_TRAILER_BYTES <= input.size) {
            if (!input.regionMatches(offset, CHUNK_MAGIC)) break
            val flags = uint16Le(input, offset + 6)
            val storedLength = uint32Le(input, offset + 12).toInt()
            val payloadStart = offset + CHUNK_HEADER_BYTES
            val trailerStart = payloadStart + storedLength
            if (storedLength < 0 || trailerStart + COMMIT_TRAILER_BYTES > input.size ||
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
            scanChunk(raw, result)
            offset = trailerStart + COMMIT_TRAILER_BYTES
        }
        return result
    }

    private fun scanChunk(raw: ByteArray, output: MutableList<CommittedAgentEvent>) {
        var offset = 0
        while (offset < raw.size) {
            val length = readUvarint(raw, offset) ?: break
            val bodyStart = length.nextOffset
            val bodyEnd = bodyStart + length.value.toInt()
            if (bodyEnd < bodyStart || bodyEnd > raw.size) break
            var cursor = bodyStart
            val type = readUvarint(raw, cursor) ?: break
            cursor = type.nextOffset
            val flags = readUvarint(raw, cursor) ?: break
            cursor = flags.nextOffset
            if (flags.value and ENVELOPE_HAS_TIME != 0L) {
                cursor = readUvarint(raw, cursor)?.nextOffset ?: break
            }
            if (flags.value and ENVELOPE_HAS_THREAD != 0L) {
                cursor = readUvarint(raw, cursor)?.nextOffset ?: break
            }
            if (flags.value and ENVELOPE_HAS_CONTEXT != 0L && flags.value and ENVELOPE_SAME_CONTEXT == 0L) {
                val presence = readUvarint(raw, cursor) ?: break
                cursor = presence.nextOffset
                if (presence.value and CONTEXT_SCREEN != 0L) cursor = skipSymbolRef(raw, cursor) ?: break
                if (presence.value and CONTEXT_OWNER != 0L) cursor = skipSymbolRef(raw, cursor) ?: break
                if (presence.value and CONTEXT_OPERATION != 0L) {
                    cursor = readUvarint(raw, cursor)?.nextOffset ?: break
                }
            }
            if (flags.value and ENVELOPE_HAS_ATTRIBUTES != 0L) {
                cursor = readUvarint(raw, cursor)?.nextOffset ?: break
            }
            when (type.value.toInt()) {
                TYPE_AGENT -> parseAgentPayload(raw, cursor, bodyEnd, output)
                TYPE_MICRO_PAGE -> parseMicroPageAgents(raw, cursor, output)
            }
            offset = bodyEnd
        }
    }

    private fun parseMicroPageAgents(raw: ByteArray, start: Int, output: MutableList<CommittedAgentEvent>) {
        val pageHeader = readUvarint(raw, start) ?: return
        var cursor = pageHeader.nextOffset
        val rowCount: Int
        val codecMask: Long
        if (pageHeader.value == 0L) {
            val encodedRows = readUvarint(raw, cursor) ?: return
            cursor = encodedRows.nextOffset
            val encodedMask = readUvarint(raw, cursor) ?: return
            cursor = encodedMask.nextOffset
            rowCount = encodedRows.value.toInt()
            codecMask = encodedMask.value
        } else {
            rowCount = pageHeader.value.toInt()
            codecMask = 0L
        }
        if (rowCount !in 1..MAX_MICRO_PAGE_ROWS) return
        val sections = Array(8) { ByteArray(0) }
        repeat(8) { section ->
            val length = readUvarint(raw, cursor) ?: return
            cursor = length.nextOffset
            val decodedLength = length.value.toInt()
            if (decodedLength < 0) return
            val encodedLength = if (codecMask and (1L shl section) == 0L) {
                decodedLength
            } else {
                val encoded = readUvarint(raw, cursor) ?: return
                cursor = encoded.nextOffset
                encoded.value.toInt()
            }
            val sectionEnd = cursor + encodedLength
            if (encodedLength < 0 || sectionEnd < cursor || sectionEnd > raw.size) return
            sections[section] = if (codecMask and (1L shl section) == 0L) {
                raw.copyOfRange(cursor, sectionEnd)
            } else {
                decodeRansSection(raw.copyOfRange(cursor, sectionEnd), decodedLength)
            }
            cursor = sectionEnd
        }
        val maskBytes = (rowCount + 7) / 8
        if (sections[1].size != maskBytes * 6) return

        fun mask(mask: Int, row: Int): Boolean {
            val byteOffset = mask * maskBytes + row / 8
            return sections[1][byteOffset].toInt() and (1 shl (row % 8)) != 0
        }

        fun recordType(row: Int): Int {
            val bitOffset = row * 5
            val byteOffset = bitOffset / 8
            val shift = bitOffset % 8
            var value = (sections[0][byteOffset].toInt() and 0xff) ushr shift
            if (shift > 3 && byteOffset + 1 < sections[0].size) {
                value = value or ((sections[0][byteOffset + 1].toInt() and 0xff) shl (8 - shift))
            }
            return (value and 0x1f) + 1
        }

        var lengthCursor = 0
        var payloadOffset = 0
        repeat(rowCount) { row ->
            if (recordType(row) != TYPE_AGENT) {
                val payloadLength = readUvarint(sections[6], lengthCursor) ?: return
                lengthCursor = payloadLength.nextOffset
                payloadOffset += payloadLength.value.toInt()
                return@repeat
            }
            val payloadLength = readUvarint(sections[6], lengthCursor) ?: return
            lengthCursor = payloadLength.nextOffset
            val payloadEnd = payloadOffset + payloadLength.value.toInt()
            if (payloadEnd < payloadOffset || payloadEnd > sections[7].size) return
            parseAgentPayload(sections[7], payloadOffset, payloadEnd, output)
            payloadOffset = payloadEnd
        }
    }

    private fun parseAgentPayload(
        bytes: ByteArray,
        start: Int,
        end: Int,
        output: MutableList<CommittedAgentEvent>,
    ) {
        if (start >= end) return
        var cursor = start
        val type = readUvarint(bytes, cursor) ?: return
        cursor = type.nextOffset
        cursor = readUvarint(bytes, cursor)?.nextOffset ?: return // schema version
        val sequence = readUvarint(bytes, cursor) ?: return
        cursor = sequence.nextOffset
        cursor = readUvarint(bytes, cursor)?.nextOffset ?: return // producer id
        val threadToken = readUvarint(bytes, cursor) ?: return
        cursor = threadToken.nextOffset
        val contextToken = readUvarint(bytes, cursor) ?: return
        cursor = contextToken.nextOffset
        val eventFlags = readUvarint(bytes, cursor) ?: return
        cursor = eventFlags.nextOffset
        val payload0 = readUvarint(bytes, cursor) ?: return
        cursor = payload0.nextOffset
        val payload1 = readUvarint(bytes, cursor) ?: return
        cursor = payload1.nextOffset
        val payload2 = readUvarint(bytes, cursor) ?: return
        cursor = payload2.nextOffset
        val payload3 = readUvarint(bytes, cursor) ?: return
        output += CommittedAgentEvent(
            type = type.value.toInt(),
            producerSequence = sequence.value,
            threadToken = threadToken.value,
            contextToken = contextToken.value,
            flags = eventFlags.value,
            payload0 = payload0.value,
            payload1 = payload1.value,
            payload2 = payload2.value,
            payload3 = payload3.value,
        )
    }

    private fun decodeRansSection(frame: ByteArray, decodedLength: Int): ByteArray {
        var cursor = 0
        fun uvarint(): Long {
            var value = 0L
            var shift = 0
            while (cursor < frame.size && shift < 64) {
                val current = frame[cursor++].toInt() and 0xff
                value = value or ((current and 0x7f).toLong() shl shift)
                if (current < 0x80) return value
                shift += 7
            }
            throw IllegalArgumentException("invalid rANS varint")
        }
        val symbolCount = uvarint().toInt()
        require(symbolCount in 1..256)
        val frequencies = IntArray(256)
        val starts = IntArray(256)
        val table = ByteArray(1 shl 12)
        var cumulative = 0
        var previous = -1
        repeat(symbolCount) {
            val symbol = frame[cursor++].toInt() and 0xff
            require(symbol > previous)
            val frequency = uvarint().toInt()
            require(frequency > 0 && cumulative + frequency <= table.size)
            starts[symbol] = cumulative
            frequencies[symbol] = frequency
            table.fill(symbol.toByte(), cumulative, cumulative + frequency)
            cumulative += frequency
            previous = symbol
        }
        require(cumulative == table.size && cursor + 8 <= frame.size)
        var state = 0L
        repeat(8) { index ->
            state = state or ((frame[cursor++].toLong() and 0xffL) shl (index * 8))
        }
        val output = ByteArray(decodedLength)
        for (index in output.indices) {
            val slot = (state and (table.size - 1).toLong()).toInt()
            val symbol = table[slot].toInt() and 0xff
            output[index] = symbol.toByte()
            state = frequencies[symbol].toLong() * (state ushr 12) + slot - starts[symbol]
            while (state < 1L shl 23) state = (state shl 8) or (frame[cursor++].toLong() and 0xffL)
        }
        require(state == 1L shl 23 && cursor == frame.size)
        return output
    }

    private data class Uvarint(val value: Long, val nextOffset: Int)

    private fun readUvarint(bytes: ByteArray, offset: Int): Uvarint? {
        var value = 0L
        var shift = 0
        var cursor = offset
        while (cursor < bytes.size && shift < 64) {
            val byte = bytes[cursor++].toInt() and 0xff
            value = value or ((byte and 0x7f).toLong() shl shift)
            if (byte and 0x80 == 0) return Uvarint(value, cursor)
            shift += 7
        }
        return null
    }

    private fun skipSymbolRef(bytes: ByteArray, offset: Int): Int? {
        val kind = readUvarint(bytes, offset) ?: return null
        return if (kind.value == 1L) kind.nextOffset + 8 else kind.nextOffset
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

    private const val MAGIC_SIZE = 11
    private const val FILE_PREFIX_BYTES = MAGIC_SIZE + 8
    private const val CHUNK_HEADER_BYTES = 32
    private const val COMMIT_TRAILER_BYTES = 20
    private const val CHUNK_FLAG_GZIP = 1
    private const val TYPE_AGENT = 28
    private const val TYPE_MICRO_PAGE = 27
    private const val MAX_MICRO_PAGE_ROWS = 128
    private const val ENVELOPE_HAS_TIME = 1L
    private const val ENVELOPE_HAS_THREAD = 1L shl 1
    private const val ENVELOPE_HAS_CONTEXT = 1L shl 2
    private const val ENVELOPE_SAME_CONTEXT = 1L shl 3
    private const val ENVELOPE_HAS_ATTRIBUTES = 1L shl 4
    private const val CONTEXT_SCREEN = 1L
    private const val CONTEXT_OWNER = 1L shl 1
    private const val CONTEXT_OPERATION = 1L shl 2
    private val CHUNK_MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'C'.code.toByte(), '1'.code.toByte())
    private val COMMIT_MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'C'.code.toByte(), 'M'.code.toByte())
}
