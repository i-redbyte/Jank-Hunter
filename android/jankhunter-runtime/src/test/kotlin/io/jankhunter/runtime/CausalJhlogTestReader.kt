package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.Jhlog
import io.jankhunter.runtime.internal.io.ProcessRecordingSession
import java.io.ByteArrayInputStream
import java.io.File
import java.util.zip.GZIPInputStream

internal data class RecordedCausalEvent(
    val kind: Long,
    val phase: Long,
    val parentKind: Long,
    val parentId: Long,
    val presence: Long,
    val leaseScopeKind: Long? = null,
    val leaseScopeId: Long? = null,
    val boundToScope: Boolean? = null,
)

/** Reads the fixed causal fields from a closed test JHLOG without interpreting optional tails. */
internal object CausalJhlogTestReader {
    fun read(directory: File): List<RecordedCausalEvent> {
        val files = directory.walkTopDown().filter { it.isFile && it.extension == "jhlog" }.toList()
        check(files.isNotEmpty()) { "No JHLOG files in $directory" }
        val types = HashMap<Long, Int>()
        val events = files.flatMap { file -> readFile(file.readBytes(), types) }
        check(events.isNotEmpty()) {
            val layouts = files.map { file ->
                val bytes = file.readBytes()
                val base = if (bytes.hasPrefix(ProcessRecordingSession.MAGIC)) ProcessRecordingSession.MAGIC.size else 0
                val headerLength = le32(bytes, base + Jhlog.FILE_MAGIC.size)
                val chunkOffset = base + Jhlog.FILE_MAGIC.size + 8 + headerLength.toInt()
                val marker = bytes.copyOfRange(chunkOffset.coerceAtMost(bytes.size),
                    (chunkOffset + 4).coerceAtMost(bytes.size)).toList()
                "${file.name}:size=${bytes.size},prefix=${bytes.take(32).map { it.toInt() and 0xff }}," +
                    "header=$headerLength,chunk=$chunkOffset,marker=$marker"
            }
            "No causal events in $layouts; record types: $types"
        }
        return events
    }

    private fun readFile(bytes: ByteArray, types: MutableMap<Long, Int>): List<RecordedCausalEvent> {
        // The public JankHunter singleton uses the process recording container before its JHLOG segment.
        val base = if (bytes.hasPrefix(ProcessRecordingSession.MAGIC)) ProcessRecordingSession.MAGIC.size else 0
        check(bytes.copyOfRange(base, base + Jhlog.FILE_MAGIC.size).contentEquals(Jhlog.FILE_MAGIC))
        var chunk = base + Jhlog.FILE_MAGIC.size + 8 + le32(bytes, base + Jhlog.FILE_MAGIC.size).toInt()
        val events = ArrayList<RecordedCausalEvent>()
        while (chunk + Jhlog.CHUNK_HEADER_BYTES <= bytes.size && bytes.hasAscii(chunk, "JHC1")) {
            val flags = le16(bytes, chunk + 6)
            val storedLength = le32(bytes, chunk + 12).toInt()
            val payloadStart = chunk + Jhlog.CHUNK_HEADER_BYTES
            val trailer = payloadStart + storedLength
            check(trailer + Jhlog.COMMIT_TRAILER_BYTES <= bytes.size && bytes.hasAscii(trailer, "JHCM"))
            val stored = bytes.copyOfRange(payloadStart, trailer)
            val raw = if (flags and Jhlog.CHUNK_FLAG_GZIP != 0) {
                GZIPInputStream(ByteArrayInputStream(stored)).use { it.readBytes() }
            } else stored
            events += readChunk(raw, types)
            chunk = trailer + Jhlog.COMMIT_TRAILER_BYTES
        }
        return events
    }

