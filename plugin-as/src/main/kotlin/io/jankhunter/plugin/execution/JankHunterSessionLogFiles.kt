package io.jankhunter.plugin.execution

import java.io.File

internal object JankHunterSessionLogFiles {
    fun latestRun(files: Iterable<File>): List<File> {
        val candidates = ArrayList<Candidate>()
        var latest: JankHunterSessionLogName? = null
        val latestRunIds = LinkedHashSet<String>()
        files.forEach { file ->
            val key = JankHunterSessionLogName.parse(file) ?: return@forEach
            candidates += Candidate(file, key)
            val order = latest?.let { current -> compareKeys(key, current) } ?: 1
            when {
                order > 0 -> {
                    latest = key
                    latestRunIds.clear()
                    latestRunIds += key.runId
                }
                order == 0 -> latestRunIds += key.runId
            }
        }
        if (latest == null) return emptyList()
        return candidates.asSequence()
            .filter { candidate -> candidate.key.runId in latestRunIds }
            .sortedWith(compareBy<Candidate> { it.key.date }.thenBy { it.key.index }.thenBy { it.file.path })
            .map(Candidate::file)
            .toList()
    }

    private fun compareKeys(left: JankHunterSessionLogName, right: JankHunterSessionLogName): Int {
        val date = left.date.compareTo(right.date)
        return if (date != 0) date else left.index.compareTo(right.index)
    }

    private data class Candidate(
        val file: File,
        val key: JankHunterSessionLogName,
    )
}
