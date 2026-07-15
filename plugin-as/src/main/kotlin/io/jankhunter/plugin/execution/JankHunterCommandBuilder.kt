package io.jankhunter.plugin.execution

import com.intellij.openapi.project.Project
import io.jankhunter.plugin.settings.JankHunterSettings
import java.io.File
import java.nio.file.Path
import java.time.LocalDateTime
import java.time.format.DateTimeFormatter

data class JankHunterRunRequest(
    val mode: JankHunterMode,
    val cliPath: String,
    val logs: String,
    val inspectLogScope: JankHunterLogScope = JankHunterLogScope.ALL_SELECTED,
    val baseline: String,
    val baselineLogScope: JankHunterLogScope = JankHunterLogScope.ALL_SELECTED,
    val candidate: String,
    val candidateLogScope: JankHunterLogScope = JankHunterLogScope.ALL_SELECTED,
    val output: String,
    val ownerMap: String,
    val mapping: String,
    val classGraph: String,
    val diagnostics: String,
    val diCatalog: String,
    val heapDump: String,
    val heapEvidence: String,
    val baselineHeapDump: String,
    val baselineHeapEvidence: String,
    val candidateHeapDump: String,
    val candidateHeapEvidence: String,
    val route: String,
    val screen: String,
    val owner: String,
    val className: String,
    val dataset: String,
    val format: String,
    val json: Boolean,
    val presentation: Boolean,
)

data class JankHunterCommand(
    val executable: String,
    val args: List<String>,
    val outputPath: String?,
) {
    fun displayText(): String = buildString {
        append(shellQuote(executable))
        args.forEach { arg ->
            append(' ')
            append(shellQuote(arg))
        }
    }

    private fun shellQuote(value: String): String {
        if (value.all { it.isLetterOrDigit() || it in ".:/_-=*," }) {
            return value
        }
        return "'" + value.replace("'", "'\\''") + "'"
    }
}

object JankHunterCommandBuilder {
    private val timestampFormatter = DateTimeFormatter.ofPattern("yyyyMMdd-HHmmss")

    fun defaultCliPath(project: Project): String = JankHunterArtifactDiscovery.detectCli(project)

    fun build(project: Project, request: JankHunterRunRequest): JankHunterCommand {
        val executable = normalizeExecutablePath(project, request.cliPath.trim().ifEmpty { defaultCliPath(project) })
        val args = mutableListOf(request.mode.command)
        var outputPath: String? = request.output.trim().takeIf(String::isNotEmpty)?.let { normalizeOutputPath(project, it) }

        fun addFlag(name: String, value: String) {
            val trimmed = value.trim()
            if (trimmed.isNotEmpty()) {
                args += "--$name"
                args += trimmed
            }
        }

        fun addAnalysisFlags() {
            addFlag("owner-map", request.ownerMap)
            addFlag("mapping", request.mapping)
            addFlag("class-graph", request.classGraph)
            addFlag("instrumentation-diagnostics", request.diagnostics)
            addFlag("di-catalog", request.diCatalog)
            addFlag("route", request.route)
            addFlag("screen", request.screen)
            addFlag("owner", request.owner)
            addFlag("class", request.className)
        }

        fun addInspectHeapFlags() {
            addFlag("heap-dump", request.heapDump)
            addFlag("heap-evidence", request.heapEvidence)
        }

        fun addCompareHeapFlags() {
            addFlag("baseline-heap-dump", request.baselineHeapDump)
            addFlag("baseline-heap-evidence", request.baselineHeapEvidence)
            addFlag("candidate-heap-dump", request.candidateHeapDump)
            addFlag("candidate-heap-evidence", request.candidateHeapEvidence)
        }

        fun ensureOutput(extension: String): String {
            if (outputPath == null) {
                outputPath = defaultOutputPath(project, request.mode, extension, request.format)
            }
            return outputPath.orEmpty()
        }

        when (request.mode) {
            JankHunterMode.INSPECT -> {
                val logs = scopedPathList(project, request.logs, request.inspectLogScope)
                require(logs.isNotEmpty()) { "Inspect: укажите хотя бы один .jhlog файл или glob-маску." }
                addAnalysisFlags()
                addInspectHeapFlags()
                if (request.inspectLogScope == JankHunterLogScope.ALL_SELECTED) args += "--all-sessions"
                if (request.json) args += "--json"
                if (request.presentation) args += "--presentation"
                addFlag("out", ensureOutput("html"))
                args += logs
            }

            JankHunterMode.COMPARE -> {
                val baseline = scopedPathList(project, request.baseline, request.baselineLogScope)
                val candidate = scopedPathList(project, request.candidate, request.candidateLogScope)
                require(baseline.isNotEmpty()) { "Compare: укажите baseline .jhlog файлы или glob-маски." }
                require(candidate.isNotEmpty()) { "Compare: укажите candidate .jhlog файлы или glob-маски." }
                addAnalysisFlags()
                addFlag("baseline", baseline.joinToString(","))
                addFlag("candidate", candidate.joinToString(","))
                addCompareHeapFlags()
                if (request.json) args += "--json"
                if (request.presentation) args += "--presentation"
                addFlag("out", ensureOutput("html"))
            }

            JankHunterMode.PROBLEMS -> {
                val logs = pathList(request.logs)
                require(logs.isNotEmpty()) { "Problems: укажите хотя бы один .jhlog файл или glob-маску." }
                addAnalysisFlags()
                addInspectHeapFlags()
                addFlag("format", request.format.ifBlank { "csv" })
                addFlag("dataset", request.dataset.ifBlank { "code-problems" })
                val extension = if (request.format.equals("json", ignoreCase = true)) "json" else "csv"
                addFlag("out", ensureOutput(extension))
                args += logs
            }

            JankHunterMode.SCORECARD -> {
                val baseline = scopedPathList(project, request.baseline, request.baselineLogScope)
                val candidate = scopedPathList(project, request.candidate, request.candidateLogScope)
                require(baseline.isNotEmpty()) { "Scorecard: укажите baseline .jhlog файлы или glob-маски." }
                require(candidate.isNotEmpty()) { "Scorecard: укажите candidate .jhlog файлы или glob-маски." }
                addAnalysisFlags()
                addFlag("baseline", baseline.joinToString(","))
                addFlag("candidate", candidate.joinToString(","))
                addCompareHeapFlags()
                addFlag("out", ensureOutput("json"))
            }

            JankHunterMode.SAMPLE -> {
                addFlag("out", ensureOutput("jhlog"))
            }

            JankHunterMode.VERSION -> {
                outputPath = null
            }
        }

        return JankHunterCommand(executable, args, outputPath)
    }

