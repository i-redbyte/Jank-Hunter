package io.jankhunter.plugin.run

import com.intellij.execution.ExecutionException
import com.intellij.execution.configurations.CommandLineState
import com.intellij.execution.process.ProcessHandler
import com.intellij.execution.runners.ExecutionEnvironment
import com.intellij.openapi.project.Project
import io.jankhunter.plugin.execution.JankHunterCommandBuilder
import io.jankhunter.plugin.execution.JankHunterRunRequest
import io.jankhunter.plugin.execution.JankHunterRunValidator
import io.jankhunter.plugin.services.JankHunterProcessRunner

class JankHunterRunProfileState(
    private val project: Project,
    environment: ExecutionEnvironment,
    private val request: JankHunterRunRequest,
) : CommandLineState(environment) {
    @Throws(ExecutionException::class)
    override fun startProcess(): ProcessHandler {
        val command = try {
            JankHunterCommandBuilder.build(project, request)
        } catch (error: IllegalArgumentException) {
            throw ExecutionException(error.message, error)
        }
        val validation = JankHunterRunValidator.validate(project, request.copy(output = command.outputPath.orEmpty()), command)
        if (!validation.ok) {
            throw ExecutionException(validation.errors.joinToString("\n"))
        }
        return JankHunterProcessRunner.start(project, command)
    }
}
