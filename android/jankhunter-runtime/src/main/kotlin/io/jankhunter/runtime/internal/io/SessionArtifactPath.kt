package io.jankhunter.runtime.internal.io

import java.io.File
import java.text.ParsePosition
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.TimeZone

/** Canonical, manifest-free filesystem paths for one application run and process instance. */
internal object SessionArtifactPath {
    private const val ID_BYTES = 16
    private const val UTC_PATTERN = "yyyy-MM-dd'T'HH-mm-ss.SSS'Z'"
    private const val TIMESTAMP_LENGTH = 24
    private const val RUN_ID_HEX_LENGTH = ID_BYTES * 2
    private const val MAX_INDEX_DIGITS = 19
    private const val MAX_CANONICAL_UNIX_MS = 253_402_300_799_999L
    private val utc = TimeZone.getTimeZone("UTC")

    fun sessionDirectoryName(
        startedAtUnixMs: Long,
        dailySessionIndex: Long,
        runId: ByteArray,
    ): String {
        require(startedAtUnixMs >= 0L) { "session start time must be non-negative" }
        require(startedAtUnixMs <= MAX_CANONICAL_UNIX_MS) { "session start time exceeds canonical range" }
        require(dailySessionIndex >= 0L) { "daily session index must be non-negative" }
        validateId(runId, "run")
        val timestamp = timestampFormatter().format(Date(startedAtUnixMs))
        return "${timestamp}_${dailySessionIndex}_${hex(runId)}"
    }

    fun processDirectoryName(processInstanceId: ByteArray): String {
        validateId(processInstanceId, "process instance")
        return hex(processInstanceId)
    }

    fun isCanonicalId(value: String): Boolean =
        value.length == RUN_ID_HEX_LENGTH && value.any { character -> character != '0' } && value.all(::isLowerHex)

    fun parseSessionDirectoryName(name: String): ParsedSession? {
        if (name.length < TIMESTAMP_LENGTH + 3 + RUN_ID_HEX_LENGTH) return null
        if (name[TIMESTAMP_LENGTH] != '_') return null
        val runSeparator = name.length - RUN_ID_HEX_LENGTH - 1
        if (runSeparator <= TIMESTAMP_LENGTH + 1 || name[runSeparator] != '_') return null
        val runId = name.substring(runSeparator + 1)
        if (!runId.all(::isLowerHex) || runId.all { value -> value == '0' }) return null
        val dailySessionIndex = parseCanonicalLong(name, TIMESTAMP_LENGTH + 1, runSeparator) ?: return null
        val timestamp = name.substring(0, TIMESTAMP_LENGTH)
        val parser = timestampFormatter().apply { isLenient = false }
        val position = ParsePosition(0)
        val startedAtUnixMs = parser.parse(timestamp, position)?.time ?: return null
        if (position.index != timestamp.length || parser.format(Date(startedAtUnixMs)) != timestamp) return null
        return ParsedSession(startedAtUnixMs, dailySessionIndex, runId)
    }

    fun scope(
        root: File,
        startedAtUnixMs: Long,
        dailySessionIndex: Long,
        runId: ByteArray,
        processInstanceId: ByteArray,
    ): Scope {
        val session = File(root, sessionDirectoryName(startedAtUnixMs, dailySessionIndex, runId))
        return Scope(session, File(session, processDirectoryName(processInstanceId)))
    }

    private fun validateId(id: ByteArray, label: String) {
        require(id.size == ID_BYTES) { "$label ID must have $ID_BYTES bytes" }
        require(id.any { value -> value != 0.toByte() }) { "$label ID must not be zero" }
    }

    private fun hex(bytes: ByteArray): String {
        val chars = CharArray(bytes.size * 2)
        for (index in bytes.indices) {
            val value = bytes[index].toInt() and 0xff
            chars[index * 2] = HEX[value ushr 4]
            chars[index * 2 + 1] = HEX[value and 0x0f]
        }
        return String(chars)
    }

    private fun timestampFormatter(): SimpleDateFormat = SimpleDateFormat(UTC_PATTERN, Locale.ROOT).apply {
        timeZone = utc
    }

    private fun parseCanonicalLong(value: String, start: Int, end: Int): Long? {
        if (start >= end || end - start > MAX_INDEX_DIGITS) return null
        if (value[start] == '0' && end - start > 1) return null
        var result = 0L
        for (index in start until end) {
            val digit = value[index] - '0'
            if (digit !in 0..9 || result > (Long.MAX_VALUE - digit) / 10L) return null
            result = result * 10L + digit
        }
        return result
    }

    private fun isLowerHex(value: Char): Boolean = value in '0'..'9' || value in 'a'..'f'

    internal class Scope(
        val sessionDirectory: File,
        val processDirectory: File,
    )

    internal data class ParsedSession(
        val startedAtUnixMs: Long,
        val dailySessionIndex: Long,
        val runId: String,
    )

    private val HEX = "0123456789abcdef".toCharArray()
}
