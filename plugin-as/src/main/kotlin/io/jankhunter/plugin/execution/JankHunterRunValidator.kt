package io.jankhunter.plugin.execution

import com.intellij.openapi.project.Project
import java.io.File
import java.nio.file.Files
import java.nio.file.Path
import kotlin.io.path.extension
import kotlin.io.path.isDirectory
import kotlin.io.path.isRegularFile

data class JankHunterValidationResult(
    val errors: List<String>,
    val warnings: List<String> = emptyList(),
) {
    val ok: Boolean get() = errors.isEmpty()
}

object JankHunterRunValidator {
    fun validate(project: Project, request: JankHunterRunRequest, command: JankHunterCommand): JankHunterValidationResult {
        val errors = mutableListOf<String>()
        val warnings = mutableListOf<String>()

        validateCli(command.executable, errors)
        validateModeInputs(project, request, errors, warnings)
        if (request.mode in analysisModes) {
            validateArtifact(project, "owner-map", request.ownerMap, errors)
            validateArtifact(project, "mapping", request.mapping, errors)
            validateArtifact(project, "class-graph", request.classGraph, errors)
            validateArtifact(project, "instrumentation-diagnostics", request.diagnostics, errors)
            validateArtifact(project, "di-catalog", request.diCatalog, errors)
        }
        when (request.mode) {
            JankHunterMode.INSPECT,
            JankHunterMode.PROBLEMS -> {
                validateArtifact(project, "heap-dump", request.heapDump, errors)
                validateArtifact(project, "heap-evidence", request.heapEvidence, errors)
            }

            JankHunterMode.COMPARE,
            JankHunterMode.SCORECARD -> {
                validateArtifact(project, "baseline-heap-dump", request.baselineHeapDump, errors)
                validateArtifact(project, "baseline-heap-evidence", request.baselineHeapEvidence, errors)
                validateArtifact(project, "candidate-heap-dump", request.candidateHeapDump, errors)
                validateArtifact(project, "candidate-heap-evidence", request.candidateHeapEvidence, errors)
            }

            JankHunterMode.SAMPLE,
            JankHunterMode.VERSION -> Unit
        }
        validateOutput(command.outputPath, errors)

        return JankHunterValidationResult(errors, warnings)
    }

    private fun validateCli(executable: String, errors: MutableList<String>) {
        val raw = executable.trim()
        if (raw.isBlank()) {
            errors += "Не указан путь к CLI."
            return
        }

        val hasSeparator = raw.contains('/') || raw.contains(File.separatorChar)
        if (hasSeparator) {
            val file = File(raw)
            when {
                !file.isFile -> errors += "CLI не найден: ${file.path}"
                !file.canExecute() -> errors += "CLI найден, но не исполняемый: ${file.path}"
            }
            return
        }

        val found = System.getenv("PATH")
            ?.split(File.pathSeparator)
            ?.map { File(it, raw) }
            ?.any { it.isFile && it.canExecute() }
            ?: false
        if (!found) {
            errors += "Команда '$raw' не найдена в PATH. Укажите полный путь к jankhunter."
        }
    }

    private fun validateModeInputs(
        project: Project,
        request: JankHunterRunRequest,
        errors: MutableList<String>,
        warnings: MutableList<String>,
    ) {
        when (request.mode) {
            JankHunterMode.INSPECT,
            JankHunterMode.PROBLEMS -> validateJhlogs(project, "Logs / globs", request.logs, errors, warnings)

            JankHunterMode.COMPARE,
            JankHunterMode.SCORECARD -> {
                validateJhlogs(project, "Baseline", request.baseline, errors, warnings)
                validateJhlogs(project, "Candidate", request.candidate, errors, warnings)
            }

            JankHunterMode.SAMPLE,
            JankHunterMode.VERSION -> Unit
        }
    }

    private fun validateJhlogs(
        project: Project,
        label: String,
        raw: String,
        errors: MutableList<String>,
        warnings: MutableList<String>,
    ) {
        val parts = JankHunterInputPaths.pathList(raw)
        if (parts.isEmpty()) {
            errors += "$label: укажите хотя бы один .jhlog файл или glob-маску."
            return
        }

        val matched = mutableListOf<Path>()
        for (part in parts) {
            val files = JankHunterInputPaths.expandOne(project, part)
            if (files.isEmpty()) {
                errors += "$label: не найдено файлов для '$part'."
            }
            matched += files
        }

        val nonJhlog = matched.filter { it.extension.lowercase() != "jhlog" }
        if (nonJhlog.isNotEmpty()) {
            warnings += "$label: некоторые выбранные файлы не имеют расширения .jhlog: ${
                nonJhlog.take(3).joinToString { it.fileName.toString() }
            }"
        }
    }

    private fun validateArtifact(project: Project, label: String, raw: String, errors: MutableList<String>) {
        JankHunterInputPaths.pathList(raw).forEach { part ->
            val file = JankHunterInputPaths.resolvePath(project, part).toFile()
            if (!file.isFile) {
                errors += "$label: файл не найден: $part"
            }
        }
    }

    private fun validateOutput(outputPath: String?, errors: MutableList<String>) {
        if (outputPath.isNullOrBlank()) return

        val output = Path.of(outputPath)
        if (output.isDirectory()) {
            errors += "Output указывает на папку, нужен путь к файлу: $outputPath"
            return
        }

        val parent = output.parent ?: Path.of(".")
        try {
            Files.createDirectories(parent)
            if (!Files.isWritable(parent)) {
                errors += "Нет прав на запись в папку результата: $parent"
            }
        } catch (error: Exception) {
            errors += "Не удалось создать папку результата '$parent': ${error.message}"
        }

        if (output.isRegularFile() && !Files.isWritable(output)) {
            errors += "Файл результата существует, но недоступен для записи: $outputPath"
        }
    }

    private val analysisModes = setOf(
        JankHunterMode.INSPECT,
        JankHunterMode.COMPARE,
        JankHunterMode.PROBLEMS,
        JankHunterMode.SCORECARD,
    )
}
