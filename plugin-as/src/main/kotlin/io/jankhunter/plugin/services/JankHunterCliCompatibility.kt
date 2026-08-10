package io.jankhunter.plugin.services

import com.intellij.execution.configurations.GeneralCommandLine
import com.intellij.execution.process.CapturingProcessHandler
import com.intellij.openapi.project.Project
import com.intellij.util.text.VersionComparatorUtil
import io.jankhunter.plugin.execution.JankHunterCommand
import java.io.File
import java.nio.charset.StandardCharsets

internal object JankHunterCliCompatibility {
    fun validate(project: Project, command: JankHunterCommand): String? {
        val flags = command.args.requiredAppearanceFlags()
        if (flags.isEmpty()) return null

        val output = runCatching { readVersion(project, command.executable) }
            .getOrElse { error ->
                return "Не удалось проверить версию Jank Hunter CLI: ${error.message ?: error.javaClass.simpleName}. " +
                    "Для ${flags.joinToString()} нужна версия $MINIMUM_APPEARANCE_VERSION или новее."
            }
        return compatibilityError(command.args, output)
    }

    internal fun compatibilityError(args: List<String>, versionOutput: String): String? {
        val flags = args.requiredAppearanceFlags()
        if (flags.isEmpty()) return null
        val version = parseVersion(versionOutput)
            ?: return "CLI не сообщил распознаваемую версию. Для ${flags.joinToString()} нужна версия " +
                "$MINIMUM_APPEARANCE_VERSION или новее. Выполните `jankhunter version` и обновите CLI."
        if (VersionComparatorUtil.compare(version, MINIMUM_APPEARANCE_VERSION) < 0) {
            return "Jank Hunter CLI $version не поддерживает ${flags.joinToString()}. " +
                "Установите версию $MINIMUM_APPEARANCE_VERSION или новее."
        }
        return null
    }

    private fun parseVersion(output: String): String? = VERSION_PATTERN.find(output)?.groupValues?.get(1)

    private fun readVersion(project: Project, executable: String): String {
        val commandLine = GeneralCommandLine(executable)
            .withParameters("version")
            .withCharset(StandardCharsets.UTF_8)
        project.basePath?.let { commandLine.withWorkDirectory(File(it)) }
        val output = CapturingProcessHandler(commandLine).runProcess(VERSION_TIMEOUT_MILLIS)
        check(!output.isTimeout) { "команда version не завершилась за ${VERSION_TIMEOUT_MILLIS / 1_000} с" }
        check(output.exitCode == 0) {
            output.stderr.trim().ifEmpty { "команда version завершилась с кодом ${output.exitCode}" }
        }
        return output.stdout + output.stderr
    }

    private fun List<String>.requiredAppearanceFlags(): List<String> = APPEARANCE_FLAGS.filter { it in this }

    private val APPEARANCE_FLAGS = listOf("--report-style", "--animated-background")
    private val VERSION_PATTERN = Regex("""Jank Hunter CLI\s+(\d+(?:\.\d+){2}(?:[-+][^\s]+)?)""")
    private const val MINIMUM_APPEARANCE_VERSION = "1.0.1"
    private const val VERSION_TIMEOUT_MILLIS = 5_000
}
