package io.jankhunter.plugin.execution

import java.io.File
import java.time.LocalDate
import java.time.format.DateTimeFormatter

internal object JankHunterSessionLogFiles {
    private val canonicalName = Regex("^jh-session-log\\.(\\d{4}-\\d{2}-\\d{2})\\.(\\d+)\\.jhlog$")

    fun latest(files: Iterable<File>): File? {
        val candidates = files.toList()
        if (candidates.isEmpty()) return null

        val canonical = candidates.mapNotNull { file ->
            parse(file)?.let { key -> Candidate(file, key) }
        }
        return canonical.maxWithOrNull(
            compareBy<Candidate> { it.key.date }
                .thenBy { it.key.index }
                .thenBy { it.file.path },
        )?.file ?: candidates.maxWithOrNull(
            compareBy<File> { it.lastModified() }
                .thenBy { it.path },
        )
    }

    private fun parse(file: File): SessionLogKey? {
        val match = canonicalName.matchEntire(file.name) ?: return null
        val date = runCatching {
            LocalDate.parse(match.groupValues[1], DateTimeFormatter.ISO_LOCAL_DATE)
        }.getOrNull() ?: return null
        val indexText = match.groupValues[2]
        val index = indexText.toLongOrNull() ?: return null
        if (index.toString() != indexText) return null
        return SessionLogKey(date, index)
    }

    private data class Candidate(
        val file: File,
        val key: SessionLogKey,
    )

    private data class SessionLogKey(
        val date: LocalDate,
        val index: Long,
    )
}
