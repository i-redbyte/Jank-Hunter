package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterBinaryStorage
import io.jankhunter.runtime.JankHunterBinaryArtifact
import java.io.File
import java.io.FileInputStream
import java.io.FileOutputStream
import java.io.IOException
import java.security.MessageDigest

internal object SealedSegmentImporter {
    private const val COPY_BUFFER_BYTES = 32 * 1024

    fun import(sourcePath: String, target: JankHunterBinaryStorage): ImportResult {
        val source = File(sourcePath)
        if (!source.isFile) throw IOException("Jank Hunter source segment is missing: $sourcePath")
        val sourceBytes = source.length()
        if (sourceBytes > target.fileSizeLimitBytes) {
            throw IOException(
                "Jank Hunter source segment exceeds target file limit: " +
                    "size=$sourceBytes limit=${target.fileSizeLimitBytes}",
            )
        }
        val fileName = source.name
        findExisting(target, fileName)?.let { existing ->
            if (!sameContent(source, existing)) {
                throw IOException("Jank Hunter target segment collision: ${existing.absolutePath}")
            }
            return ImportResult(existing.absolutePath, created = false, target.protect(fileName))
        }

        val artifact = target.createArtifact(fileName)
        val destination = File(artifact.path)
        val parent = destination.parentFile
            ?: throw IOException("Jank Hunter target segment has no parent: ${destination.absolutePath}")
        if (!parent.isDirectory && !parent.mkdirs()) {
            throw IOException("Cannot create Jank Hunter target directory: ${parent.absolutePath}")
        }
        val temporary = File.createTempFile(".${destination.name}.", ".handoff", parent)
        var published = false
        var created = false
        var protection: JankHunterBinaryArtifact? = null
        try {
            copyAndSync(source, temporary)
            if (!sameContent(source, temporary)) throw IOException("Jank Hunter segment copy verification failed")
            if (destination.exists()) {
                if (!sameContent(source, destination)) {
                    throw IOException("Jank Hunter target segment collision: ${destination.absolutePath}")
                }
                temporary.delete()
            } else if (!temporary.renameTo(destination)) {
                throw IOException("Cannot atomically publish Jank Hunter segment ${destination.absolutePath}")
            } else {
                created = true
            }
            if (!sameContent(source, destination)) throw IOException("Jank Hunter published segment verification failed")
            protection = target.protect(fileName)
            artifact.commit()
            published = true
            return ImportResult(destination.absolutePath, created, checkNotNull(protection))
        } finally {
            temporary.delete()
            if (!published) {
                runCatching { protection?.abort() }
                runCatching { artifact.abort() }
            }
        }
    }

    fun import(sourcePath: String, targetDirectory: File): ImportResult {
        val source = File(sourcePath)
        if (!source.isFile) throw IOException("Jank Hunter source segment is missing: $sourcePath")
        if (!targetDirectory.isDirectory && !targetDirectory.mkdirs()) {
            throw IOException("Cannot create Jank Hunter target directory: ${targetDirectory.absolutePath}")
        }
        val destination = File(targetDirectory, source.name)
        if (destination.isFile) {
            if (!sameContent(source, destination)) {
                throw IOException("Jank Hunter target segment collision: ${destination.absolutePath}")
            }
            return ImportResult(destination.absolutePath, created = false, protection = null)
        }
        val temporary = File.createTempFile(".${destination.name}.", ".handoff", targetDirectory)
        var published = false
        var created = false
        try {
            copyAndSync(source, temporary)
            if (!sameContent(source, temporary)) throw IOException("Jank Hunter segment copy verification failed")
            if (destination.exists()) {
                if (!sameContent(source, destination)) {
                    throw IOException("Jank Hunter target segment collision: ${destination.absolutePath}")
                }
                temporary.delete()
            } else if (!temporary.renameTo(destination)) {
                throw IOException("Cannot atomically publish Jank Hunter segment ${destination.absolutePath}")
            } else {
                created = true
            }
            if (!sameContent(source, destination)) throw IOException("Jank Hunter published segment verification failed")
            published = true
            return ImportResult(destination.absolutePath, created, protection = null)
        } finally {
            temporary.delete()
            if (!published && created) destination.delete()
        }
    }

    private fun findExisting(storage: JankHunterBinaryStorage, fileName: String): File? {
        return storage.listFiles().firstNotNullOfOrNull { path ->
            File(path).takeIf { file -> file.name == fileName && file.isFile }
        }
    }

    private fun copyAndSync(source: File, destination: File) {
        val buffer = ByteArray(COPY_BUFFER_BYTES)
        FileInputStream(source).use { input ->
            FileOutputStream(destination, false).use { output ->
                while (true) {
                    val read = input.read(buffer)
                    if (read < 0) break
                    output.write(buffer, 0, read)
                }
                output.flush()
                output.fd.sync()
            }
        }
    }

    private fun sameContent(first: File, second: File): Boolean {
        if (first.length() != second.length()) return false
        return digest(first).contentEquals(digest(second))
    }

    private fun digest(file: File): ByteArray {
        val digest = MessageDigest.getInstance("SHA-256")
        val buffer = ByteArray(COPY_BUFFER_BYTES)
        FileInputStream(file).use { input ->
            while (true) {
                val read = input.read(buffer)
                if (read < 0) break
                digest.update(buffer, 0, read)
            }
        }
        return digest.digest()
    }

    data class ImportResult(
        val path: String,
        val created: Boolean,
        val protection: JankHunterBinaryArtifact?,
    )
}
