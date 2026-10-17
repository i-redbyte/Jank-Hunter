package io.jankhunter.plugin.services

import com.intellij.execution.ExecutionException
import com.intellij.execution.configurations.GeneralCommandLine
import com.intellij.execution.process.OSProcessHandler
import com.intellij.execution.process.ProcessHandler
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.project.Project
import io.jankhunter.plugin.execution.JankHunterCommand
import java.io.File
import java.nio.charset.StandardCharsets

object JankHunterProcessRunner {
    internal fun discardUnstarted(handler: ProcessHandler) {
        // ProcessHandler defers destruction until startNotify; an abandoned launch has no UI owner.
        handler.startNotify()
        handler.destroyProcess()
    }

    @Throws(ExecutionException::class)
    fun start(project: Project, command: JankHunterCommand): OSProcessHandler =
        create(project, command).also { it.startNotify() }

    @Throws(ExecutionException::class)
    fun create(project: Project, command: JankHunterCommand): OSProcessHandler {
        check(!ApplicationManager.getApplication().isDispatchThread) {
            "Jank Hunter CLI must be started away from the Swing event-dispatch thread"
        }
        val parent = command.outputPath?.let(::File)?.parentFile
        if (parent != null && !parent.isDirectory && !parent.mkdirs() && !parent.isDirectory) {
            throw ExecutionException("Не удалось создать папку отчёта: ${parent.path}")
        }
        val commandLine = GeneralCommandLine(command.executable)
            .withParameters(command.args)
            .withCharset(StandardCharsets.UTF_8)
        project.basePath?.let { commandLine.withWorkDirectory(File(it)) }
        return OSProcessHandler(commandLine)
    }
}
