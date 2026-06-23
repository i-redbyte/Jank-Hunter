package io.jankhunter.plugin.services

import com.intellij.execution.ExecutionException
import com.intellij.execution.configurations.GeneralCommandLine
import com.intellij.execution.process.OSProcessHandler
import com.intellij.execution.process.ProcessEvent
import com.intellij.execution.process.ProcessListener
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
    fun localCliFile(project: Project): File? {
        val base = project.basePath ?: return null
        return listOf(
            File(base, "cli/bin/jankhunter"),
            File(base, "../cli/bin/jankhunter").canonicalFile,
            File(base, "../../cli/bin/jankhunter").canonicalFile,
        ).firstOrNull { it.parentFile?.isDirectory == true }
    }

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

    fun buildCli(project: Project, onText: (String) -> Unit, onDone: (Boolean) -> Unit) {
        val cliDir = project.basePath?.let { File(it, "cli") }?.takeIf { File(it, "Makefile").isFile }
            ?: project.basePath?.let { File(it, "../cli").canonicalFile }?.takeIf { File(it, "Makefile").isFile }
        if (cliDir == null) {
            onText("Не нашел cli/Makefile рядом с проектом.\n")
            onDone(false)
            return
        }
        val commandLine = GeneralCommandLine("make")
            .withParameters("build")
            .withWorkDirectory(cliDir)
            .withCharset(StandardCharsets.UTF_8)
        try {
            val handler = OSProcessHandler(commandLine)
            handler.addProcessListener(
                object : ProcessListener {
                    override fun onTextAvailable(event: ProcessEvent, outputType: com.intellij.openapi.util.Key<*>) {
                        onText(event.text)
                    }

                    override fun processTerminated(event: ProcessEvent) {
                        onDone(event.exitCode == 0)
                    }
                },
            )
            handler.startNotify()
        } catch (error: ExecutionException) {
            onText("Не удалось запустить make build: ${error.message}\n")
            onDone(false)
        }
    }

    private fun runVersion(project: Project, cliPath: String): String {
        return runCatching {
            val commandLine = GeneralCommandLine(cliPath)
                .withParameters("version")
                .withCharset(StandardCharsets.UTF_8)
            project.basePath?.let { commandLine.withWorkDirectory(File(it)) }
            val process = commandLine.createProcess()
            val out = process.inputStream.bufferedReader().readText()
            val err = process.errorStream.bufferedReader().readText()
            process.waitFor()
            out + err
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

    private const val PLUGIN_EXPECTED_CLI_VERSION = "1.0.0"
}
