package io.jankhunter.plugin.execution

import java.io.File
import java.time.LocalDate
import java.time.format.DateTimeFormatter

data class JankHunterDiscoveredLog(
    val file: File,
    val fingerprint: String,
    val processed: Boolean,
)

data class JankHunterLogDirectorySnapshot(
    val logs: List<JankHunterDiscoveredLog>,
    val heapCandidates: List<File>,
)

object JankHunterLogDiscovery {
    fun scan(directory: File, processedFingerprints: Set<String>): JankHunterLogDirectorySnapshot {
        if (!directory.isDirectory) return JankHunterLogDirectorySnapshot(emptyList(), emptyList())

        val files = supportedFiles(
            directory.walkTopDown()
                .maxDepth(MAX_SCAN_DEPTH)
                .onEnter { !Thread.currentThread().isInterrupted },
        )

        val logs = files
            .filter { it.extension.equals("jhlog", ignoreCase = true) }
            .sortedWith(logComparator)
            .map { file ->
                val fingerprint = fingerprint(file)
                JankHunterDiscoveredLog(file, fingerprint, fingerprint in processedFingerprints)
            }
        val heaps = files
            .filter { it.extension.equals("hprof", ignoreCase = true) }
            .sortedWith(compareByDescending<File>(File::lastModified).thenBy(File::getPath))

        return JankHunterLogDirectorySnapshot(logs, heaps)
    }

    fun fingerprint(file: File): String {
        val path = runCatching { file.canonicalFile.path }
            .getOrDefault(file.absoluteFile.toPath().normalize().toString())
        return "$path|${file.length()}|${file.lastModified()}"
    }

    fun sourceName(directory: File?): String {
        val raw = directory?.name.orEmpty().ifBlank { "logs" }
        return raw.replace(Regex("[^A-Za-z0-9._-]+"), "-")
            .trim('-')
            .ifBlank { "logs" }
            .take(MAX_SOURCE_NAME_LENGTH)
    }

    internal fun supportedFiles(files: Sequence<File>, limit: Int = MAX_SCANNED_FILES): List<File> = files
        .filter(File::isFile)
        .filter { it.extension.lowercase() in SUPPORTED_EXTENSIONS }
        .take(limit)
        .toList()

    private val logComparator = Comparator<File> { left, right ->
        val leftKey = sessionKey(left)
        val rightKey = sessionKey(right)
        when {
            leftKey != null && rightKey != null -> compareValuesBy(rightKey, leftKey, SessionKey::date, SessionKey::index)
            leftKey != null -> -1
            rightKey != null -> 1
            else -> compareValuesBy(right, left, File::lastModified, File::getPath)
        }
    }

    private fun sessionKey(file: File): SessionKey? {
        val match = CANONICAL_NAME.matchEntire(file.name) ?: return null
        val date = runCatching { LocalDate.parse(match.groupValues[1], DateTimeFormatter.ISO_LOCAL_DATE) }.getOrNull()
            ?: return null
        val indexText = match.groupValues[2]
        val index = indexText.toLongOrNull() ?: return null
        if (index.toString() != indexText) return null
        return SessionKey(date, index)
    }

    private data class SessionKey(val date: LocalDate, val index: Long)

    private val CANONICAL_NAME = Regex("^jh-session-log\\.(\\d{4}-\\d{2}-\\d{2})\\.(\\d+)\\.jhlog$")
    private val SUPPORTED_EXTENSIONS = setOf("jhlog", "hprof")
    private const val MAX_SCAN_DEPTH = 6
    private const val MAX_SCANNED_FILES = 5_000
    private const val MAX_SOURCE_NAME_LENGTH = 80
}
