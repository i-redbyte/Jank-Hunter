package io.jankhunter.plugin.services

import com.intellij.execution.ExecutionException
import com.intellij.execution.configurations.GeneralCommandLine
import com.intellij.execution.process.OSProcessHandler
import com.intellij.openapi.project.Project
import io.jankhunter.plugin.execution.JankHunterCommand
import java.io.File
import java.nio.charset.StandardCharsets

object JankHunterProcessRunner {
    @Throws(ExecutionException::class)
    fun start(project: Project, command: JankHunterCommand): OSProcessHandler {
        command.outputPath?.let { File(it).parentFile?.mkdirs() }
        val commandLine = GeneralCommandLine(command.executable)
            .withParameters(command.args)
            .withCharset(StandardCharsets.UTF_8)
        project.basePath?.let { commandLine.withWorkDirectory(File(it)) }
        return OSProcessHandler(commandLine).also { it.startNotify() }
    }

    fun commandLine(project: Project, command: JankHunterCommand): GeneralCommandLine {
        command.outputPath?.let { File(it).parentFile?.mkdirs() }
        val commandLine = GeneralCommandLine(command.executable)
            .withParameters(command.args)
            .withCharset(StandardCharsets.UTF_8)
        project.basePath?.let { commandLine.withWorkDirectory(File(it)) }
        return commandLine
    }
}
