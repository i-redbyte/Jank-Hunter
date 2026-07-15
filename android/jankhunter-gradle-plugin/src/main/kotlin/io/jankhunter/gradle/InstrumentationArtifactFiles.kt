package io.jankhunter.gradle

import java.io.File
import java.nio.file.AtomicMoveNotSupportedException
import java.nio.file.Files
import java.nio.file.StandardCopyOption
import java.util.Locale

internal object InstrumentationArtifactFiles {
    fun writeClassShard(directoryPath: String, className: String, text: String) {
        if (directoryPath.isBlank()) return
        val directory = File(directoryPath)
        directory.mkdirs()
        val shard = File(directory, shardName(className))
        if (text.isBlank()) {
            shard.delete()
            return
        }
        val tmp = File(shard.parentFile, "${shard.name}.${System.nanoTime()}.tmp")
        tmp.writeText(text)
        replaceAtomically(tmp, shard)
    }

    fun writeAtomically(file: File, text: String) {
        file.parentFile?.mkdirs()
        val tmp = File(file.parentFile, "${file.name}.${System.nanoTime()}.tmp")
        tmp.writeText(text)
        replaceAtomically(tmp, file)
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
        } catch (error: Exception) {
            tmp.delete()
            throw error
        }
    }

    private fun replaceNonAtomically(tmp: File, target: File) {
        try {
            Files.move(tmp.toPath(), target.toPath(), StandardCopyOption.REPLACE_EXISTING)
        } catch (error: Exception) {
            tmp.delete()
            throw error
        }
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
        outputFile.parentFile?.mkdirs()
        val lines = directory?.let(::readJsonlLines).orEmpty()
        outputFile.writeText(lines.joinToString(separator = "\n", postfix = if (lines.isEmpty()) "" else "\n"))
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
