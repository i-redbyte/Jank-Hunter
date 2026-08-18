package io.jankhunter.plugin.execution

import java.io.File
import java.time.LocalDate
import java.time.format.DateTimeFormatter

internal data class JankHunterSessionLogName(
    val date: LocalDate,
    val runId: String,
    val index: Long,
) {
    companion object {
        private const val PREFIX = "jh-session-log."
        private const val SUFFIX = ".jhlog"
        private const val DATE_LENGTH = 10
        private const val RUN_ID_LENGTH = 32

        fun parse(file: File): JankHunterSessionLogName? = parse(file.name)

        fun parse(fileName: String): JankHunterSessionLogName? {
            if (!fileName.startsWith(PREFIX) || !fileName.endsWith(SUFFIX)) return null
            val body = fileName.substring(PREFIX.length, fileName.length - SUFFIX.length)
            val runSeparator = DATE_LENGTH
            val indexSeparator = runSeparator + 1 + RUN_ID_LENGTH
            if (body.length <= indexSeparator + 1 || body[runSeparator] != '.' || body[indexSeparator] != '.') {
                return null
            }
            val dateText = body.substring(0, runSeparator)
            val date = runCatching { LocalDate.parse(dateText, DateTimeFormatter.ISO_LOCAL_DATE) }.getOrNull()
                ?: return null
            val runId = body.substring(runSeparator + 1, indexSeparator)
            if (!runId.all { char -> char in '0'..'9' || char in 'a'..'f' } || runId.all { it == '0' }) return null
            val indexText = body.substring(indexSeparator + 1)
            if (indexText.length > 1 && indexText[0] == '0') return null
            val index = indexText.toLongOrNull()?.takeIf { value -> value >= 0L } ?: return null
            return JankHunterSessionLogName(date, runId, index)
        }
    }
}
