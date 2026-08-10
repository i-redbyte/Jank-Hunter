package io.jankhunter.plugin.services

import com.intellij.openapi.project.Project
import io.jankhunter.plugin.execution.JankHunterUserPaths
import java.io.File

object JankHunterProjectIntrospection {
    fun defaultLogsDirectory(project: Project): File {
        val root = project.basePath?.let(::File)?.takeIf(File::isDirectory)
            ?: JankHunterUserPaths.homeDirectory()
        return File(root, "build/jankhunter/logs")
    }
}
