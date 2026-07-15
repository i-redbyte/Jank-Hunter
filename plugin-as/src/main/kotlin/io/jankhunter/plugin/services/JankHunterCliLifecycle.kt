package io.jankhunter.plugin.services

import com.intellij.execution.configurations.GeneralCommandLine
import com.intellij.openapi.project.Project
import io.jankhunter.plugin.execution.JankHunterArtifactDiscovery
import java.io.File
import java.nio.charset.StandardCharsets

data class JankHunterCliStatus(
    val cliPath: String,
    val exists: Boolean,
    val executable: Boolean,
    val versionOutput: String = "",
    val version: String = "",
    val stale: Boolean = false,
)

object JankHunterCliLifecycle {
    fun status(project: Project, configuredPath: String): JankHunterCliStatus {
        val path = configuredPath.ifBlank { JankHunterArtifactDiscovery.detectCli(project) }
        val file = if (path.contains('/')) File(path) else null
        val exists = file?.isFile ?: commandExists(path)
        val executable = file?.canExecute() ?: exists
        val versionOutput = if (exists && executable) runVersion(project, path) else ""
        val version = Regex("""Jank Hunter CLI\s+([^\s]+)""").find(versionOutput)?.groupValues?.getOrNull(1).orEmpty()
        val stale = version.isNotBlank() && compareVersions(version, PLUGIN_EXPECTED_CLI_VERSION) < 0
        return JankHunterCliStatus(path, exists, executable, versionOutput, version, stale)
    }

    fun buildCommand(project: Project): GeneralCommandLine? {
        val cliDir = project.basePath?.let { File(it, "cli") }?.takeIf { File(it, "Makefile").isFile }
            ?: project.basePath?.let { File(it, "../cli").canonicalFile }?.takeIf { File(it, "Makefile").isFile }
            ?: return null
        return GeneralCommandLine("make")
            .withParameters("build")
            .withWorkDirectory(cliDir)
            .withCharset(StandardCharsets.UTF_8)
    }

    private fun runVersion(project: Project, cliPath: String): String {
        return runCatching {
            val commandLine = GeneralCommandLine(cliPath)
                .withParameters("version")
                .withCharset(StandardCharsets.UTF_8)
            project.basePath?.let { commandLine.withWorkDirectory(File(it)) }
            JankHunterProcessCapture.run(commandLine, VERSION_TIMEOUT_MILLIS).combinedOutput()
        }.getOrDefault("")
    }

    private fun commandExists(name: String): Boolean =
        System.getenv("PATH")
            ?.split(File.pathSeparator)
            ?.any { File(it, name).canExecute() }
            ?: false

    private fun compareVersions(left: String, right: String): Int {
        val l = left.split('.', '-').mapNotNull { it.toIntOrNull() }
        val r = right.split('.', '-').mapNotNull { it.toIntOrNull() }
        for (i in 0 until maxOf(l.size, r.size)) {
            val diff = (l.getOrNull(i) ?: 0) - (r.getOrNull(i) ?: 0)
            if (diff != 0) return diff
        }
        return 0
    }

    private const val PLUGIN_EXPECTED_CLI_VERSION = "1.0.1"
    private const val VERSION_TIMEOUT_MILLIS = 5_000L
}
