package io.jankhunter.gradle

import java.io.File
import java.io.Writer
import java.nio.file.AtomicMoveNotSupportedException
import java.nio.file.Files
import java.nio.file.StandardCopyOption
import java.util.Locale

internal object InstrumentationArtifactFiles {
    fun writeClassShard(directoryPath: String, className: String, text: String) {
        val shard = classShard(directoryPath, className) ?: return
        if (text.isBlank()) {
            Files.deleteIfExists(shard.toPath())
            return
        }
        writeTextAtomically(shard, text)
    }

    fun writeClassShard(directoryPath: String, className: String, write: (Writer) -> Unit) {
        val shard = classShard(directoryPath, className) ?: return
        writeAtomically(shard, write)
    }

    fun writeAtomically(file: File, text: String) {
        writeAtomically(file) { writer -> writer.write(text) }
    }

    private fun writeTextAtomically(target: File, text: String) {
        writeAtomically(target) { writer -> writer.write(text) }
    }

    private fun writeAtomically(target: File, write: (Writer) -> Unit) {
        val parent = requireNotNull(target.absoluteFile.parentFile) { "Artifact target must have a parent: $target" }
        Files.createDirectories(parent.toPath())
        val tmp = Files.createTempFile(parent.toPath(), ".${target.name}.", ".tmp").toFile()
        try {
            tmp.bufferedWriter().use(write)
            replaceAtomically(tmp, target)
        } catch (error: Throwable) {
            try {
                Files.deleteIfExists(tmp.toPath())
            } catch (cleanupError: Exception) {
                error.addSuppressed(cleanupError)
            }
            throw error
        }
    }

    private fun replaceAtomically(tmp: File, target: File) {
        try {
            Files.move(
                tmp.toPath(),
                target.toPath(),
                StandardCopyOption.REPLACE_EXISTING,
                StandardCopyOption.ATOMIC_MOVE,
            )
        } catch (_: AtomicMoveNotSupportedException) {
            replaceNonAtomically(tmp, target)
        } catch (_: UnsupportedOperationException) {
            replaceNonAtomically(tmp, target)
        }
    }

    private fun replaceNonAtomically(tmp: File, target: File) {
        Files.move(tmp.toPath(), target.toPath(), StandardCopyOption.REPLACE_EXISTING)
    }

    private fun classShard(directoryPath: String, className: String): File? {
        if (directoryPath.isBlank()) return null
        val directory = File(directoryPath)
        Files.createDirectories(directory.toPath())
        return File(directory, shardName(className))
    }

    fun readJsonlLines(directory: File): List<String> {
        if (!directory.isDirectory) return emptyList()
        return directory
            .walkTopDown()
            .filter { it.isFile && it.extension == "jsonl" }
            .sortedBy { it.relativeTo(directory).invariantSeparatorsPath }
            .flatMap { file ->
                file.readLines()
                    .map(String::trim)
                    .filter(String::isNotEmpty)
            }
            .toList()
    }

    fun mergeJsonl(directory: File?, outputFile: File) {
        writeAtomically(outputFile) { writer ->
            if (directory?.isDirectory != true) return@writeAtomically
            directory
                .walkTopDown()
                .filter { it.isFile && it.extension == "jsonl" }
                .sortedBy { it.relativeTo(directory).invariantSeparatorsPath }
                .forEach { file ->
                    file.useLines { lines ->
                        lines.forEach { line ->
                            val trimmed = line.trim()
                            if (trimmed.isNotEmpty()) {
                                writer.write(trimmed)
                                writer.write("\n")
                            }
                        }
                    }
                }
        }
    }

    private fun shardName(className: String): String {
        val normalized = className.replace('/', '.')
        val safe = normalized
            .map { char ->
                when {
                    char.isLetterOrDigit() || char == '.' || char == '_' || char == '-' -> char
                    else -> '_'
                }
            }
            .joinToString("")
            .trim('.')
            .ifBlank { "class" }
            .takeLast(MAX_SAFE_NAME_CHARS)
        return "${fnv1a64(normalized)}-$safe.jsonl"
    }

    private fun fnv1a64(value: String): String {
        var hash = FNV_OFFSET_BASIS
        value.toByteArray(Charsets.UTF_8).forEach { byte ->
            hash = hash xor (byte.toLong() and 0xffL)
            hash *= FNV_PRIME
        }
        return hash.toULong().toString(radix = 16).lowercase(Locale.US).padStart(16, '0')
    }

    private const val MAX_SAFE_NAME_CHARS = 120
    private const val FNV_OFFSET_BASIS = -3750763034362895579L
    private const val FNV_PRIME = 1099511628211L
}
