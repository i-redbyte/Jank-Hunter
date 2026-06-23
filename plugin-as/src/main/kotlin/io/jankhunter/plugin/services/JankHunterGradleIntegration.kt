package io.jankhunter.plugin.services

import com.intellij.execution.ExecutionException
import com.intellij.execution.configurations.GeneralCommandLine
import com.intellij.execution.process.OSProcessHandler
import com.intellij.execution.process.ProcessEvent
import com.intellij.execution.process.ProcessListener
import com.intellij.openapi.project.Project
import java.io.File
import java.nio.charset.StandardCharsets

object JankHunterGradleIntegration {
    fun runAndroidTask(
        project: Project,
        task: String,
        onText: (String) -> Unit,
        onDone: (Boolean) -> Unit,
    ) {
        val androidDir = androidDir(project)
        if (androidDir == null) {
            onText("Не нашел android/gradlew.\n")
            onDone(false)
            return
        }
        val wrapper = File(androidDir, if (System.getProperty("os.name").startsWith("Windows")) "gradlew.bat" else "gradlew")
        val commandLine = GeneralCommandLine(wrapper.path)
            .withParameters(task)
            .withWorkDirectory(androidDir)
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
            onText("Не удалось запустить Gradle task $task: ${error.message}\n")
            onDone(false)
        }
    }

    fun sampleAssembleTask(): String = ":sample-app:assembleDebug"

    fun sampleConnectedTestTask(): String = ":sample-app:connectedDebugAndroidTest"

    fun collectArtifactsTask(): String = ":sample-app:assembleDebug"

    private fun androidDir(project: Project): File? {
        val base = project.basePath ?: return null
        return listOf(
            File(base, "android"),
            File(base, "../android").canonicalFile,
        ).firstOrNull { File(it, "gradlew").isFile || File(it, "gradlew.bat").isFile }
    }
}
