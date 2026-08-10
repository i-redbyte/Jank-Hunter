package io.jankhunter.plugin.ui

import com.intellij.execution.configurations.GeneralCommandLine
import com.intellij.execution.process.OSProcessHandler
import com.intellij.execution.process.ProcessEvent
import com.intellij.execution.process.ProcessListener
import com.intellij.ide.BrowserUtil
import com.intellij.openapi.Disposable
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.fileChooser.FileChooser
import com.intellij.openapi.fileChooser.FileChooserDescriptor
import com.intellij.openapi.project.Project
import com.intellij.openapi.ui.Messages
import com.intellij.openapi.ui.TextFieldWithBrowseButton
import com.intellij.openapi.util.Key
import com.intellij.ui.components.JBCheckBox
import com.intellij.ui.components.JBLabel
import io.jankhunter.plugin.execution.JankHunterCommand
import io.jankhunter.plugin.execution.JankHunterCommandBuilder
import io.jankhunter.plugin.execution.JankHunterDiscoveredLog
import io.jankhunter.plugin.execution.JankHunterLogDiscovery
import io.jankhunter.plugin.execution.JankHunterLogScope
import io.jankhunter.plugin.execution.JankHunterMode
import io.jankhunter.plugin.execution.JankHunterRunRequest
import io.jankhunter.plugin.execution.JankHunterRunValidator
import io.jankhunter.plugin.execution.JankHunterUserPaths
import io.jankhunter.plugin.services.JankHunterNotifications
import io.jankhunter.plugin.services.JankHunterProjectIntrospection
import io.jankhunter.plugin.services.JankHunterProjectService
import io.jankhunter.plugin.settings.JankHunterSettings
import java.awt.BorderLayout
import java.awt.CardLayout
import java.awt.FlowLayout
import java.io.File
import java.nio.charset.StandardCharsets
import java.time.LocalDateTime
import java.time.format.DateTimeFormatter
import java.util.concurrent.Future
import javax.swing.BorderFactory
import javax.swing.JButton
import javax.swing.JComponent
import javax.swing.JPanel
import javax.swing.JSplitPane