    private fun normalizeExecutablePath(project: Project, raw: String): String {
        val hasSeparator = raw.contains('/') || raw.contains(File.separatorChar)
        if (!hasSeparator) return raw
        val path = Path.of(raw)
        return if (path.isAbsolute) {
            path.normalize().toString()
        } else {
            Path.of(project.basePath ?: ".").resolve(path).normalize().toString()
        }
    }

    private fun normalizeOutputPath(project: Project, raw: String): String {
        val path = Path.of(raw)
        return if (path.isAbsolute) {
            path.normalize().toString()
        } else {
            Path.of(project.basePath ?: ".").resolve(path).normalize().toString()
        }
    }

    private fun defaultOutputPath(
        project: Project,
        mode: JankHunterMode,
        extension: String,
        format: String,
    ): String {
        val settings = JankHunterSettings.getInstance().state
        val baseDir = settings.outputDirectory.trim().ifEmpty {
            val basePath = project.basePath ?: System.getProperty("user.home")
            File(basePath, "build/jankhunter").path
        }
        val effectiveExtension = if (mode == JankHunterMode.PROBLEMS) {
            if (format.equals("json", ignoreCase = true)) "json" else extension
        } else {
            extension
        }
        val timestamp = LocalDateTime.now().format(timestampFormatter)
        return File(baseDir, "${mode.command}-$timestamp.$effectiveExtension").path
    }

    private fun pathList(raw: String): List<String> = JankHunterInputPaths.pathList(raw)

    private fun scopedPathList(project: Project, raw: String, scope: JankHunterLogScope): List<String> {
        val parts = pathList(raw)
        if (parts.isEmpty()) return emptyList()
        return when (scope) {
            JankHunterLogScope.ALL_SELECTED -> parts
            JankHunterLogScope.LATEST_LOG -> latestExistingLog(project, raw)?.let { listOf(it) } ?: parts.take(1)
        }
    }

    private fun latestExistingLog(project: Project, raw: String): String? =
        JankHunterSessionLogFiles.latest(
            JankHunterInputPaths.expandExistingFiles(project, raw).map { it.toFile() },
        )?.toPath()?.normalize()?.toString()
}
