package io.jankhunter.plugin.ui

import com.intellij.execution.ExecutionException
import com.intellij.execution.configurations.GeneralCommandLine
import com.intellij.execution.process.OSProcessHandler
import com.intellij.execution.process.ProcessEvent
import com.intellij.execution.process.ProcessListener
import com.intellij.openapi.ide.CopyPasteManager
import com.intellij.ide.BrowserUtil
import com.intellij.openapi.Disposable
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.fileChooser.FileChooser
import com.intellij.openapi.fileChooser.FileChooserDescriptor
import com.intellij.openapi.fileEditor.FileEditorManager
import com.intellij.openapi.project.Project
import com.intellij.openapi.ui.Messages
import com.intellij.openapi.ui.TextFieldWithBrowseButton
import com.intellij.openapi.vfs.LocalFileSystem
import com.intellij.ui.components.JBLabel
import com.intellij.ui.components.JBScrollPane
import com.intellij.ui.components.JBTextArea
import com.intellij.ui.components.JBTextField
import com.intellij.ui.JBColor
import com.intellij.ui.jcef.JBCefApp
import com.intellij.ui.jcef.JBCefBrowser
import com.intellij.ui.jcef.JBCefBrowserBase
import com.intellij.ui.jcef.JBCefJSQuery
import com.intellij.ui.table.JBTable
import io.jankhunter.plugin.execution.JankHunterArtifactDiscovery
import io.jankhunter.plugin.execution.JankHunterArtifactSet
import io.jankhunter.plugin.execution.JankHunterCommand
import io.jankhunter.plugin.execution.JankHunterCommandBuilder
import io.jankhunter.plugin.execution.JankHunterMode
import io.jankhunter.plugin.execution.JankHunterPreset
import io.jankhunter.plugin.execution.JankHunterRunRequest
import io.jankhunter.plugin.execution.JankHunterRunValidator
import io.jankhunter.plugin.profiles.JankHunterProfileStore
import io.jankhunter.plugin.problems.ProblemsParser
import io.jankhunter.plugin.problems.ProblemsTable
import io.jankhunter.plugin.problems.ProblemsTableModel
import io.jankhunter.plugin.problems.SourceNavigator
import io.jankhunter.plugin.services.JankHunterAdbIntegration
import io.jankhunter.plugin.services.JankHunterCliLifecycle
import io.jankhunter.plugin.services.JankHunterDevice
import io.jankhunter.plugin.services.JankHunterGradleIntegration
import io.jankhunter.plugin.services.JankHunterNotifications
import io.jankhunter.plugin.services.JankHunterProjectService
import io.jankhunter.plugin.settings.JankHunterSettings
import io.jankhunter.plugin.settings.RecentRun
import java.awt.BorderLayout
import java.awt.Color
import java.awt.FlowLayout
import java.awt.GridBagConstraints
import java.awt.GridBagLayout
import java.awt.Insets
import java.awt.datatransfer.StringSelection
import java.awt.event.MouseAdapter
import java.awt.event.MouseEvent
import java.io.File
import java.nio.charset.StandardCharsets
import java.time.LocalDateTime
import java.time.format.DateTimeFormatter
import javax.swing.DefaultComboBoxModel
import javax.swing.JButton
import javax.swing.JCheckBox
import javax.swing.JComboBox
import javax.swing.JComponent
import javax.swing.JPanel
import javax.swing.JSplitPane
import javax.swing.JTabbedPane
import javax.swing.ListSelectionModel
import javax.swing.SwingUtilities
import javax.swing.RowFilter
import javax.swing.table.DefaultTableCellRenderer
import javax.swing.table.DefaultTableModel
import javax.swing.table.TableRowSorter
import org.cef.browser.CefBrowser
import org.cef.handler.CefLoadHandlerAdapter

