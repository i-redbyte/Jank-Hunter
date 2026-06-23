package io.jankhunter.plugin.execution

import com.intellij.openapi.project.Project
import java.io.File
import java.nio.file.FileSystems
import java.nio.file.Files
import java.nio.file.Path
import kotlin.io.path.isRegularFile

object JankHunterInputPaths {
    fun pathList(raw: String): List<String> =
        raw.lines()
            .flatMap { it.split(',') }
            .map { it.trim() }
            .filter { it.isNotEmpty() }

    fun resolvePath(project: Project, raw: String): Path {
        val path = Path.of(raw.trim())
        return if (path.isAbsolute) {
            path.normalize()
        } else {
            Path.of(project.basePath ?: ".").resolve(path).normalize()
        }
    }

    fun expandExistingFiles(project: Project, raw: String): List<Path> =
        pathList(raw).flatMap { part -> expandOne(project, part) }

    fun expandOne(project: Project, raw: String): List<Path> {
        val normalized = raw.trim()
        if (normalized.isBlank()) return emptyList()
        if (!containsGlob(normalized)) {
            val path = resolvePath(project, normalized)
            return if (path.isRegularFile()) listOf(path) else emptyList()
        }

        val root = globWalkRoot(project, normalized)
        if (!Files.isDirectory(root)) return emptyList()

        val absolutePattern = resolvePath(project, normalized).toString()
        val matcher = FileSystems.getDefault().getPathMatcher("glob:$absolutePattern")
        return Files.walk(root, 16).use { stream ->
            stream
                .filter { path -> path.isRegularFile() && matcher.matches(path.normalize()) }
                .sorted()
                .toList()
        }
    }

    fun containsGlob(value: String): Boolean = value.any { it == '*' || it == '?' || it == '[' || it == '{' }

    private fun globWalkRoot(project: Project, raw: String): Path {
        val wildcard = raw.indexOfFirst { it == '*' || it == '?' || it == '[' || it == '{' }
        val prefix = if (wildcard < 0) raw else raw.substring(0, wildcard)
        val slash = maxOf(prefix.lastIndexOf('/'), prefix.lastIndexOf(File.separatorChar))
        val base = when {
            slash >= 0 -> prefix.substring(0, slash + 1)
            else -> ""
        }
        return resolvePath(project, base.ifBlank { "." })
    }
}