    private fun readChunk(raw: ByteArray, types: MutableMap<Long, Int>): List<RecordedCausalEvent> {
        val events = ArrayList<RecordedCausalEvent>()
        var offset = 0
        while (offset < raw.size) {
            val (length, bodyStart) = uvarint(raw, offset)
            val bodyEnd = bodyStart + length.toInt()
            check(bodyEnd <= raw.size)
            var cursor = bodyStart
            val (type, afterType) = uvarint(raw, cursor)
            types[type] = (types[type] ?: 0) + 1
            cursor = afterType
            val (flags, afterFlags) = uvarint(raw, cursor)
            cursor = afterFlags
            if (flags and Jhlog.ENVELOPE_HAS_TIME != 0L) cursor = uvarint(raw, cursor).second
            if (flags and Jhlog.ENVELOPE_HAS_THREAD != 0L) cursor = uvarint(raw, cursor).second
            if (flags and Jhlog.ENVELOPE_HAS_CONTEXT != 0L && flags and Jhlog.ENVELOPE_SAME_CONTEXT == 0L) {
                val (contextPresence, afterPresence) = uvarint(raw, cursor)
                cursor = afterPresence
                if (contextPresence and Jhlog.CONTEXT_SCREEN != 0L) cursor = skipSymbol(raw, cursor)
                if (contextPresence and Jhlog.CONTEXT_OWNER != 0L) cursor = skipSymbol(raw, cursor)
                if (contextPresence and Jhlog.CONTEXT_OPERATION != 0L) cursor = uvarint(raw, cursor).second
            }
            if (flags and Jhlog.ENVELOPE_HAS_ATTRIBUTES != 0L) cursor = uvarint(raw, cursor).second
            if (type == Jhlog.TYPE_CAUSAL_EVENT.toLong()) {
                val fixed = LongArray(13)
                for (index in fixed.indices) {
                    val (value, next) = uvarint(raw, cursor)
                    fixed[index] = value
                    cursor = next
                }
                check(fixed[0] == 1L)
                val presence = fixed[12]
                for (bit in 0..17) {
                    if (presence and (1L shl bit) != 0L) cursor = skipOptionalField(raw, cursor, bit)
                }
                val scope = if (presence and (1L shl 18) != 0L) {
                    val (kind, afterKind) = uvarint(raw, cursor)
                    val (id, afterId) = uvarint(raw, afterKind)
                    val (bound, _) = uvarint(raw, afterId)
                    Triple(kind, id, bound == 1L)
                } else null
                events += RecordedCausalEvent(fixed[1], fixed[2], fixed[4], fixed[5], presence,
                    scope?.first, scope?.second, scope?.third)
            }
            offset = bodyEnd
        }
        return events
    }

    private fun skipOptionalField(bytes: ByteArray, offset: Int, bit: Int): Int = when (bit) {
        2, 3 -> {
            val afterSite = uvarint(bytes, offset).second
            val (length, afterLength) = uvarint(bytes, afterSite)
            uvarint(bytes, afterLength + length.toInt()).second
        }
        11 -> uvarint(bytes, uvarint(bytes, uvarint(bytes, offset).second).second).second
        17 -> uvarint(bytes, uvarint(bytes, offset).second).second
        else -> uvarint(bytes, offset).second
    }

    private fun skipSymbol(bytes: ByteArray, offset: Int): Int {
        val (token, next) = uvarint(bytes, offset)
        return if (token == 1L) next + Long.SIZE_BYTES else next
    }

    private fun uvarint(bytes: ByteArray, offset: Int): Pair<Long, Int> {
        var cursor = offset
        var shift = 0
        var value = 0L
        while (cursor < bytes.size && shift < Long.SIZE_BITS) {
            val byte = bytes[cursor++].toInt() and 0xff
            value = value or ((byte and 0x7f).toLong() shl shift)
            if (byte and 0x80 == 0) return value to cursor
            shift += 7
        }
        error("Truncated JHLOG varint")
    }

    private fun le16(bytes: ByteArray, offset: Int): Int =
        (bytes[offset].toInt() and 0xff) or ((bytes[offset + 1].toInt() and 0xff) shl 8)

    private fun le32(bytes: ByteArray, offset: Int): Long {
        var value = 0L
        repeat(4) { index -> value = value or ((bytes[offset + index].toLong() and 0xffL) shl (index * 8)) }
        return value
    }

    private fun ByteArray.hasAscii(offset: Int, value: String): Boolean =
        offset >= 0 && offset + value.length <= size && value.indices.all { this[offset + it].toInt() == value[it].code }

    private fun ByteArray.hasPrefix(prefix: ByteArray): Boolean =
        size >= prefix.size && prefix.indices.all { this[it] == prefix[it] }
}