class JankHunterToolWindow(
    private val project: Project,
    private val enableBrowser: Boolean = true,
) : Disposable {
    val component: JComponent

    private val settings = JankHunterSettings.getInstance().state
    private val rootLayout = CardLayout()
    private val root = JPanel(rootLayout)
    private val taskLayout = CardLayout()
    private val taskContent = JPanel(taskLayout)
    private val outputDirectoryField = directoryField()
    private val openBrowserCheckBox = JBCheckBox("Открыть отчёт в браузере после генерации")
    private val statusLabel = JBLabel("Выберите логи для анализа")
    private val reportButton = JButton("Отчёт")
    private val compareButton = JButton("Сравнить отчёты")
    private val advancedButton = JButton("Расширенный режим")
    private val runButton = JButton("Сгенерировать отчёт")
    private val stopButton = JButton("Отменить")
    private val processedFingerprints: Set<String>
        get() = settings.processedLogFingerprints.toSet()

    private val reportSelection = selectionPanel(
        "Логи и данные об утечках",
        settings.logsDirectory.ifBlank { JankHunterProjectIntrospection.defaultLogsDirectory(project).path },
    ) { path ->
        settings.logsDirectory = path
    }
    private val baselineSelection = selectionPanel("Было · baseline", settings.baselineLogsDirectory) { path ->
        settings.baselineLogsDirectory = path
    }
    private val candidateSelection = selectionPanel("Стало · candidate", settings.candidateLogsDirectory) { path ->
        settings.candidateLogsDirectory = path
    }
    private val advancedPanel = JankHunterAdvancedPanel(
        project = project,
        onBack = ::showSimpleMode,
        onRun = ::runCurrentReport,
        onScorecard = ::runScorecard,
        onSelectReport = { selectTask(UserTask.REPORT) },
        onSelectCompare = { selectTask(UserTask.COMPARE) },
        onChooseReportDirectory = reportSelection::chooseDirectory,
        onChooseBaselineDirectory = baselineSelection::chooseDirectory,
        onChooseCandidateDirectory = candidateSelection::chooseDirectory,
    )
    private val backgroundTasks = mutableListOf<Future<*>>()
    private var currentTask = UserTask.REPORT
    private var processHandler: OSProcessHandler? = null
    private var commandStarting = false
    private var advancedModeVisible = false
    private var disposed = false

    init {
        outputDirectoryField.text = settings.outputDirectory.ifBlank { JankHunterSettings.defaultOutputDirectory() }
        openBrowserCheckBox.isSelected = settings.openReportExternally

        reportButton.addActionListener { selectTask(UserTask.REPORT) }
        compareButton.addActionListener { selectTask(UserTask.COMPARE) }
        advancedButton.addActionListener { showAdvancedMode() }
        runButton.addActionListener { runCurrentReport() }
        stopButton.addActionListener { stopProcess() }
        stopButton.isEnabled = false

        taskContent.add(reportSelection.component, UserTask.REPORT.card)
        taskContent.add(
            JSplitPane(JSplitPane.VERTICAL_SPLIT, baselineSelection.component, candidateSelection.component).apply {
                resizeWeight = 0.5
                isContinuousLayout = true
                border = null
            },
            UserTask.COMPARE.card,
        )

        root.add(buildSimplePanel(), SIMPLE_CARD)
        root.add(advancedPanel.component, ADVANCED_CARD)
        component = root

        selectTask(UserTask.REPORT)
        JankHunterProjectService.getInstance(project).register(this)
        scanConfiguredSelections()
    }

    fun applyClassFilter(className: String) {
        advancedPanel.setClassFilter(className)
        showAdvancedMode()
    }

    override fun dispose() {
        if (disposed) return
        disposed = true
        JankHunterProjectService.getInstance(project).unregister(this)
        backgroundTasks.forEach { it.cancel(true) }
        backgroundTasks.clear()
        processHandler?.destroyProcess()
        reportSelection.dispose()
        baselineSelection.dispose()
        candidateSelection.dispose()
        advancedPanel.dispose()
    }

    private fun buildSimplePanel(): JComponent = JPanel(BorderLayout(0, 10)).apply {
        border = BorderFactory.createEmptyBorder(10, 10, 10, 10)
        add(
            JPanel(BorderLayout()).apply {
                add(JBLabel("Jank Hunter"), BorderLayout.WEST)
                add(
                    JPanel(FlowLayout(FlowLayout.CENTER, 6, 0)).apply {
                        add(reportButton)
                        add(compareButton)
                    },
                    BorderLayout.CENTER,
                )
                add(advancedButton, BorderLayout.EAST)
            },
            BorderLayout.NORTH,
        )
        add(taskContent, BorderLayout.CENTER)
        add(
            JPanel(BorderLayout(0, 8)).apply {
                add(
                    JPanel(BorderLayout(8, 0)).apply {
                        border = BorderFactory.createTitledBorder("Папка для отчётов")
                        add(outputDirectoryField, BorderLayout.CENTER)
                    },
                    BorderLayout.NORTH,
                )
                add(
                    JPanel(BorderLayout()).apply {
                        add(openBrowserCheckBox, BorderLayout.WEST)
                        add(
                            JPanel(FlowLayout(FlowLayout.RIGHT, 7, 0)).apply {
                                add(stopButton)
                                add(runButton)
                            },
                            BorderLayout.EAST,
                        )
                    },
                    BorderLayout.CENTER,
                )
                add(statusLabel, BorderLayout.SOUTH)
            },
            BorderLayout.SOUTH,
        )
    }

    private fun selectionPanel(
        title: String,
        initialDirectory: String,
        onDirectoryChanged: (String) -> Unit,
    ): JankHunterLogSelectionPanel = JankHunterLogSelectionPanel(
        project = project,
        title = title,
        initialDirectory = initialDirectory,
        processedFingerprints = { processedFingerprints },
        onDirectoryChanged = onDirectoryChanged,
        onSelectionChanged = ::updateAdvancedContext,
    )

    private fun scanConfiguredSelections() {
        listOf(reportSelection, baselineSelection, candidateSelection)
            .filter { it.directory() != null }
            .forEach(JankHunterLogSelectionPanel::scan)
    }

    private fun directoryField(): TextFieldWithBrowseButton = TextFieldWithBrowseButton().apply {
        addActionListener {
            val descriptor = FileChooserDescriptor(false, true, false, false, false, false)
            FileChooser.chooseFile(descriptor, project, null)?.let { selected -> text = selected.path }
        }
    }

    private fun selectTask(task: UserTask) {
        currentTask = task
        taskLayout.show(taskContent, task.card)
        reportButton.isEnabled = task != UserTask.REPORT
        compareButton.isEnabled = task != UserTask.COMPARE
        runButton.text = if (task == UserTask.REPORT) "Сгенерировать отчёт" else "Сравнить отчёты"
        statusLabel.text = if (task == UserTask.REPORT) {
            "Выберите один или несколько логов"
        } else {
            "Выберите две непересекающиеся группы логов"
        }
        updateAdvancedContext()
    }

    private fun showSimpleMode() {
        advancedModeVisible = false
        rootLayout.show(root, SIMPLE_CARD)
    }

    private fun showAdvancedMode() {
        advancedModeVisible = true
        updateAdvancedContext()
        advancedPanel.ensureArtifactsLoaded()
        rootLayout.show(root, ADVANCED_CARD)
    }

    private fun updateAdvancedContext() {
        val text = when (currentTask) {
            UserTask.REPORT -> {
                val logs = reportSelection.selectedLogs().size
                val heap = reportSelection.heapInput().takeIf(String::isNotBlank)?.let { " + HPROF/evidence" }.orEmpty()
                "Отчёт: $logs логов$heap"
            }

            UserTask.COMPARE -> {
                val baseline = baselineSelection.selectedLogs().size
                val candidate = candidateSelection.selectedLogs().size
                "Сравнение: $baseline baseline → $candidate candidate"
            }
        }
        advancedPanel.setContext(text, currentTask == UserTask.COMPARE)
    }

    private fun runCurrentReport() {
        runMode(if (currentTask == UserTask.REPORT) JankHunterMode.INSPECT else JankHunterMode.COMPARE)
    }

    private fun exportProblems() {
        runMode(JankHunterMode.PROBLEMS)
    }

    private fun runScorecard() {
        if (currentTask != UserTask.COMPARE) {
            Messages.showInfoMessage(project, "Scorecard доступен для сравнения baseline и candidate.", "Jank Hunter")
            return
        }
        runMode(JankHunterMode.SCORECARD)
    }

    private fun runMode(mode: JankHunterMode) {
        if (processHandler != null || commandStarting) return
        val advanced = if (advancedModeVisible) advancedPanel.options() else JankHunterAdvancedOptions.SIMPLE
        persistUiSettings(advancedModeVisible, advanced)
        val request = runCatching { buildRequest(mode, advanced) }.getOrElse { error ->
            showError(error.message ?: "Не удалось прочитать параметры запуска.")
            return
        }
        commandStarting = true
        setBusy(true)
        statusLabel.text = "Проверка входных данных…"
        advancedPanel.replaceConsole("")

        runInBackground(
            task = {
                val command = JankHunterCommandBuilder.build(project, request)
                val normalized = request.copy(cliPath = command.executable, output = command.outputPath.orEmpty())
                val validation = JankHunterRunValidator.validate(project, normalized, command)
                PreparedRun(normalized, command, validation.errors, validation.warnings)
            },
            onDone = { result ->
                val prepared = result.getOrElse { error ->
                    commandStarting = false
                    setBusy(false)
                    showError(error.message ?: "Не удалось подготовить запуск.")
                    return@runInBackground
                }
                if (prepared.errors.isNotEmpty()) {
                    commandStarting = false
                    setBusy(false)
                    showError(prepared.errors.joinToString("\n"))
                    return@runInBackground
                }
                startProcess(prepared)
            },
        )
    }

    private fun buildRequest(mode: JankHunterMode, advanced: JankHunterAdvancedOptions): JankHunterRunRequest {
        val reportLogs = reportSelection.selectedLogs()
        val baselineLogs = baselineSelection.selectedLogs()
        val candidateLogs = candidateSelection.selectedLogs()
        val problemsLogs = if (currentTask == UserTask.REPORT) reportLogs else candidateLogs
        val output = outputFile(mode, when (mode) {
            JankHunterMode.COMPARE, JankHunterMode.SCORECARD -> candidateSelection.directory()
            JankHunterMode.PROBLEMS -> if (currentTask == UserTask.REPORT) reportSelection.directory() else candidateSelection.directory()
            else -> reportSelection.directory()
        })

        val inspectHeap = classifyHeap(if (mode == JankHunterMode.PROBLEMS && currentTask == UserTask.COMPARE) {
            candidateSelection.heapInput()
        } else {
            reportSelection.heapInput()
        })
        val baselineHeap = classifyHeap(baselineSelection.heapInput())
        val candidateHeap = classifyHeap(candidateSelection.heapInput())

        return JankHunterRunRequest(
            mode = mode,
            cliPath = advancedPanel.cliPath().ifBlank { settings.cliPath },
            logs = paths(if (mode == JankHunterMode.PROBLEMS) problemsLogs else reportLogs),
            inspectLogScope = JankHunterLogScope.ALL_SELECTED,
            baseline = paths(baselineLogs),
            baselineLogScope = JankHunterLogScope.ALL_SELECTED,
            candidate = paths(candidateLogs),
            candidateLogScope = JankHunterLogScope.ALL_SELECTED,
            output = output.path,
            ownerMap = advanced.ownerMap,
            mapping = advanced.mapping,
            classGraph = advanced.classGraph,
            diagnostics = advanced.diagnostics,
            diCatalog = advanced.diCatalog,
            heapDump = inspectHeap.hprof,
            heapEvidence = inspectHeap.evidence,
            baselineHeapDump = baselineHeap.hprof,
            baselineHeapEvidence = baselineHeap.evidence,
            candidateHeapDump = candidateHeap.hprof,
            candidateHeapEvidence = candidateHeap.evidence,
            route = advanced.route,
            screen = advanced.screen,
            owner = advanced.owner,
            className = advanced.className,
            dataset = "code-problems",
            format = "csv",
            json = false,
            presentation = advanced.presentation,
            reportStyle = advanced.reportStyle,
            animatedBackground = false,
        )
    }

    private fun outputFile(mode: JankHunterMode, sourceDirectory: File?): File {
        val rootDirectory = JankHunterUserPaths.expandHome(outputDirectoryField.text)
            .takeIf(String::isNotEmpty)
            ?.let(::File)
            ?: File(JankHunterSettings.defaultOutputDirectory())
        val source = JankHunterLogDiscovery.sourceName(sourceDirectory)
        val extension = when (mode) {
            JankHunterMode.PROBLEMS -> "csv"
            JankHunterMode.SCORECARD -> "json"
            else -> "html"
        }
        val prefix = when (mode) {
            JankHunterMode.INSPECT -> "report"
            else -> mode.command
        }
        return File(File(rootDirectory, source), "$prefix-${OUTPUT_TIME_FORMAT.format(LocalDateTime.now())}.$extension")
    }

    private fun classifyHeap(path: String): HeapInput {
        if (path.isBlank()) return HeapInput()
        return if (File(path).extension.equals("json", ignoreCase = true)) {
            HeapInput(evidence = path)
        } else {
            HeapInput(hprof = path)
        }
    }

    private fun paths(logs: List<JankHunterDiscoveredLog>): String = logs.joinToString(", ") { it.file.path }

    private fun startProcess(prepared: PreparedRun) {
        val command = prepared.command
        val outputParent = command.outputPath?.let(::File)?.parentFile
        if (outputParent != null && !outputParent.isDirectory && !outputParent.mkdirs()) {
            commandStarting = false
            setBusy(false)
            showError("Не удалось создать папку отчёта: ${outputParent.path}")
            return
        }

        advancedPanel.setCommandPreview(command.displayText())
        advancedPanel.appendConsole("$ ${command.displayText()}\n\n")
        prepared.warnings.forEach { warning -> advancedPanel.appendConsole("warning: $warning\n") }

        val commandLine = GeneralCommandLine(command.executable)
            .withParameters(command.args)
            .withCharset(StandardCharsets.UTF_8)
        project.basePath?.let { commandLine.withWorkDirectory(it) }

        val handler = runCatching { OSProcessHandler(commandLine) }.getOrElse { error ->
            commandStarting = false
            setBusy(false)
            showError(error.message ?: "Не удалось запустить Jank Hunter CLI.")
            return
        }
        commandStarting = false
        processHandler = handler
        handler.addProcessListener(
            object : ProcessListener {
                override fun onTextAvailable(event: ProcessEvent, outputType: Key<*>) {
                    ApplicationManager.getApplication().invokeLater {
                        if (!disposed) advancedPanel.appendConsole(event.text)
                    }
                }

                override fun processTerminated(event: ProcessEvent) {
                    ApplicationManager.getApplication().invokeLater {
                        if (disposed || project.isDisposed) return@invokeLater
                        processHandler = null
                        setBusy(false)
                        advancedPanel.appendConsole("\nПроцесс завершился с кодом ${event.exitCode}\n")
                        if (event.exitCode == 0) {
                            onSucceeded(prepared.request, command.outputPath)
                        } else {
                            statusLabel.text = "Ошибка генерации · код ${event.exitCode}"
                            showAdvancedMode()
                            JankHunterNotifications.error(
                                project,
                                "Jank Hunter",
                                "Команда завершилась с кодом ${event.exitCode}. Подробности показаны в диагностике.",
                            )
                        }
                    }
                }
            },
        )
        statusLabel.text = "Генерация отчёта…"
        handler.startNotify()
    }

    private fun onSucceeded(request: JankHunterRunRequest, outputPath: String?) {
        markCurrentLogsProcessed(request.mode)
        val output = outputPath?.let(::File)
        statusLabel.text = if (output?.isFile == true) "Готово: ${output.path}" else "Готово"
        if (output?.isFile == true && output.extension.equals("html", ignoreCase = true) && openBrowserCheckBox.isSelected) {
            if (enableBrowser) BrowserUtil.browse(output.toURI())
        }
        if (output?.isFile == true) {
            JankHunterNotifications.reportReady(
                project,
                output.path,
                problemCount = null,
                openReport = { BrowserUtil.browse(output.toURI()) },
                openProblems = null,
                rerun = { runMode(request.mode) },
            )
        }
        when (currentTask) {
            UserTask.REPORT -> reportSelection.scan()
            UserTask.COMPARE -> {
                baselineSelection.scan()
                candidateSelection.scan()
            }
        }
    }

    private fun markCurrentLogsProcessed(mode: JankHunterMode) {
        val logs = when (mode) {
            JankHunterMode.INSPECT -> reportSelection.selectedLogs()
            JankHunterMode.PROBLEMS -> if (currentTask == UserTask.REPORT) {
                reportSelection.selectedLogs()
            } else {
                candidateSelection.selectedLogs()
            }
            JankHunterMode.COMPARE, JankHunterMode.SCORECARD ->
                baselineSelection.selectedLogs() + candidateSelection.selectedLogs()
            else -> emptyList()
        }
        val fingerprints = LinkedHashSet(settings.processedLogFingerprints)
        logs.mapTo(fingerprints, JankHunterDiscoveredLog::fingerprint)
        settings.processedLogFingerprints = fingerprints.toList()
            .takeLast(MAX_PROCESSED_FINGERPRINTS)
            .toMutableList()
    }

    private fun persistUiSettings(includeAdvanced: Boolean, advanced: JankHunterAdvancedOptions) {
        settings.outputDirectory = outputDirectoryField.text.trim().ifBlank { JankHunterSettings.defaultOutputDirectory() }
        settings.openReportExternally = openBrowserCheckBox.isSelected
        settings.cliPath = advancedPanel.cliPath()
        if (includeAdvanced) {
            settings.presentationMode = advanced.presentation
            settings.reportStyle = advanced.reportStyle
        }
    }

    private fun stopProcess() {
        processHandler?.destroyProcess()
        statusLabel.text = "Остановка…"
    }

    private fun setBusy(busy: Boolean) {
        runButton.isEnabled = !busy
        stopButton.isEnabled = busy
        reportButton.isEnabled = !busy && currentTask != UserTask.REPORT
        compareButton.isEnabled = !busy && currentTask != UserTask.COMPARE
        advancedButton.isEnabled = !busy
    }

    private fun showError(message: String) {
        statusLabel.text = "Проверьте входные данные"
        Messages.showErrorDialog(project, message, "Jank Hunter")
    }

    private fun <T> runInBackground(task: () -> T, onDone: (Result<T>) -> Unit) {
        val future = ApplicationManager.getApplication().executeOnPooledThread {
            val result = runCatching(task)
            ApplicationManager.getApplication().invokeLater {
                if (!disposed && !project.isDisposed) onDone(result)
            }
        }
        backgroundTasks.removeAll { it.isDone || it.isCancelled }
        backgroundTasks += future
    }

    private data class PreparedRun(
        val request: JankHunterRunRequest,
        val command: JankHunterCommand,
        val errors: List<String>,
        val warnings: List<String>,
    )

    private data class HeapInput(val hprof: String = "", val evidence: String = "")

    private enum class UserTask(val card: String) {
        REPORT("report"),
        COMPARE("compare"),
    }

    companion object {
        private const val SIMPLE_CARD = "simple"
        private const val ADVANCED_CARD = "advanced"
        private const val MAX_PROCESSED_FINGERPRINTS = 1_000
        private val OUTPUT_TIME_FORMAT = DateTimeFormatter.ofPattern("yyyy-MM-dd-HH-mm-ss")
    }
}
