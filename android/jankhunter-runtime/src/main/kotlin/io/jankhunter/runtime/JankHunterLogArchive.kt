package io.jankhunter.runtime

import java.io.File
import java.io.IOException
import java.util.zip.Deflater
import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream

/** One portable archive containing a consistent snapshot of every live Jank Hunter process. */
class JankHunterLogArchive internal constructor(
    val capturedAtMs: Long,
    val archivePath: String,
    val archiveBytes: Long,
    val logCount: Int,
    val processCount: Int,
    val captureSkewMs: Long,
)

internal object JankHunterLogArchiveWriter {
    fun write(destination: File, snapshot: JankHunterLogSnapshot): JankHunterLogArchive {
        require(snapshot.logPaths.isNotEmpty()) { "Jank Hunter snapshot has no log files" }
        val target = destination.absoluteFile
        require(!target.exists()) { "Jank Hunter archive already exists: $target" }
        val parent = target.parentFile ?: throw IOException("Jank Hunter archive has no parent directory")
        if (!parent.isDirectory && !parent.mkdirs()) {
            throw IOException("Cannot create Jank Hunter archive directory: $parent")
        }

        val sources = canonicalSources(snapshot.logPaths, target)
        val temporary = File.createTempFile(".jh-archive-", ".tmp", parent)
        var published = false
        try {
            ZipOutputStream(temporary.outputStream().buffered(COPY_BUFFER_BYTES)).use { output ->
                // JHLOG chunks are already compressed. Recompressing them burns device CPU for
                // negligible gain, while a standard ZIP still provides a single shareable file.
                output.setLevel(Deflater.NO_COMPRESSION)
                sources.forEach { source ->
                    output.putNextEntry(ZipEntry(source.name).apply { time = 0L })
                    source.inputStream().buffered(COPY_BUFFER_BYTES).use { input ->
                        input.copyTo(output, COPY_BUFFER_BYTES)
                    }
                    output.closeEntry()
                }
            }
            if (!temporary.renameTo(target)) throw IOException("Cannot publish Jank Hunter archive: $target")
            published = true
            return JankHunterLogArchive(
                capturedAtMs = snapshot.capturedAtMs,
                archivePath = target.absolutePath,
                archiveBytes = target.length().coerceAtLeast(0L),
                logCount = sources.size,
                processCount = snapshot.processCount,
                captureSkewMs = snapshot.captureSkewMs,
            )
        } finally {
            if (!published) temporary.delete()
        }
    }

    private fun canonicalSources(paths: List<String>, target: File): List<File> {
        val targetPath = target.canonicalPath
        val names = HashSet<String>(paths.size)
        return paths.asSequence()
            .map(::File)
            .distinctBy { source -> source.canonicalPath }
            .sortedBy { source -> source.name }
            .map { source ->
                if (!source.isFile) throw IOException("Jank Hunter snapshot file is missing: $source")
                if (source.canonicalPath == targetPath) {
                    throw IOException("Jank Hunter archive cannot replace a snapshot file: $source")
                }
                if (!names.add(source.name)) {
                    throw IOException("Duplicate Jank Hunter snapshot file name: ${source.name}")
                }
                source
            }
            .toList()
    }

    private const val COPY_BUFFER_BYTES = 64 * 1024
}
