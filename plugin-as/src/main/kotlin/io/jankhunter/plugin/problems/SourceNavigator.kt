package io.jankhunter.plugin.problems

import com.intellij.openapi.fileEditor.OpenFileDescriptor
import com.intellij.openapi.project.Project
import com.intellij.openapi.vfs.LocalFileSystem
import com.intellij.openapi.vfs.VirtualFile
import java.io.File

internal data class SourceLocation(val file: VirtualFile, val line: Int)

internal object SourceNavigator {
    private val skippedDirectories = setOf(
        ".git",
        ".gradle",
        ".idea",
        ".intellijPlatform",
        "build",
        "node_modules",
        "tmp",
    )

    /** Performs filesystem discovery and must be called away from the Swing event-dispatch thread. */
    fun find(project: Project, row: Map<String, String>): SourceLocation? {
        val className = classNameFrom(row) ?: return null
        val method = methodFrom(row)
        val source = findSourceFile(project, className) ?: return null
        val line = findLine(source, className, method)
        val virtualFile = LocalFileSystem.getInstance().refreshAndFindFileByIoFile(source) ?: return null
        return SourceLocation(virtualFile, line.coerceAtLeast(0))
    }

    /** Opens a previously resolved source location on the Swing event-dispatch thread. */
    fun open(project: Project, location: SourceLocation): Boolean {
        if (!location.file.isValid) return false
        OpenFileDescriptor(project, location.file, location.line, 0).navigate(true)
        return true
    }

    private fun classNameFrom(row: Map<String, String>): String? {
        val candidates = listOf(
            "class",
            "class_name",
            "ClassName",
            "from",
            "to",
            "holder",
        )
        return candidates
            .asSequence()
            .mapNotNull { key -> row[key]?.trim()?.takeIf(String::isNotEmpty) }
            .map(::normalizeClassCandidate)
            .firstOrNull(String::isNotEmpty)
    }

    private fun methodFrom(row: Map<String, String>): String? =
        listOf("method", "Method", "to")
            .asSequence()
            .mapNotNull { row[it]?.trim()?.takeIf(String::isNotEmpty) }
            .map { value -> value.substringBefore('(').substringAfterLast('.').trim() }
            .firstOrNull { it.isNotEmpty() && it.first().isJavaIdentifierStart() }

    private fun normalizeClassCandidate(raw: String): String {
        val cleaned = raw
            .substringBefore(" -> ")
            .substringBefore('|')
            .substringBefore(' ')
            .trim()
            .removeSuffix(".kt")
            .removeSuffix(".java")
        if (cleaned.isBlank()) return ""
        return cleaned.substringBefore('$')
    }

    private fun findSourceFile(project: Project, className: String): File? {
        val root = project.basePath?.let(::File)?.takeIf(File::isDirectory) ?: return null
        val simpleName = className.substringAfterLast('.').substringBefore('$')
        val expectedNames = setOf("$simpleName.kt", "$simpleName.java")

        val nameMatches = root.walkTopDown()
            .maxDepth(MAX_SOURCE_SEARCH_DEPTH)
            .onEnter { file -> !Thread.currentThread().isInterrupted && file.name !in skippedDirectories }
            .filter { it.isFile && it.name in expectedNames }
            .take(MAX_SOURCE_CANDIDATES)
            .toList()
        if (nameMatches.size == 1) return nameMatches.first()

        return nameMatches.firstOrNull { file ->
            val text = runCatching { file.readText() }.getOrDefault("")
            text.contains("class $simpleName") ||
                text.contains("object $simpleName") ||
                text.contains("interface $simpleName") ||
                text.contains("enum class $simpleName")
        } ?: nameMatches.firstOrNull()
    }

    private fun findLine(file: File, className: String, method: String?): Int {
        val simpleName = className.substringAfterLast('.').substringBefore('$')
        val lines = runCatching { file.readLines() }.getOrDefault(emptyList())
        if (!method.isNullOrBlank()) {
            val methodRegex = Regex("""\b(fun\s+)?${Regex.escape(method)}\s*[\(<]""")
            val methodLine = lines.indexOfFirst { line -> methodRegex.containsMatchIn(line) }
            if (methodLine >= 0) return methodLine
        }

        val classRegex = Regex("""\b(class|object|interface)\s+${Regex.escape(simpleName)}\b|enum\s+class\s+${Regex.escape(simpleName)}\b""")
        val classLine = lines.indexOfFirst { line -> classRegex.containsMatchIn(line) }
        return classLine.takeIf { it >= 0 } ?: 0
    }

    private const val MAX_SOURCE_SEARCH_DEPTH = 12
    private const val MAX_SOURCE_CANDIDATES = 100
}
