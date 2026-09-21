package io.jankhunter.plugin.execution

import java.io.File

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
        val leftKey = JankHunterSessionLogName.parse(left)
        val rightKey = JankHunterSessionLogName.parse(right)
        when {
            leftKey != null && rightKey != null -> compareValuesBy(
                rightKey,
                leftKey,
                JankHunterSessionLogName::date,
                JankHunterSessionLogName::index,
            )
            leftKey != null -> -1
            rightKey != null -> 1
            else -> compareValuesBy(right, left, File::lastModified, File::getPath)
        }
    }

    private val SUPPORTED_EXTENSIONS = setOf("jhlog", "hprof")
    private const val MAX_SCAN_DEPTH = 6
    private const val MAX_SCANNED_FILES = 5_000
    private const val MAX_SOURCE_NAME_LENGTH = 80
}
