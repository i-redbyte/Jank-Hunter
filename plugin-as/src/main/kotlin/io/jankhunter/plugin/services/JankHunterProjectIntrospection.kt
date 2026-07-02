package io.jankhunter.plugin.services

import com.intellij.openapi.project.Project
import java.io.File

data class JankHunterTargetProject(
    val root: File,
    val moduleName: String,
    val buildFile: File?,
    val applicationId: String,
    val namespace: String,
) {
    val packageName: String
        get() = applicationId.ifBlank { namespace }
}

object JankHunterProjectIntrospection {
    private val skippedDirectories = setOf(
        ".git",
        ".gradle",
        ".idea",
        ".jankhunter",
        ".jankhunter-backups",
        "build",
        "node_modules",
        "tmp",
    )

    fun detect(project: Project): JankHunterTargetProject? {
        val root = project.basePath?.let(::File)?.takeIf(File::isDirectory) ?: return null
        val buildFiles = root.walkTopDown()
            .maxDepth(5)
            .onEnter { file -> file.name !in skippedDirectories }
            .filter { file -> file.isFile && file.name in setOf("build.gradle", "build.gradle.kts") }
            .toList()

        val best = buildFiles
            .map { file -> file to score(file) }
            .filter { (_, score) -> score > 0 }
            .maxWithOrNull(compareBy<Pair<File, Int>> { it.second }.thenBy { -it.first.path.length })
            ?.first

        val text = best?.readText().orEmpty()
        val appId = findGradleString(text, "applicationId")
        val namespace = findGradleString(text, "namespace")
        val module = best?.parentFile
            ?.relativeToOrNull(root)
            ?.path
            ?.replace(File.separatorChar, ':')
            ?.takeIf { it.isNotBlank() }
            ?.let { ":$it" }
            ?: ":"

        return JankHunterTargetProject(
            root = root,
            moduleName = module,
            buildFile = best,
            applicationId = appId,
            namespace = namespace,
        )
    }

    fun defaultLogsDirectory(project: Project): File {
        val root = project.basePath?.let(::File)?.takeIf(File::isDirectory)
            ?: File(System.getProperty("user.home"))
        return File(root, "build/jankhunter/logs")
    }

    private fun score(file: File): Int {
        val text = runCatching { file.readText() }.getOrDefault("")
        var score = 0
        if (Regex("""com\.android\.application|android[._-]?application|androidApplication|android_application""").containsMatchIn(text)) {
            score += 40
        }
        if (findGradleString(text, "applicationId").isNotBlank()) score += 40
        if (findGradleString(text, "namespace").isNotBlank()) score += 15
        if (file.parentFile?.name.equals("app", ignoreCase = true)) score += 20
        if (file.path.contains("${File.separator}build${File.separator}")) score -= 100
        return score
    }

    private fun findGradleString(text: String, key: String): String =
        Regex("""(?m)\b$key\s*(?:=|\s)\s*["']([^"']+)["']""")
            .find(text)
            ?.groupValues
            ?.getOrNull(1)
            .orEmpty()

    private fun File.relativeToOrNull(base: File): File? =
        runCatching { relativeTo(base) }.getOrNull()
}