class JankHunterToolWindow(
    private val project: Project,
    private val enableBrowser: Boolean = true,
) : Disposable {
    val component: JComponent

    private val presetCombo = JComboBox(JankHunterPreset.entries.toTypedArray())
    private val applyPresetButton = JButton("Apply")
    private val profileStore = JankHunterProfileStore(project)
    private val profileCombo = JComboBox<String>()
    private val loadProfileButton = JButton("Load")
    private val saveProfileButton = JButton("Save")
    private val modeCombo = JComboBox(JankHunterMode.entries.toTypedArray())
    private val cliPathField = browseField()
    private val checkCliButton = JButton("Check CLI")
    private val buildCliButton = JButton("Build CLI")
    private val artifactCombo = JComboBox<String>()
    private val applyArtifactsButton = JButton("Apply")
    private val scanArtifactsButton = JButton("Scan")
    private val logsField = browseField(appendSelection = true)
    private val baselineField = browseField(appendSelection = true)
    private val candidateField = browseField(appendSelection = true)
    private val outputField = JBTextField()
    private val ownerMapField = browseField()
    private val mappingField = browseField()
    private val classGraphField = browseField()
    private val diagnosticsField = browseField()
    private val heapDumpField = browseField()
    private val heapEvidenceField = browseField()
    private val baselineHeapDumpField = browseField()
    private val baselineHeapEvidenceField = browseField()
    private val candidateHeapDumpField = browseField()
    private val candidateHeapEvidenceField = browseField()
    private val routeField = JBTextField()
    private val screenField = JBTextField()
    private val ownerField = JBTextField()
    private val classField = JBTextField()
    private val datasetCombo = JComboBox(arrayOf("code-problems", "leaks", "influence", "math-findings"))
    private val formatCombo = JComboBox(arrayOf("csv", "json"))
    private val jsonCheckBox = JCheckBox("JSON")
    private val presentationCheckBox = JCheckBox("Presentation")
    private val openInIdeCheckBox = JCheckBox("Open in IDE")
    private val openExternalCheckBox = JCheckBox("Browser")
    private val runButton = JButton("Run")
    private val stopButton = JButton("Stop")
    private val openInIdeButton = JButton("Open In IDE")
    private val openBrowserButton = JButton("Open Browser")
    private val clearButton = JButton("Clear")
    private val findLogsButton = JButton("Find Logs")
    private val buildSampleButton = JButton("Build Sample")
    private val connectedTestButton = JButton("Connected Test")
    private val collectArtifactsButton = JButton("Build Artifacts")
    private val gradleInspectButton = JButton("Artifacts + Inspect")
    private val deviceCombo = JComboBox<JankHunterDevice>()
    private val scanDevicesButton = JButton("Devices")
    private val packageField = JBTextField("io.jankhunter.sample")
    private val remoteLogsField = JBTextField("/sdcard/Android/data/io.jankhunter.sample/files/jankhunter")
    private val pullLogsButton = JButton("Pull Logs")
    private val openRemoteLogsButton = JButton("Remote Folder")
    private val collectInspectButton = JButton("Collect + Inspect")
    private val consoleArea = JBTextArea()
    private val tabs = JTabbedPane()
    private val browser = if (enableBrowser && JBCefApp.isSupported()) JBCefBrowser() else null
    private val browserQuery = browser?.let(::createBrowserQuery)
    private val problemsModel = ProblemsTableModel()
    private val problemsTable = JBTable(problemsModel)
    private val problemsSorter = TableRowSorter(problemsModel)
    private var rawProblemsTable: ProblemsTable = ProblemsTable(emptyList(), emptyList())
    private val severityFilter = JComboBox(arrayOf("all", "critical", "high", "medium", "low", "info"))
    private val categoryFilter = JBTextField()
    private val screenFilter = JBTextField()
    private val ownerFilter = JBTextField()
    private val groupProblemsCombo = JComboBox(arrayOf("none", "class", "owner"))
    private val applyProblemsFilterButton = JButton("Filter")
    private val resetProblemsFilterButton = JButton("Reset")
    private val openProblemSourceButton = JButton("Open Source")
    private val openProblemReportButton = JButton("Open Report")
    private val copyRecommendationButton = JButton("Copy Recommendation")
    private val reloadProblemsButton = JButton("Reload")
    private val historyModel = object : DefaultTableModel(arrayOf("Time", "Mode", "Output", "Command"), 0) {
        override fun isCellEditable(row: Int, column: Int): Boolean = false
    }
    private val historyTable = JBTable(historyModel)
    private val loadHistoryButton = JButton("Load")
    private val rerunHistoryButton = JButton("Rerun")
    private val openHistoryOutputButton = JButton("Open Output")
    private val rowMap = linkedMapOf<String, JPanel>()

    private val artifactSets = mutableListOf<JankHunterArtifactSet>()
    private var processHandler: OSProcessHandler? = null
    private var lastOutputPath: String? = null
    private var lastHtmlOutputPath: String? = null
    private var lastRunRequest: JankHunterRunRequest? = null

    init {
        val settings = JankHunterSettings.getInstance().state
        cliPathField.text = settings.cliPath.ifBlank { JankHunterCommandBuilder.defaultCliPath(project) }
        presentationCheckBox.isSelected = settings.presentationMode
        openInIdeCheckBox.isSelected = settings.openReportInIde
        openExternalCheckBox.isSelected = settings.openReportExternally

        consoleArea.isEditable = false
        consoleArea.lineWrap = true
        consoleArea.wrapStyleWord = false

        configureProfileCombo()
        configureProblemsTable()
        configureHistoryTable()
        configureBrowserBridge()

        tabs.addTab("Console", JBScrollPane(consoleArea))
        tabs.addTab("Report", browser?.component ?: JBLabel("JCEF is not available in this IDE runtime."))
        tabs.addTab("Problems", buildProblemsPanel())
        tabs.addTab("History", buildHistoryPanel())

        configureTooltips()
        configureActions()

        stopButton.isEnabled = false
        openInIdeButton.isEnabled = false
        openBrowserButton.isEnabled = false

        val root = JPanel(BorderLayout())
        root.add(buildToolbar(), BorderLayout.NORTH)
        val splitPane = JSplitPane(JSplitPane.VERTICAL_SPLIT, JBScrollPane(buildForm()), tabs).apply {
            resizeWeight = 0.46
            isContinuousLayout = true
        }
        root.add(splitPane, BorderLayout.CENTER)

        component = root
        JankHunterProjectService.getInstance(project).register(this)
        refreshArtifacts(autoApplyBlankFields = true, showMessage = false)
        refreshHistoryTable()
        updateModeVisibility()
    }

    override fun dispose() {
        JankHunterProjectService.getInstance(project).unregister(this)
        processHandler?.destroyProcess()
        browserQuery?.dispose()
        browser?.dispose()
    }

    private fun configureActions() {
        runButton.addActionListener { runRequest(collectRequest()) }
        stopButton.addActionListener { stopCommand() }
        openInIdeButton.addActionListener { openLastOutputInsideIde() }
        openBrowserButton.addActionListener { openLastOutputInBrowser() }
        clearButton.addActionListener { consoleArea.text = "" }
        findLogsButton.addActionListener { fillRecentLogs() }
        applyPresetButton.addActionListener { applySelectedPreset() }
        loadProfileButton.addActionListener { loadSelectedProfile() }
        saveProfileButton.addActionListener { saveSelectedProfile() }
        checkCliButton.addActionListener { checkCliStatus() }
        buildCliButton.addActionListener { buildCli() }
        scanArtifactsButton.addActionListener { refreshArtifacts(autoApplyBlankFields = false, showMessage = true) }
        applyArtifactsButton.addActionListener { applySelectedArtifacts(force = true) }
        buildSampleButton.addActionListener { runGradleTask(JankHunterGradleIntegration.sampleAssembleTask()) }
        connectedTestButton.addActionListener { runGradleTask(JankHunterGradleIntegration.sampleConnectedTestTask()) }
        collectArtifactsButton.addActionListener {
            runGradleTask(JankHunterGradleIntegration.collectArtifactsTask()) {
                refreshArtifacts(autoApplyBlankFields = true, showMessage = false)
            }
        }
        gradleInspectButton.addActionListener { buildArtifactsThenInspect() }
        scanDevicesButton.addActionListener { refreshDevices() }
        packageField.addActionListener { syncRemotePathFromPackage() }
        pullLogsButton.addActionListener { pullDeviceLogs(openInspect = false) }
        openRemoteLogsButton.addActionListener { openRemoteLogFolder() }
        collectInspectButton.addActionListener { pullDeviceLogs(openInspect = true) }
        modeCombo.addActionListener {
            outputField.text = ""
            updateModeVisibility()
        }
        applyProblemsFilterButton.addActionListener { applyProblemsView() }
        resetProblemsFilterButton.addActionListener { resetProblemsFilters() }
        groupProblemsCombo.addActionListener { applyProblemsView() }
        openProblemSourceButton.addActionListener { openSelectedProblemSource() }
        openProblemReportButton.addActionListener { openProblemReport() }
        copyRecommendationButton.addActionListener { copySelectedRecommendation() }
        reloadProblemsButton.addActionListener { lastOutputPath?.let(::loadProblemsTable) }
        loadHistoryButton.addActionListener { loadSelectedHistory() }
        rerunHistoryButton.addActionListener { rerunSelectedHistory() }
        openHistoryOutputButton.addActionListener { openSelectedHistoryOutput() }
    }

    private fun configureProblemsTable() {
        problemsTable.autoCreateRowSorter = true
        problemsTable.rowSorter = problemsSorter
        problemsTable.selectionModel.selectionMode = ListSelectionModel.SINGLE_SELECTION
        problemsTable.setDefaultRenderer(Any::class.java, SeverityBadgeRenderer())
        problemsTable.addMouseListener(
            object : MouseAdapter() {
                override fun mouseClicked(event: MouseEvent) {
                    if (event.clickCount == 2) {
                        openSelectedProblemSource()
                    }
                }
            },
        )
    }

    private fun configureProfileCombo() {
        profileCombo.model = DefaultComboBoxModel(profileStore.load().profiles.keys.toTypedArray())
        profileCombo.selectedItem = "debug"
    }

    private fun configureBrowserBridge() {
        val cefBrowser = browser ?: return
        val query = browserQuery ?: return
        query.addHandler { className ->
            SourceNavigator.open(project, mapOf("class" to className))
            JBCefJSQuery.Response(null)
        }
        cefBrowser.jbCefClient.addLoadHandler(
            object : CefLoadHandlerAdapter() {
                override fun onLoadEnd(browser: CefBrowser, frame: org.cef.browser.CefFrame, httpStatusCode: Int) {
                    if (!frame.isMain) return
                    val js = """
                        (function() {
                          const send = function(value) { ${query.inject("value")} };
                          const re = /\b([a-zA-Z_][\w]*\.)+[A-Z][A-Za-z0-9_]*(?:\$[A-Za-z0-9_]+)?\b/;
                          document.addEventListener('click', function(event) {
                            let node = event.target;
                            while (node && node !== document.body) {
                              const text = (node.innerText || node.textContent || '').trim();
                              const match = text.match(re);
                              if (match) {
                                event.preventDefault();
                                send(match[0]);
                                return;
                              }
                              node = node.parentElement;
                            }
                          }, true);
                        })();
                    """.trimIndent()
                    cefBrowser.runJavaScript(js)
                }
            },
            cefBrowser.cefBrowser,
        )
    }

    private fun configureHistoryTable() {
        historyTable.autoCreateRowSorter = true
        historyTable.selectionModel.selectionMode = ListSelectionModel.SINGLE_SELECTION
        historyTable.addMouseListener(
            object : MouseAdapter() {
                override fun mouseClicked(event: MouseEvent) {
                    if (event.clickCount == 2) {
                        rerunSelectedHistory()
                    }
                }
            },
        )
    }

    private fun buildToolbar(): JComponent =
        JPanel(FlowLayout(FlowLayout.LEFT, 8, 4)).apply {
            add(runButton)
            add(stopButton)
            add(openInIdeButton)
            add(openBrowserButton)
            add(clearButton)
            add(findLogsButton)
        }

    private fun buildProblemsPanel(): JComponent =
        JPanel(BorderLayout()).apply {
            add(
                buildProblemsToolbar(),
                BorderLayout.NORTH,
            )
            add(JBScrollPane(problemsTable), BorderLayout.CENTER)
        }

    private fun buildProblemsToolbar(): JComponent =
        JPanel(FlowLayout(FlowLayout.LEFT, 8, 4)).apply {
            add(JBLabel("Severity"))
            add(severityFilter)
            add(JBLabel("Category"))
            add(categoryFilter)
            add(JBLabel("Screen"))
            add(screenFilter)
            add(JBLabel("Owner"))
            add(ownerFilter)
            add(JBLabel("Group"))
            add(groupProblemsCombo)
            add(applyProblemsFilterButton)
            add(resetProblemsFilterButton)
            add(openProblemSourceButton)
            add(openProblemReportButton)
            add(copyRecommendationButton)
            add(reloadProblemsButton)
        }

    private fun buildHistoryPanel(): JComponent =
        JPanel(BorderLayout()).apply {
            add(
                JPanel(FlowLayout(FlowLayout.LEFT, 8, 4)).apply {
                    add(loadHistoryButton)
                    add(rerunHistoryButton)
                    add(openHistoryOutputButton)
                },
                BorderLayout.NORTH,
            )
            add(JBScrollPane(historyTable), BorderLayout.CENTER)
        }

    private fun buildForm(): JComponent {
        val form = JPanel(GridBagLayout())
        var row = 0
        addRow(form, row++, "Profile", buildProfilePanel(), "profile")
        addRow(form, row++, "Preset", buildPresetPanel(), "preset")
        addRow(form, row++, "Mode", modeCombo)
        addRow(form, row++, "CLI", buildCliPanel(), "cli")
        addWideRow(form, row++, buildGradlePanel(), "gradle")
        addWideRow(form, row++, buildAdbPanel(), "adb")
        addRow(form, row++, "Artifacts", buildArtifactsPanel(), "artifacts")
        addRow(form, row++, "Logs / globs", logsField, "logs")
        addRow(form, row++, "Baseline", baselineField, "baseline")
        addRow(form, row++, "Candidate", candidateField, "candidate")
        addRow(form, row++, "Output", outputField, "output")
        addWideRow(form, row++, buildOptionsPanel(), "options")
        addRow(form, row++, "Owner map", ownerMapField, "ownerMap")
        addRow(form, row++, "Mapping", mappingField, "mapping")
        addRow(form, row++, "Class graph", classGraphField, "classGraph")
        addRow(form, row++, "Diagnostics", diagnosticsField, "diagnostics")
        addRow(form, row++, "Heap dump", heapDumpField, "heapDump")
        addRow(form, row++, "Heap evidence", heapEvidenceField, "heapEvidence")
        addRow(form, row++, "Baseline heap dump", baselineHeapDumpField, "baselineHeapDump")
        addRow(form, row++, "Baseline heap evidence", baselineHeapEvidenceField, "baselineHeapEvidence")
        addRow(form, row++, "Candidate heap dump", candidateHeapDumpField, "candidateHeapDump")
        addRow(form, row++, "Candidate heap evidence", candidateHeapEvidenceField, "candidateHeapEvidence")
        addRow(form, row++, "Route", routeField, "route")
        addRow(form, row++, "Screen", screenField, "screen")
        addRow(form, row++, "Owner", ownerField, "owner")
        addRow(form, row++, "Class", classField, "class")
        addRow(form, row++, "Dataset", datasetCombo, "dataset")
        addRow(form, row++, "Format", formatCombo, "format")

        form.add(
            JPanel(),
            GridBagConstraints().apply {
                gridx = 0
                gridy = row
                gridwidth = 2
                weighty = 1.0
                fill = GridBagConstraints.BOTH
            },
        )
        return form
    }

    private fun buildPresetPanel(): JComponent =
        JPanel(BorderLayout(8, 0)).apply {
            add(presetCombo, BorderLayout.CENTER)
            add(applyPresetButton, BorderLayout.EAST)
        }

    private fun buildProfilePanel(): JComponent =
        JPanel(BorderLayout(8, 0)).apply {
            add(profileCombo, BorderLayout.CENTER)
            add(
                JPanel(FlowLayout(FlowLayout.RIGHT, 4, 0)).apply {
                    add(loadProfileButton)
                    add(saveProfileButton)
                },
                BorderLayout.EAST,
            )
        }

    private fun buildCliPanel(): JComponent =
        JPanel(BorderLayout(8, 0)).apply {
            add(cliPathField, BorderLayout.CENTER)
            add(
                JPanel(FlowLayout(FlowLayout.RIGHT, 4, 0)).apply {
                    add(checkCliButton)
                    add(buildCliButton)
                },
                BorderLayout.EAST,
            )
        }

    private fun buildGradlePanel(): JComponent =
        JPanel(FlowLayout(FlowLayout.LEFT, 8, 0)).apply {
            add(JBLabel("Gradle"))
            add(buildSampleButton)
            add(connectedTestButton)
            add(collectArtifactsButton)
            add(gradleInspectButton)
        }

    private fun buildAdbPanel(): JComponent =
        JPanel(BorderLayout(8, 0)).apply {
            add(
                JPanel(FlowLayout(FlowLayout.LEFT, 8, 0)).apply {
                    add(JBLabel("ADB"))
                    add(deviceCombo)
                    add(scanDevicesButton)
                    add(JBLabel("Package"))
                    add(packageField)
                    add(JBLabel("Remote"))
                    add(remoteLogsField)
                },
                BorderLayout.CENTER,
            )
            add(
                JPanel(FlowLayout(FlowLayout.RIGHT, 4, 0)).apply {
                    add(openRemoteLogsButton)
                    add(pullLogsButton)
                    add(collectInspectButton)
                },
                BorderLayout.EAST,
            )
        }

    private fun buildArtifactsPanel(): JComponent =
        JPanel(BorderLayout(8, 0)).apply {
            add(artifactCombo, BorderLayout.CENTER)
            add(
                JPanel(FlowLayout(FlowLayout.RIGHT, 4, 0)).apply {
                    add(scanArtifactsButton)
                    add(applyArtifactsButton)
                },
                BorderLayout.EAST,
            )
        }

    private fun buildOptionsPanel(): JComponent =
        JPanel(FlowLayout(FlowLayout.LEFT, 8, 0)).apply {
            add(jsonCheckBox)
            add(presentationCheckBox)
            add(openInIdeCheckBox)
            add(openExternalCheckBox)
        }

    private fun addRow(
        form: JPanel,
        row: Int,
        label: String,
        input: JComponent,
        key: String? = null,
    ) {
        val rowPanel = JPanel(BorderLayout(8, 0)).apply {
            add(JBLabel(label), BorderLayout.WEST)
            add(input, BorderLayout.CENTER)
        }
        form.add(
            rowPanel,
            GridBagConstraints().apply {
                gridx = 0
                gridy = row
                gridwidth = 2
                weightx = 1.0
                fill = GridBagConstraints.HORIZONTAL
                insets = Insets(3, 6, 3, 6)
            },
        )
        if (key != null) {
            rowMap[key] = rowPanel
        }
    }

    private fun addWideRow(form: JPanel, row: Int, input: JComponent, key: String) {
        form.add(
            input,
            GridBagConstraints().apply {
                gridx = 0
                gridy = row
                gridwidth = 2
                weightx = 1.0
                fill = GridBagConstraints.HORIZONTAL
                insets = Insets(3, 6, 3, 6)
            },
        )
        rowMap[key] = input as JPanel
    }

    private fun updateModeVisibility() {
        val mode = selectedMode()
        modeCombo.toolTipText = hint(mode.hint)
        val analysis = mode in setOf(
            JankHunterMode.INSPECT,
            JankHunterMode.COMPARE,
            JankHunterMode.PROBLEMS,
            JankHunterMode.SCORECARD,
        )
        val inspectLike = mode == JankHunterMode.INSPECT || mode == JankHunterMode.PROBLEMS
        val compareLike = mode == JankHunterMode.COMPARE || mode == JankHunterMode.SCORECARD

        rowMap["logs"]?.isVisible = inspectLike
        rowMap["baseline"]?.isVisible = compareLike
        rowMap["candidate"]?.isVisible = compareLike
        rowMap["output"]?.isVisible = mode != JankHunterMode.VERSION
        rowMap["options"]?.isVisible = mode != JankHunterMode.VERSION && mode != JankHunterMode.SAMPLE
        rowMap["artifacts"]?.isVisible = analysis

        listOf("ownerMap", "mapping", "classGraph", "diagnostics", "route", "screen", "owner", "class")
            .forEach { rowMap[it]?.isVisible = analysis }
        listOf("heapDump", "heapEvidence")
            .forEach { rowMap[it]?.isVisible = inspectLike }
        listOf("baselineHeapDump", "baselineHeapEvidence", "candidateHeapDump", "candidateHeapEvidence")
            .forEach { rowMap[it]?.isVisible = compareLike }

        rowMap["dataset"]?.isVisible = mode == JankHunterMode.PROBLEMS
        rowMap["format"]?.isVisible = mode == JankHunterMode.PROBLEMS
        jsonCheckBox.isVisible = mode == JankHunterMode.INSPECT || mode == JankHunterMode.COMPARE
        presentationCheckBox.isVisible = mode == JankHunterMode.INSPECT || mode == JankHunterMode.COMPARE
        openInIdeCheckBox.isVisible = mode == JankHunterMode.INSPECT || mode == JankHunterMode.COMPARE
        openExternalCheckBox.isVisible = mode == JankHunterMode.INSPECT || mode == JankHunterMode.COMPARE

        component.revalidate()
        component.repaint()
    }

    private fun runRequest(rawRequest: JankHunterRunRequest) {
        if (processHandler != null) {
            return
        }

        val command = try {
            JankHunterCommandBuilder.build(project, rawRequest)
        } catch (error: IllegalArgumentException) {
            Messages.showErrorDialog(project, error.message ?: "Невалидная команда Jank Hunter.", "Jank Hunter")
            return
        }
        val request = rawRequest.copy(
            cliPath = command.executable,
            output = command.outputPath.orEmpty(),
        )
        val validation = JankHunterRunValidator.validate(project, request, command)
        if (!validation.ok) {
            Messages.showErrorDialog(project, validation.errors.joinToString("\n"), "Jank Hunter: проверка не пройдена")
            return
        }
        outputField.text = command.outputPath.orEmpty()
        rememberSettings()
        addHistory(request, command)
        lastRunRequest = request
        startProcess(request, command, validation.warnings)
    }

    private fun collectRequest(): JankHunterRunRequest =
        JankHunterRunRequest(
            mode = selectedMode(),
            cliPath = cliPathField.text,
            logs = logsField.text,
            baseline = baselineField.text,
            candidate = candidateField.text,
            output = outputField.text,
            ownerMap = ownerMapField.text,
            mapping = mappingField.text,
            classGraph = classGraphField.text,
            diagnostics = diagnosticsField.text,
            heapDump = heapDumpField.text,
            heapEvidence = heapEvidenceField.text,
            baselineHeapDump = baselineHeapDumpField.text,
            baselineHeapEvidence = baselineHeapEvidenceField.text,
            candidateHeapDump = candidateHeapDumpField.text,
            candidateHeapEvidence = candidateHeapEvidenceField.text,
            route = routeField.text,
            screen = screenField.text,
            owner = ownerField.text,
            className = classField.text,
            dataset = datasetCombo.selectedItem?.toString().orEmpty(),
            format = formatCombo.selectedItem?.toString().orEmpty(),
            json = jsonCheckBox.isSelected,
            presentation = presentationCheckBox.isSelected,
        )

    private fun applyRequest(request: JankHunterRunRequest) {
        modeCombo.selectedItem = request.mode
        cliPathField.text = request.cliPath
        logsField.text = request.logs
        baselineField.text = request.baseline
        candidateField.text = request.candidate
        outputField.text = request.output
        ownerMapField.text = request.ownerMap
        mappingField.text = request.mapping
        classGraphField.text = request.classGraph
        diagnosticsField.text = request.diagnostics
        heapDumpField.text = request.heapDump
        heapEvidenceField.text = request.heapEvidence
        baselineHeapDumpField.text = request.baselineHeapDump
        baselineHeapEvidenceField.text = request.baselineHeapEvidence
        candidateHeapDumpField.text = request.candidateHeapDump
        candidateHeapEvidenceField.text = request.candidateHeapEvidence
        routeField.text = request.route
        screenField.text = request.screen
        ownerField.text = request.owner
        classField.text = request.className
        datasetCombo.selectedItem = request.dataset.ifBlank { "code-problems" }
        formatCombo.selectedItem = request.format.ifBlank { "csv" }
        jsonCheckBox.isSelected = request.json
        presentationCheckBox.isSelected = request.presentation
        updateModeVisibility()
    }

    private fun rememberSettings() {
        val state = JankHunterSettings.getInstance().state
        state.cliPath = cliPathField.text.trim()
        state.openReportInIde = openInIdeCheckBox.isSelected
        state.openReportExternally = openExternalCheckBox.isSelected
        state.presentationMode = presentationCheckBox.isSelected
    }

    private fun startProcess(request: JankHunterRunRequest, command: JankHunterCommand, warnings: List<String>) {
        val workDirectory = project.basePath?.let(::File)
        command.outputPath?.let { File(it).parentFile?.mkdirs() }
        consoleArea.text = ""
        appendConsole("$ ${command.displayText()}\n\n")
        warnings.forEach { warning -> appendConsole("warning: $warning\n") }
        if (warnings.isNotEmpty()) {
            appendConsole("\n")
        }
        setRunning(true)

        val commandLine = GeneralCommandLine(command.executable)
            .withParameters(command.args)
            .withCharset(StandardCharsets.UTF_8)
        if (workDirectory != null) {
            commandLine.withWorkDirectory(workDirectory)
        }

        val handler = try {
            OSProcessHandler(commandLine)
        } catch (error: ExecutionException) {
            setRunning(false)
            appendConsole("Не удалось запустить Jank Hunter: ${error.message}\n")
            return
        }

        processHandler = handler
        handler.addProcessListener(
            object : ProcessListener {
                override fun onTextAvailable(event: ProcessEvent, outputType: com.intellij.openapi.util.Key<*>) {
                    appendConsole(event.text)
                }

                override fun processTerminated(event: ProcessEvent) {
                    ApplicationManager.getApplication().invokeLater {
                        processHandler = null
                        setRunning(false)
                        appendConsole("\nПроцесс завершился с кодом ${event.exitCode}\n")
                        if (event.exitCode == 0) {
                            onProcessSucceeded(request, command.outputPath)
                        } else if (request.mode == JankHunterMode.SCORECARD) {
                            JankHunterNotifications.scorecardFailed(
                                project,
                                "Scorecard завершился с кодом ${event.exitCode}.",
                                openOutput = { command.outputPath?.let { openOutputInsideIde(File(it)) } },
                                rerun = { runRequest(request) },
                            )
                        }
                    }
                }
            },
        )
        handler.startNotify()
    }

    private fun stopCommand() {
        processHandler?.destroyProcess()
    }

    private fun setRunning(running: Boolean) {
        runButton.isEnabled = !running
        stopButton.isEnabled = running
        modeCombo.isEnabled = !running
        presetCombo.isEnabled = !running
    }

    private fun onProcessSucceeded(request: JankHunterRunRequest, outputPath: String?) {
        if (outputPath == null) {
            lastOutputPath = null
            updateOpenButtons()
            return
        }

        lastOutputPath = outputPath
        if (outputPath.endsWith(".html", ignoreCase = true)) {
            lastHtmlOutputPath = outputPath
        }
        updateOpenButtons()
        val problems = loadProblemsTable(outputPath)

        if (openInIdeCheckBox.isSelected) {
            openOutputInsideIde(File(outputPath))
        }
        if (openExternalCheckBox.isSelected && outputPath.endsWith(".html", ignoreCase = true)) {
            openOutputInBrowser(File(outputPath))
        }
        JankHunterNotifications.reportReady(
            project,
            outputPath,
            problems?.rows?.size,
            openReport = { openOutputInsideIde(File(outputPath)) },
            openProblems = { tabs.selectedIndex = 2 },
            rerun = { runRequest(request) },
        )
    }

    private fun updateOpenButtons() {
        val file = lastOutputPath?.let(::File)
        val exists = file?.isFile == true
        openInIdeButton.isEnabled = exists
        openBrowserButton.isEnabled = exists && file.extension.equals("html", ignoreCase = true)
    }

    private fun openLastOutputInsideIde() {
        val file = lastOutputPath?.let(::File) ?: return
        openOutputInsideIde(file)
    }

    private fun openLastOutputInBrowser() {
        val file = lastOutputPath?.let(::File) ?: return
        openOutputInBrowser(file)
    }

    private fun openOutputInsideIde(file: File) {
        if (!file.isFile) {
            appendConsole("Файл результата пока недоступен: ${file.path}\n")
            return
        }

        if (file.extension.equals("html", ignoreCase = true) && browser != null) {
            browser.loadURL(file.toURI().toString())
            tabs.selectedIndex = 1
            return
        }

        val virtualFile = LocalFileSystem.getInstance().refreshAndFindFileByIoFile(file)
        if (virtualFile != null) {
            FileEditorManager.getInstance(project).openFile(virtualFile, true)
        }
    }

    private fun openOutputInBrowser(file: File) {
        if (file.isFile) {
            BrowserUtil.browse(file.toURI())
        }
    }

    private fun refreshArtifacts(autoApplyBlankFields: Boolean, showMessage: Boolean) {
        artifactSets.clear()
        artifactSets += JankHunterArtifactDiscovery.findArtifactSets(project)

        val model = DefaultComboBoxModel<String>()
        artifactSets.forEach { set ->
            model.addElement(
                buildString {
                    append(set.displayName())
                    append("  ")
                    append(
                        listOfNotNull(
                            "owner-map".takeIf { set.ownerMap.isNotBlank() },
                            "mapping".takeIf { set.mapping.isNotBlank() },
                            "class-graph".takeIf { set.classGraph.isNotBlank() },
                            "diagnostics".takeIf { set.diagnostics.isNotBlank() },
                        ).joinToString(", "),
                    )
                },
            )
        }
        if (model.size == 0) {
            model.addElement("Артефакты не найдены")
        }
        artifactCombo.model = model
        artifactCombo.selectedIndex = 0

        if (artifactSets.isNotEmpty() && autoApplyBlankFields) {
            applySelectedArtifacts(force = false)
        }
        if (showMessage) {
            val message = if (artifactSets.isEmpty()) {
                "Не нашел owner-map/class-graph/diagnostics/mapping в проекте."
            } else {
                "Найдено наборов артефактов: ${artifactSets.size}."
            }
            Messages.showInfoMessage(project, message, "Jank Hunter")
        }
    }

    private fun applySelectedArtifacts(force: Boolean) {
        val set = artifactSets.getOrNull(artifactCombo.selectedIndex) ?: return
        setIfAllowed(ownerMapField, set.ownerMap, force)
        setIfAllowed(mappingField, set.mapping, force)
        setIfAllowed(classGraphField, set.classGraph, force)
        setIfAllowed(diagnosticsField, set.diagnostics, force)
    }

    private fun setIfAllowed(field: TextFieldWithBrowseButton, value: String, force: Boolean) {
        if (value.isBlank()) return
        if (force || field.text.isBlank()) {
            field.text = value
        }
    }

    private fun fillRecentLogs() {
        val logs = JankHunterArtifactDiscovery.findRecentLogs(project)
        if (logs.isEmpty()) {
            Messages.showInfoMessage(project, "Не нашел .jhlog файлов внутри проекта.", "Jank Hunter")
            return
        }
        val joined = logs.joinToString(", ")
        if (selectedMode() == JankHunterMode.COMPARE || selectedMode() == JankHunterMode.SCORECARD) {
            if (baselineField.text.isBlank()) baselineField.text = joined else candidateField.text = joined
        } else {
            logsField.text = joined
        }
    }

    private fun applySelectedPreset() {
        when (presetCombo.selectedItem as? JankHunterPreset ?: JankHunterPreset.CUSTOM) {
            JankHunterPreset.CUSTOM -> Unit
            JankHunterPreset.FAST_INSPECT -> {
                modeCombo.selectedItem = JankHunterMode.INSPECT
                jsonCheckBox.isSelected = false
                presentationCheckBox.isSelected = false
                openInIdeCheckBox.isSelected = true
                openExternalCheckBox.isSelected = false
                heapDumpField.text = ""
                heapEvidenceField.text = ""
                outputField.text = ""
            }
            JankHunterPreset.INSPECT_WITH_HEAP -> {
                modeCombo.selectedItem = JankHunterMode.INSPECT
                jsonCheckBox.isSelected = false
                presentationCheckBox.isSelected = true
                openInIdeCheckBox.isSelected = true
                outputField.text = ""
            }
            JankHunterPreset.COMPARE_WITH_HEAP -> {
                modeCombo.selectedItem = JankHunterMode.COMPARE
                jsonCheckBox.isSelected = false
                presentationCheckBox.isSelected = true
                openInIdeCheckBox.isSelected = true
                outputField.text = ""
            }
            JankHunterPreset.PROBLEMS_CSV -> {
                modeCombo.selectedItem = JankHunterMode.PROBLEMS
                datasetCombo.selectedItem = "code-problems"
                formatCombo.selectedItem = "csv"
                openInIdeCheckBox.isSelected = false
                openExternalCheckBox.isSelected = false
                outputField.text = ""
            }
            JankHunterPreset.CI_SCORECARD -> {
                modeCombo.selectedItem = JankHunterMode.SCORECARD
                openInIdeCheckBox.isSelected = false
                openExternalCheckBox.isSelected = false
                outputField.text = ""
            }
        }
        updateModeVisibility()
    }

    private fun loadSelectedProfile() {
        val name = profileCombo.selectedItem?.toString().orEmpty()
        val profile = profileStore.load().profiles[name] ?: return
        applyRequest(profile.toRequest(cliPathField.text))
    }

    private fun saveSelectedProfile() {
        val name = profileCombo.selectedItem?.toString()?.takeIf(String::isNotBlank) ?: "debug"
        profileStore.saveProfile(name, collectRequest())
        configureProfileCombo()
        profileCombo.selectedItem = name
        Messages.showInfoMessage(project, "Профиль '$name' сохранен в .jankhunter/plugin.json.", "Jank Hunter")
    }

    private fun checkCliStatus() {
        val status = JankHunterCliLifecycle.status(project, cliPathField.text)
        cliPathField.text = status.cliPath
        val message = buildString {
            append("CLI: ${status.cliPath}\n")
            append("exists=${status.exists} executable=${status.executable}\n")
            if (status.versionOutput.isNotBlank()) append(status.versionOutput.trim())
            if (status.stale) append("\nВерсия CLI старее ожидаемой для плагина.")
        }
        Messages.showInfoMessage(project, message, "Jank Hunter CLI")
    }

    private fun buildCli() {
        consoleArea.text = ""
        appendConsole("$ make build\n\n")
        JankHunterCliLifecycle.buildCli(
            project,
            onText = ::appendConsole,
            onDone = { ok ->
                ApplicationManager.getApplication().invokeLater {
                    if (ok) {
                        cliPathField.text = JankHunterCommandBuilder.defaultCliPath(project)
                        Messages.showInfoMessage(project, "CLI собран: ${cliPathField.text}", "Jank Hunter")
                    } else {
                        JankHunterNotifications.error(project, "Jank Hunter CLI", "Не удалось собрать CLI.")
                    }
                }
            },
        )
    }

    private fun runGradleTask(task: String, afterSuccess: (() -> Unit)? = null) {
        consoleArea.text = ""
        appendConsole("$ ./gradlew $task\n\n")
        JankHunterGradleIntegration.runAndroidTask(
            project,
            task,
            onText = ::appendConsole,
            onDone = { ok ->
                ApplicationManager.getApplication().invokeLater {
                    appendConsole("\nGradle task $task finished: $ok\n")
                    if (ok) afterSuccess?.invoke() else JankHunterNotifications.error(project, "Gradle failed", task)
                }
            },
        )
    }

    private fun buildArtifactsThenInspect() {
        runGradleTask(JankHunterGradleIntegration.collectArtifactsTask()) {
            refreshArtifacts(autoApplyBlankFields = true, showMessage = false)
            fillRecentLogs()
            if (logsField.text.isNotBlank()) {
                modeCombo.selectedItem = JankHunterMode.INSPECT
                runRequest(collectRequest())
            } else {
                Messages.showInfoMessage(project, "Артефакты собраны, но .jhlog для inspect не найдены.", "Jank Hunter")
            }
        }
    }

    private fun refreshDevices() {
        val devices = JankHunterAdbIntegration.listDevices(project)
        val model = DefaultComboBoxModel<JankHunterDevice>()
        devices.forEach(model::addElement)
        deviceCombo.model = model
        if (devices.isEmpty()) {
            Messages.showInfoMessage(project, "ADB devices не найдены.", "Jank Hunter")
        }
    }

    private fun syncRemotePathFromPackage() {
        val pkg = packageField.text.trim()
        if (pkg.isNotBlank()) {
            remoteLogsField.text = "/sdcard/Android/data/$pkg/files/jankhunter"
        }
    }

    private fun pullDeviceLogs(openInspect: Boolean) {
        syncRemotePathFromPackage()
        val device = deviceCombo.selectedItem as? JankHunterDevice
        val localDir = File(project.basePath ?: ".", "build/jankhunter/device-logs")
        consoleArea.text = ""
        appendConsole("$ adb pull ${remoteLogsField.text} ${localDir.path}\n\n")
        JankHunterAdbIntegration.pullLogs(
            project,
            device?.serial.orEmpty(),
            remoteLogsField.text,
            localDir,
            onText = ::appendConsole,
            onDone = { ok, files ->
                ApplicationManager.getApplication().invokeLater {
                    appendConsole("\nADB pull finished: $ok, logs=${files.size}\n")
                    if (files.isNotEmpty()) {
                        logsField.text = files.joinToString(", ") { it.path }
                    }
                    if (ok && openInspect && files.isNotEmpty()) {
                        modeCombo.selectedItem = JankHunterMode.INSPECT
                        runRequest(collectRequest())
                    }
                }
            },
        )
    }

    private fun openRemoteLogFolder() {
        syncRemotePathFromPackage()
        val device = deviceCombo.selectedItem as? JankHunterDevice
        val listing = JankHunterAdbIntegration.listRemoteLogs(project, device?.serial.orEmpty(), remoteLogsField.text)
        consoleArea.text = listing.ifBlank { "Remote folder is empty or unavailable.\n" }
        tabs.selectedIndex = 0
    }

    private fun addHistory(request: JankHunterRunRequest, command: JankHunterCommand) {
        val state = JankHunterSettings.getInstance().state
        val timestamp = LocalDateTime.now().format(DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm:ss"))
        val entry = RecentRun.fromRequest(timestamp, command.displayText(), request)
        state.recentRuns.removeAll { it.commandLine == entry.commandLine }
        state.recentRuns.add(0, entry)
        while (state.recentRuns.size > MAX_HISTORY) {
            state.recentRuns.removeAt(state.recentRuns.lastIndex)
        }
        refreshHistoryTable()
    }

    private fun refreshHistoryTable() {
        historyModel.rowCount = 0
        JankHunterSettings.getInstance().state.recentRuns.forEach { run ->
            historyModel.addRow(arrayOf(run.timestamp, run.mode, run.output, run.commandLine))
        }
    }

    private fun selectedHistoryEntry(): RecentRun? {
        val viewRow = historyTable.selectedRow
        if (viewRow < 0) return null
        val modelRow = historyTable.convertRowIndexToModel(viewRow)
        return JankHunterSettings.getInstance().state.recentRuns.getOrNull(modelRow)
    }

    private fun loadSelectedHistory() {
        val entry = selectedHistoryEntry() ?: return
        applyRequest(entry.toRequest())
        lastOutputPath = entry.output.takeIf(String::isNotBlank)
        updateOpenButtons()
    }

    private fun rerunSelectedHistory() {
        val entry = selectedHistoryEntry() ?: return
        applyRequest(entry.toRequest())
        runRequest(entry.toRequest())
    }

    private fun openSelectedHistoryOutput() {
        val output = selectedHistoryEntry()?.output?.takeIf(String::isNotBlank) ?: return
        openOutputInsideIde(File(output))
    }

    private fun loadProblemsTable(outputPath: String): ProblemsTable? {
        val file = File(outputPath)
        if (!file.isFile || file.extension.lowercase() !in setOf("csv", "json")) {
            return null
        }
        val table = runCatching { ProblemsParser.parse(file) }
            .onFailure { error -> appendConsole("Не удалось разобрать problems-файл: ${error.message}\n") }
            .getOrNull()
            ?: return null
        if (!table.isEmpty) {
            rawProblemsTable = table
            applyProblemsView()
            tabs.selectedIndex = 2
        }
        return table
    }

    private fun openSelectedProblemSource() {
        val viewRow = problemsTable.selectedRow
        if (viewRow < 0) return
        val row = problemsModel.rowAt(problemsTable.convertRowIndexToModel(viewRow))
        if (!SourceNavigator.open(project, row)) {
            Messages.showInfoMessage(project, "Не удалось найти исходник для выбранной строки.", "Jank Hunter")
        }
    }

    private fun openProblemReport() {
        val report = lastHtmlOutputPath?.let(::File)
            ?: lastOutputPath?.let { latestHtmlNear(File(it)) }
        if (report?.isFile == true) {
            openOutputInsideIde(report)
        } else {
            Messages.showInfoMessage(project, "HTML-отчет для выбранной строки пока неизвестен.", "Jank Hunter")
        }
    }

    private fun copySelectedRecommendation() {
        val viewRow = problemsTable.selectedRow
        if (viewRow < 0) return
        val row = problemsModel.rowAt(problemsTable.convertRowIndexToModel(viewRow))
        val text = row["recommendation"]
            ?: row["Recommendation"]
            ?: row["fix_examples"]
            ?: row["verification_steps"]
            ?: row["evidence"]
            ?: ""
        if (text.isBlank()) return
        CopyPasteManager.getInstance().setContents(StringSelection(text))
    }

    private fun applyProblemsView() {
        val grouped = groupedProblems(rawProblemsTable, groupProblemsCombo.selectedItem?.toString().orEmpty())
        problemsModel.setTable(grouped)
        problemsSorter.model = problemsModel
        problemsSorter.rowFilter = object : RowFilter<ProblemsTableModel, Int>() {
            override fun include(entry: Entry<out ProblemsTableModel, out Int>): Boolean {
                val row = problemsModel.rowAt(entry.identifier)
                return matchesProblemsFilters(row)
            }
        }
    }

    private fun resetProblemsFilters() {
        severityFilter.selectedItem = "all"
        categoryFilter.text = ""
        screenFilter.text = ""
        ownerFilter.text = ""
        groupProblemsCombo.selectedItem = "none"
        applyProblemsView()
    }

    private fun groupedProblems(table: ProblemsTable, group: String): ProblemsTable {
        if (table.isEmpty || group == "none") return table
        val keyName = if (group == "owner") "owner" else "class"
        val groups = table.rows.groupBy { row ->
            row[keyName]
                ?: row[keyName.replaceFirstChar { it.titlecase() }]
                ?: row["from"]
                ?: row["holder"]
                ?: ""
        }
        val rows = groups.map { (key, rows) ->
            mapOf(
                keyName to key.ifBlank { "(empty)" },
                "count" to rows.size.toString(),
                "max_severity" to rows.maxByOrNull { severityRank(it["severity"].orEmpty()) }?.get("severity").orEmpty(),
                "score" to rows.mapNotNull { it["score"]?.toDoubleOrNull() }.maxOrNull()?.toString().orEmpty(),
                "recommendation" to rows.firstNotNullOfOrNull { it["recommendation"]?.takeIf(String::isNotBlank) }.orEmpty(),
            )
        }
        return ProblemsTable(listOf(keyName, "count", "max_severity", "score", "recommendation"), rows)
    }

    private fun matchesProblemsFilters(row: Map<String, String>): Boolean {
        val severity = severityFilter.selectedItem?.toString().orEmpty()
        if (severity != "all" && !row.values.any { it.equals(severity, ignoreCase = true) }) return false
        if (!containsAny(row, categoryFilter.text, "categories", "problems", "record_type")) return false
        if (!containsAny(row, screenFilter.text, "screen", "screens")) return false
        if (!containsAny(row, ownerFilter.text, "owner", "holder", "class", "from")) return false
        return true
    }

    private fun containsAny(row: Map<String, String>, needle: String, vararg keys: String): Boolean {
        val trimmed = needle.trim()
        if (trimmed.isBlank()) return true
        return keys.any { key -> row[key]?.contains(trimmed, ignoreCase = true) == true }
    }

    private fun latestHtmlNear(file: File): File? {
        val dir = file.parentFile ?: return null
        return dir.listFiles { candidate -> candidate.isFile && candidate.extension.equals("html", ignoreCase = true) }
            ?.maxByOrNull(File::lastModified)
    }

    fun applyClassFilter(className: String) {
        modeCombo.selectedItem = JankHunterMode.INSPECT
        classField.text = className
        updateModeVisibility()
        tabs.selectedIndex = 0
    }

    private fun severityRank(value: String): Int =
        when (value.lowercase()) {
            "critical" -> 5
            "high" -> 4
            "medium" -> 3
            "low" -> 2
            "info" -> 1
            else -> 0
        }

    private fun appendConsole(text: String) {
        if (SwingUtilities.isEventDispatchThread()) {
            consoleArea.append(text)
            consoleArea.caretPosition = consoleArea.document.length
        } else {
            ApplicationManager.getApplication().invokeLater {
                consoleArea.append(text)
                consoleArea.caretPosition = consoleArea.document.length
            }
        }
    }

    private fun selectedMode(): JankHunterMode = modeCombo.selectedItem as? JankHunterMode ?: JankHunterMode.INSPECT

    private fun browseField(appendSelection: Boolean = false): TextFieldWithBrowseButton {
        val field = TextFieldWithBrowseButton()
        field.addActionListener {
            val descriptor = FileChooserDescriptor(true, true, false, false, false, appendSelection)
            if (appendSelection) {
                val selected = FileChooser.chooseFiles(descriptor, project, null)
                if (selected.isEmpty()) return@addActionListener
                val value = selected.joinToString(", ") { it.path }
                field.text = if (field.text.isBlank()) value else field.text.trimEnd() + ", " + value
            } else {
                val selected = FileChooser.chooseFile(descriptor, project, null) ?: return@addActionListener
                field.text = selected.path
            }
        }
        return field
    }

    private fun configureTooltips() {
        presetCombo.toolTipText = hint("Готовые наборы настроек для частых сценариев запуска.")
        applyPresetButton.toolTipText = hint("Применить выбранный пресет к форме.")
        modeCombo.toolTipText = hint(selectedMode().hint)
        cliPathField.toolTipText = hint(
            "Путь к бинарнику jankhunter. Если оставить пустым, плагин попробует найти ../cli/bin/jankhunter или команду jankhunter в PATH.",
        )
        artifactCombo.toolTipText = hint("Найденные артефакты Android Gradle plugin, сгруппированные по variant.")
        scanArtifactsButton.toolTipText = hint("Просканировать проект и обновить список owner-map/class-graph/diagnostics/mapping.")
        applyArtifactsButton.toolTipText = hint("Заполнить поля артефактов выбранным набором.")
        logsField.toolTipText = hint(
            "Файлы .jhlog для inspect/problems. Можно выбрать несколько файлов, указать glob-маски или перечислить пути через запятую.",
        )
        baselineField.toolTipText = hint("Базовый прогон для compare/scorecard. Поддерживаются несколько файлов и glob-маски.")
        candidateField.toolTipText = hint("Кандидатный прогон для compare/scorecard. Обычно это логи после изменения.")
        outputField.toolTipText = hint("Куда записать результат. Если пусто, плагин создаст файл в build/jankhunter внутри проекта.")
        ownerMapField.toolTipText = hint("owner-map.json от Android Gradle plugin: раскрывает owner hash в class.method.")
        mappingField.toolTipText = hint("R8/ProGuard mapping.txt: раскрывает обфусцированные имена классов и методов.")
        classGraphField.toolTipText = hint("class-graph.jsonl: статические связи, горячие пути и method-level hotspots.")
        diagnosticsField.toolTipText = hint("instrumentation-diagnostics.jsonl: отчет по ASM-инструментации.")
        heapDumpField.toolTipText = hint("HPROF для inspect/problems: CLI построит цепочки удержания от GC root.")
        heapEvidenceField.toolTipText = hint("Готовый JSON heap evidence вместо HPROF.")
        baselineHeapDumpField.toolTipText = hint("HPROF для базового прогона в compare/scorecard.")
        baselineHeapEvidenceField.toolTipText = hint("JSON heap evidence для базового прогона.")
        candidateHeapDumpField.toolTipText = hint("HPROF для кандидатного прогона в compare/scorecard.")
        candidateHeapEvidenceField.toolTipText = hint("JSON heap evidence для кандидатного прогона.")
        routeField.toolTipText = hint("Фильтр по части route, например /feed или /checkout.")
        screenField.toolTipText = hint("Фильтр по части имени экрана, например Feed или Checkout.")
        ownerField.toolTipText = hint("Фильтр по owner/source work, например FeedRepository.")
        classField.toolTipText = hint("Фильтр по части имени класса.")
        datasetCombo.toolTipText = hint("Датасет Problems: проблемы кода, утечки, граф влияния или математические findings.")
        formatCombo.toolTipText = hint("Формат Problems: csv для таблицы или json для дальнейшей обработки.")
        jsonCheckBox.toolTipText = hint("Добавить --json: основной результат команды будет напечатан в JSON.")
        presentationCheckBox.toolTipText = hint("Добавить --presentation: более крупные акценты и печатный CSS в HTML.")
        openInIdeCheckBox.toolTipText = hint("После успешного запуска открыть HTML-отчет во вкладке Report.")
        openExternalCheckBox.toolTipText = hint("После успешного запуска открыть HTML-отчет в браузере по умолчанию.")
        runButton.toolTipText = hint("Проверить поля и запустить Jank Hunter CLI.")
        stopButton.toolTipText = hint("Остановить текущий процесс Jank Hunter.")
        openInIdeButton.toolTipText = hint("Открыть последний созданный файл результата внутри IDE.")
        openBrowserButton.toolTipText = hint("Открыть последний созданный HTML-отчет в браузере.")
        clearButton.toolTipText = hint("Очистить консольный вывод.")
        findLogsButton.toolTipText = hint("Найти свежие .jhlog файлы в проекте и подставить их в текущий режим.")
        openProblemSourceButton.toolTipText = hint("Открыть исходник для выбранной строки problems-таблицы.")
        reloadProblemsButton.toolTipText = hint("Перечитать последний CSV/JSON результат в таблицу.")
        loadHistoryButton.toolTipText = hint("Загрузить выбранный запуск обратно в форму.")
        rerunHistoryButton.toolTipText = hint("Повторить выбранный запуск из истории.")
        openHistoryOutputButton.toolTipText = hint("Открыть файл результата выбранного запуска.")
        consoleArea.toolTipText = hint("Здесь отображаются команда, stdout, stderr и код завершения процесса.")
    }

    private fun hint(text: String): String = "<html>${text.replace("\n", "<br>")}</html>"

    private fun createBrowserQuery(browser: JBCefBrowser): JBCefJSQuery = JBCefJSQuery.create(browser as JBCefBrowserBase)

    private class SeverityBadgeRenderer : DefaultTableCellRenderer() {
        override fun getTableCellRendererComponent(
            table: javax.swing.JTable,
            value: Any?,
            isSelected: Boolean,
            hasFocus: Boolean,
            row: Int,
            column: Int,
        ): java.awt.Component {
            val component = super.getTableCellRendererComponent(table, value, isSelected, hasFocus, row, column)
            val modelColumn = table.convertColumnIndexToModel(column)
            val name = table.model.getColumnName(modelColumn)
            val textValue = value?.toString().orEmpty()
            if (!isSelected) {
                val color = when {
                    name.equals("severity", ignoreCase = true) || name.equals("max_severity", ignoreCase = true) -> {
                        when (textValue.lowercase()) {
                            "critical" -> JBColor(Color(0x8B0000), Color(0xFF6B6B))
                            "high" -> JBColor(Color(0xB54708), Color(0xF79009))
                            "medium" -> JBColor(Color(0x8A6D00), Color(0xD0A000))
                            "low" -> JBColor(Color(0x155EEF), Color(0x84ADFF))
                            else -> null
                        }
                    }
                    else -> null
                }
                foreground = color ?: table.foreground
                background = table.background
            }
            toolTipText = textValue
            return component
        }
    }

    companion object {
        private const val MAX_HISTORY = 25
    }
}
