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
import io.jankhunter.plugin.execution.JankHunterLogScope
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
import io.jankhunter.plugin.services.JankHunterCliStatus
import io.jankhunter.plugin.services.JankHunterCliLifecycle
import io.jankhunter.plugin.services.JankHunterDevice
import io.jankhunter.plugin.services.JankHunterNotifications
import io.jankhunter.plugin.services.JankHunterProjectService
import io.jankhunter.plugin.services.JankHunterProjectIntrospection
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
import javax.swing.ButtonGroup
import javax.swing.JButton
import javax.swing.JCheckBox
import javax.swing.JComboBox
import javax.swing.JComponent
import javax.swing.JPanel
import javax.swing.JRadioButton
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
    private val saveDefaultsButton = JButton("Save Defaults")
    private val modeCombo = JComboBox(JankHunterMode.entries.toTypedArray())
    private val modeButtons = JankHunterMode.entries.associateWith { mode -> JRadioButton(mode.label) }
    private val targetProjectLabel = JBLabel()
    private val cliPathField = browseField()
    private val cliStatusLabel = JBLabel()
    private val checkCliButton = JButton("Check CLI")
    private val buildCliButton = JButton("Build CLI")
    private val logsDirectoryField = browseDirectoryField()
    private val openLogsButton = JButton("Open Logs")
    private val generateReportButton = JButton("Generate")
    private val artifactCombo = JComboBox<String>()
    private val applyArtifactsButton = JButton("Apply")
    private val scanArtifactsButton = JButton("Scan")
    private val logsField = browseField(appendSelection = true)
    private val inspectLogScopeCombo = JComboBox(JankHunterLogScope.entries.toTypedArray())
    private val baselineField = browseField(appendSelection = true)
    private val baselineLogScopeCombo = JComboBox(JankHunterLogScope.entries.toTypedArray())
    private val candidateField = browseField(appendSelection = true)
    private val candidateLogScopeCombo = JComboBox(JankHunterLogScope.entries.toTypedArray())
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
    private val deviceCombo = JComboBox<JankHunterDevice>()
    private val scanDevicesButton = JButton("Devices")
    private val packageField = JBTextField()
    private val remoteLogsField = JBTextField("/sdcard/Android/data/io.jankhunter.sample/files/jankhunter")
    private val pullLogsButton = JButton("Pull From App")
    private val openRemoteLogsButton = JButton("List App Logs")
    private val collectInspectButton = JButton("Pull + Generate")
    private val consoleArea = JBTextArea()
    private val tabs = JTabbedPane()
    private val reportPlaceholder = JBLabel("HTML-отчет появится здесь после успешного запуска.")
    private var browser: JBCefBrowser? = null
    private var browserQuery: JBCefJSQuery? = null
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
    private val visibleHistory = mutableListOf<RecentRun>()

    private val artifactSets = mutableListOf<JankHunterArtifactSet>()
    private var processHandler: OSProcessHandler? = null
    private var lastOutputPath: String? = null
    private var lastHtmlOutputPath: String? = null
    private var lastRunRequest: JankHunterRunRequest? = null
    @Volatile
    private var disposed = false

    init {
        val settings = JankHunterSettings.getInstance().state
        cliPathField.text = settings.cliPath.ifBlank { JankHunterCommandBuilder.defaultCliPath(project) }
        logsDirectoryField.text = settings.logsDirectory.ifBlank {
            JankHunterProjectIntrospection.defaultLogsDirectory(project).path
        }
        packageField.text = settings.packageName
        if (settings.remoteLogsPath.isNotBlank()) {
            remoteLogsField.text = settings.remoteLogsPath
        } else if (packageField.text.isNotBlank()) {
            syncRemotePathFromPackage()
        }
        targetProjectLabel.text = ":  /  ${project.basePath.orEmpty()}"
        presentationCheckBox.isSelected = settings.presentationMode
        openInIdeCheckBox.isSelected = settings.openReportInIde
        openExternalCheckBox.isSelected = settings.openReportExternally
        inspectLogScopeCombo.selectedItem = JankHunterLogScope.ALL_SELECTED
        baselineLogScopeCombo.selectedItem = JankHunterLogScope.ALL_SELECTED
        candidateLogScopeCombo.selectedItem = JankHunterLogScope.ALL_SELECTED
        modeButtons[JankHunterMode.INSPECT]?.isSelected = true

        consoleArea.isEditable = false
        consoleArea.lineWrap = true
        consoleArea.wrapStyleWord = false

        configureProfileCombo()
        configureProblemsTable()
        configureHistoryTable()

        tabs.addTab("Console", JBScrollPane(consoleArea))
        tabs.addTab("Report", reportPlaceholder)
        tabs.addTab("Problems", buildProblemsPanel())
        tabs.addTab("History", buildHistoryPanel())

        configureTooltips()
        configureActions()

        stopButton.isEnabled = false
        openInIdeButton.isEnabled = false
        openBrowserButton.isEnabled = false

        val root = JPanel(BorderLayout())
        root.add(buildHeaderPanel(), BorderLayout.NORTH)
        val splitPane = JSplitPane(JSplitPane.VERTICAL_SPLIT, buildForm(), tabs).apply {
            resizeWeight = 0.34
            isContinuousLayout = true
        }
        root.add(splitPane, BorderLayout.CENTER)

        component = root
        restoreStartupRequest()
        JankHunterProjectService.getInstance(project).register(this)
        initializeArtifactPlaceholder()
        initializeCliStatusPlaceholder()
        refreshTargetProjectAsync()
        refreshHistoryTable()
        updateModeVisibility()
    }

    override fun dispose() {
        disposed = true
        JankHunterProjectService.getInstance(project).unregister(this)
        processHandler?.destroyProcess()
        browserQuery?.dispose()
        browser?.dispose()
    }

    private fun configureActions() {
        runButton.addActionListener { runRequest(collectRequest()) }
        generateReportButton.addActionListener { generateReportFromLogs() }
        stopButton.addActionListener { stopCommand() }
        openInIdeButton.addActionListener { openLastOutputInsideIde() }
        openBrowserButton.addActionListener { openLastOutputInBrowser() }
        clearButton.addActionListener { consoleArea.text = "" }
        findLogsButton.addActionListener { fillRecentLogs() }
        openLogsButton.addActionListener { chooseLogsDirectory() }
        applyPresetButton.addActionListener { applySelectedPreset() }
        loadProfileButton.addActionListener { loadSelectedProfile() }
        saveProfileButton.addActionListener { saveSelectedProfile() }
        saveDefaultsButton.addActionListener { saveProjectDefaults() }
        checkCliButton.addActionListener { refreshCliStatusAsync(showDialog = true) }
        buildCliButton.addActionListener { buildCli() }
        scanArtifactsButton.addActionListener { refreshArtifactsAsync(autoApplyBlankFields = false, showMessage = true) }
        applyArtifactsButton.addActionListener { applySelectedArtifacts(force = true) }
        scanDevicesButton.addActionListener { refreshDevices() }
        packageField.addActionListener { syncRemotePathFromPackage() }
        pullLogsButton.addActionListener { pullDeviceLogs(openInspect = false) }
        openRemoteLogsButton.addActionListener { openRemoteLogFolder() }
        collectInspectButton.addActionListener { pullDeviceLogs(openInspect = true) }
        modeCombo.addActionListener {
            syncModeButtons()
            outputField.text = ""
            updateModeVisibility()
        }
        modeButtons.forEach { (mode, button) ->
            button.addActionListener {
                modeCombo.selectedItem = mode
                outputField.text = ""
                updateModeVisibility()
            }
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

    private fun configureBrowserBridge(cefBrowser: JBCefBrowser, query: JBCefJSQuery) {
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
            isOpaque = false
            add(generateReportButton)
            add(runButton)
            add(stopButton)
            add(openInIdeButton)
            add(openBrowserButton)
            add(clearButton)
        }

    private fun buildHeaderPanel(): JComponent =
        JPanel(BorderLayout()).apply {
            background = JBColor(Color(0x111827), Color(0x0B0F14))
            border = javax.swing.BorderFactory.createEmptyBorder(8, 10, 6, 10)
            add(
                JPanel(BorderLayout()).apply {
                    isOpaque = false
                    add(
                        JBLabel("JANK HUNTER // APP LOG OPS").apply {
                            foreground = JBColor(Color(0x0F766E), Color(0x6EE7B7))
                            font = font.deriveFont(java.awt.Font.BOLD, 14f)
                        },
                        BorderLayout.WEST,
                    )
                    add(
                        JBLabel("pull -> inspect -> report").apply {
                            foreground = JBColor(Color(0x334155), Color(0x94A3B8))
                            font = font.deriveFont(11f)
                        },
                        BorderLayout.EAST,
                    )
                },
                BorderLayout.NORTH,
            )
            add(buildToolbar(), BorderLayout.SOUTH)
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
        rowMap.clear()
        return JTabbedPane().apply {
            addTab("Run", buildRunPanel())
            addTab("Logs", buildLogsPanel())
            addTab("Artifacts", buildArtifactInputsPanel())
            addTab("Filters", buildAdvancedInputsPanel())
        }
    }

    private fun buildRunPanel(): JComponent {
        val form = JPanel(GridBagLayout())
        var row = 0
        addRow(form, row++, "Target", targetProjectLabel, "target")
        addRow(form, row++, "Package", packageField, "package")
        addWideRow(form, row++, buildModePanel(), "mode")
        addRow(form, row++, "CLI", buildCliPanel(), "cli")
        addRow(form, row++, "Logs", buildLocalLogsPanel(), "logsDirectory")
        addRow(form, row++, "Output", outputField, "output")
        addWideRow(form, row++, buildOptionsPanel(), "options")
        addFormFiller(form, row)
        return form
    }

    private fun buildLogsPanel(): JComponent {
        val form = JPanel(GridBagLayout())
        var row = 0
        addRow(form, row++, "Logs / globs", logsField, "logs")
        addRow(form, row++, "Inspect scope", inspectLogScopeCombo, "inspectLogScope")
        addRow(form, row++, "Baseline", baselineField, "baseline")
        addRow(form, row++, "Baseline scope", baselineLogScopeCombo, "baselineLogScope")
        addRow(form, row++, "Candidate", candidateField, "candidate")
        addRow(form, row++, "Candidate scope", candidateLogScopeCombo, "candidateLogScope")
        addWideRow(form, row++, buildAdbPanel(), "adb")
        addFormFiller(form, row)
        return form
    }

    private fun buildArtifactInputsPanel(): JComponent {
        val form = JPanel(GridBagLayout())
        var row = 0
        addRow(form, row++, "Artifacts", buildArtifactsPanel(), "artifacts")
        addRow(form, row++, "Owner map", ownerMapField, "ownerMap")
        addRow(form, row++, "Mapping", mappingField, "mapping")
        addRow(form, row++, "Class graph", classGraphField, "classGraph")
        addRow(form, row++, "Diagnostics", diagnosticsField, "diagnostics")
        addFormFiller(form, row)
        return form
    }

    private fun buildAdvancedInputsPanel(): JComponent {
        val form = JPanel(GridBagLayout())
        var row = 0
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
        addFormFiller(form, row)
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
                    add(saveDefaultsButton)
                },
                BorderLayout.EAST,
            )
        }

    private fun buildModePanel(): JComponent =
        JPanel(FlowLayout(FlowLayout.LEFT, 8, 0)).apply {
            val group = ButtonGroup()
            listOf(JankHunterMode.INSPECT, JankHunterMode.COMPARE, JankHunterMode.PROBLEMS).forEach { mode ->
                val button = modeButtons.getValue(mode)
                group.add(button)
                add(button)
            }
        }

    private fun buildCliPanel(): JComponent =
        JPanel(BorderLayout(8, 0)).apply {
            add(cliPathField, BorderLayout.CENTER)
            add(
                JPanel(FlowLayout(FlowLayout.RIGHT, 4, 0)).apply {
                    add(cliStatusLabel)
                    add(checkCliButton)
                    add(buildCliButton)
                },
                BorderLayout.EAST,
            )
        }

    private fun buildLocalLogsPanel(): JComponent =
        JPanel(BorderLayout(8, 0)).apply {
            add(logsDirectoryField, BorderLayout.CENTER)
            add(
                JPanel(FlowLayout(FlowLayout.RIGHT, 4, 0)).apply {
                    add(openLogsButton)
                    add(pullLogsButton)
                    add(collectInspectButton)
                    add(findLogsButton)
                },
                BorderLayout.EAST,
            )
        }

    private fun buildAdbPanel(): JComponent =
        JPanel(BorderLayout(8, 0)).apply {
            add(
                JPanel(FlowLayout(FlowLayout.LEFT, 8, 0)).apply {
                    add(deviceCombo)
                    add(scanDevicesButton)
                },
                BorderLayout.CENTER,
            )
            add(
                JPanel(FlowLayout(FlowLayout.RIGHT, 4, 0)).apply {
                    add(openRemoteLogsButton)
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

    private fun addFormFiller(form: JPanel, row: Int) {
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
        rowMap["inspectLogScope"]?.isVisible = mode == JankHunterMode.INSPECT
        rowMap["baseline"]?.isVisible = compareLike
        rowMap["baselineLogScope"]?.isVisible = compareLike
        rowMap["candidate"]?.isVisible = compareLike
        rowMap["candidateLogScope"]?.isVisible = compareLike
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
            inspectLogScope = selectedInspectLogScope(),
            baseline = baselineField.text,
            baselineLogScope = selectedBaselineLogScope(),
            candidate = candidateField.text,
            candidateLogScope = selectedCandidateLogScope(),
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
        selectMode(request.mode)
        cliPathField.text = request.cliPath
        logsField.text = request.logs
        inspectLogScopeCombo.selectedItem = request.inspectLogScope
        baselineField.text = request.baseline
        baselineLogScopeCombo.selectedItem = request.baselineLogScope
        candidateField.text = request.candidate
        candidateLogScopeCombo.selectedItem = request.candidateLogScope
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

    private fun restoreStartupRequest() {
        val request = profileStore.loadDefaults(cliPathField.text)
            ?: lastRunForCurrentProject()?.toRequest()
            ?: return
        applyRequest(
            request.copy(
                cliPath = request.cliPath.ifBlank { cliPathField.text },
            ),
        )
        lastOutputPath = request.output.takeIf(String::isNotBlank)
        lastHtmlOutputPath = request.output.takeIf { it.endsWith(".html", ignoreCase = true) }
        updateOpenButtons()
    }

    private fun lastRunForCurrentProject(): RecentRun? {
        val projectPath = currentProjectPath()
        if (projectPath.isBlank()) return null
        val state = JankHunterSettings.getInstance().state
        return state.lastRun?.takeIf { it.projectPath == projectPath }
            ?: state.recentRuns.firstOrNull { it.projectPath == projectPath }
    }

    private fun rememberSettings() {
        val state = JankHunterSettings.getInstance().state
        state.cliPath = cliPathField.text.trim()
        state.logsDirectory = logsDirectoryField.text.trim()
        state.packageName = packageField.text.trim()
        state.remoteLogsPath = remoteLogsField.text.trim()
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
        generateReportButton.isEnabled = !running
        stopButton.isEnabled = running
        modeCombo.isEnabled = !running
        modeButtons.values.forEach { it.isEnabled = !running }
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

        if (file.extension.equals("html", ignoreCase = true)) {
            val reportBrowser = ensureReportBrowser()
            if (reportBrowser != null) {
                reportBrowser.loadURL(file.toURI().toString())
                tabs.selectedIndex = 1
                return
            }
        }

        val virtualFile = LocalFileSystem.getInstance().refreshAndFindFileByIoFile(file)
        if (virtualFile != null) {
            FileEditorManager.getInstance(project).openFile(virtualFile, true)
        }
    }

    private fun ensureReportBrowser(): JBCefBrowser? {
        browser?.let { return it }
        if (!enableBrowser || !JBCefApp.isSupported()) return null
        val created = JBCefBrowser()
        val query = createBrowserQuery(created)
        browser = created
        browserQuery = query
        configureBrowserBridge(created, query)
        val reportIndex = tabs.indexOfTab("Report")
        if (reportIndex >= 0) {
            tabs.setComponentAt(reportIndex, created.component)
            tabs.selectedIndex = 1
        }
        return created
    }

    private fun openOutputInBrowser(file: File) {
        if (file.isFile) {
            BrowserUtil.browse(file.toURI())
        }
    }

    private fun initializeArtifactPlaceholder() {
        artifactSets.clear()
        artifactCombo.model = DefaultComboBoxModel(arrayOf("Нажмите Scan для поиска артефактов"))
        artifactCombo.selectedIndex = 0
    }

    private fun refreshArtifactsAsync(autoApplyBlankFields: Boolean, showMessage: Boolean) {
        scanArtifactsButton.isEnabled = false
        artifactCombo.model = DefaultComboBoxModel(arrayOf("Сканирую проект..."))
        ApplicationManager.getApplication().executeOnPooledThread {
            val result = runCatching { JankHunterArtifactDiscovery.findArtifactSets(project) }
            ApplicationManager.getApplication().invokeLater {
                if (disposed || project.isDisposed) return@invokeLater
                scanArtifactsButton.isEnabled = true
                val sets = result.getOrElse { error ->
                    if (showMessage) {
                        JankHunterNotifications.error(
                            project,
                            "Jank Hunter",
                            "Не удалось просканировать артефакты: ${error.message.orEmpty()}",
                        )
                    }
                    emptyList()
                }
                applyArtifactSets(sets, autoApplyBlankFields, showMessage && result.isSuccess)
            }
        }
    }

    private fun applyArtifactSets(
        sets: List<JankHunterArtifactSet>,
        autoApplyBlankFields: Boolean,
        showMessage: Boolean,
    ) {
        artifactSets.clear()
        artifactSets += sets

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

    private fun chooseLogsDirectory() {
        val selected = FileChooser.chooseFile(directoryDescriptor(), project, null) ?: return
        logsDirectoryField.text = selected.path
        fillLogsFromDirectory(showMessage = true)
    }

    private fun generateReportFromLogs() {
        if (logsField.text.isBlank()) {
            val files = fillLogsFromDirectory(showMessage = false)
            if (files.isEmpty()) {
                chooseLogsDirectory()
            }
        }
        if (logsField.text.isBlank()) {
            Messages.showInfoMessage(project, "Укажите папку или файлы .jhlog перед генерацией отчета.", "Jank Hunter")
            return
        }
        if (selectedMode() != JankHunterMode.COMPARE && selectedMode() != JankHunterMode.SCORECARD) {
            selectMode(JankHunterMode.INSPECT)
        }
        runRequest(collectRequest())
    }

    private fun fillLogsFromDirectory(showMessage: Boolean): List<File> {
        val dir = logsDirectory()
        if (dir == null || !dir.isDirectory) {
            if (showMessage) {
                Messages.showInfoMessage(project, "Выберите папку с .jhlog файлами.", "Jank Hunter")
            }
            return emptyList()
        }

        val logs = dir.walkTopDown()
            .filter { file -> file.isFile && file.extension.equals("jhlog", ignoreCase = true) }
            .sortedByDescending(File::lastModified)
            .take(50)
            .toList()
        if (logs.isNotEmpty()) {
            logsField.text = applyLogScope(logs, selectedInspectLogScope()).joinToString(", ") { it.path }
        }

        val heapDump = dir.walkTopDown()
            .firstOrNull { file -> file.isFile && file.extension.equals("hprof", ignoreCase = true) }
        if (heapDump != null && heapDumpField.text.isBlank()) {
            heapDumpField.text = heapDump.path
        }

        if (showMessage) {
            val message = if (logs.isEmpty()) {
                "В выбранной папке нет .jhlog файлов."
            } else {
                "Подставлено .jhlog файлов: ${applyLogScope(logs, selectedInspectLogScope()).size} из ${logs.size}."
            }
            Messages.showInfoMessage(project, message, "Jank Hunter")
        }
        return logs
    }

    private fun ensureLogsDirectoryForPull(): File? {
        val existing = logsDirectory()
        if (existing != null) {
            existing.mkdirs()
            return existing
        }
        val selected = FileChooser.chooseFile(directoryDescriptor(), project, null) ?: return null
        logsDirectoryField.text = selected.path
        val dir = File(selected.path)
        dir.mkdirs()
        return dir
    }

    private fun logsDirectory(): File? =
        logsDirectoryField.text.trim().takeIf(String::isNotEmpty)?.let(::File)

    private fun fillRecentLogs() {
        val logs = JankHunterArtifactDiscovery.findRecentLogs(project)
        if (logs.isEmpty()) {
            Messages.showInfoMessage(project, "Не нашел .jhlog файлов внутри проекта.", "Jank Hunter")
            return
        }
        if (selectedMode() == JankHunterMode.COMPARE || selectedMode() == JankHunterMode.SCORECARD) {
            val files = logs.map(::File)
            if (baselineField.text.isBlank()) {
                baselineField.text = applyLogScope(files, selectedBaselineLogScope()).joinToString(", ") { it.path }
            } else {
                candidateField.text = applyLogScope(files, selectedCandidateLogScope()).joinToString(", ") { it.path }
            }
        } else {
            val joined = applyLogScope(logs.map(::File), selectedInspectLogScope()).joinToString(", ") { it.path }
            logsField.text = joined
        }
    }

    private fun applySelectedPreset() {
        when (presetCombo.selectedItem as? JankHunterPreset ?: JankHunterPreset.CUSTOM) {
            JankHunterPreset.CUSTOM -> Unit
            JankHunterPreset.FAST_INSPECT -> {
                selectMode(JankHunterMode.INSPECT)
                inspectLogScopeCombo.selectedItem = JankHunterLogScope.ALL_SELECTED
                jsonCheckBox.isSelected = false
                presentationCheckBox.isSelected = false
                openInIdeCheckBox.isSelected = true
                openExternalCheckBox.isSelected = false
                heapDumpField.text = ""
                heapEvidenceField.text = ""
                outputField.text = ""
            }
            JankHunterPreset.INSPECT_WITH_HEAP -> {
                selectMode(JankHunterMode.INSPECT)
                inspectLogScopeCombo.selectedItem = JankHunterLogScope.ALL_SELECTED
                jsonCheckBox.isSelected = false
                presentationCheckBox.isSelected = true
                openInIdeCheckBox.isSelected = true
                outputField.text = ""
            }
            JankHunterPreset.COMPARE_WITH_HEAP -> {
                selectMode(JankHunterMode.COMPARE)
                baselineLogScopeCombo.selectedItem = JankHunterLogScope.ALL_SELECTED
                candidateLogScopeCombo.selectedItem = JankHunterLogScope.ALL_SELECTED
                jsonCheckBox.isSelected = false
                presentationCheckBox.isSelected = true
                openInIdeCheckBox.isSelected = true
                outputField.text = ""
            }
            JankHunterPreset.PROBLEMS_CSV -> {
                selectMode(JankHunterMode.PROBLEMS)
                datasetCombo.selectedItem = "code-problems"
                formatCombo.selectedItem = "csv"
                openInIdeCheckBox.isSelected = false
                openExternalCheckBox.isSelected = false
                outputField.text = ""
            }
            JankHunterPreset.CI_SCORECARD -> {
                selectMode(JankHunterMode.SCORECARD)
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
        rememberSettings()
        profileStore.saveProfile(name, collectRequest())
        configureProfileCombo()
        profileCombo.selectedItem = name
        Messages.showInfoMessage(project, "Профиль '$name' сохранен в .jankhunter/plugin.json.", "Jank Hunter")
    }

    private fun saveProjectDefaults() {
        rememberSettings()
        profileStore.saveDefaults(collectRequest())
        Messages.showInfoMessage(project, "Defaults сохранены в .jankhunter/plugin.json.", "Jank Hunter")
    }

    private fun initializeCliStatusPlaceholder() {
        cliStatusLabel.text = "Not checked"
        cliStatusLabel.foreground = JBColor(Color(0x8A6D00), Color(0xD0A000))
    }

    private fun refreshTargetProjectAsync() {
        ApplicationManager.getApplication().executeOnPooledThread {
            val targetProject = JankHunterProjectIntrospection.detect(project)
            ApplicationManager.getApplication().invokeLater {
                if (disposed || project.isDisposed) return@invokeLater
                targetProjectLabel.text = buildString {
                    append(targetProject?.moduleName ?: ":")
                    targetProject?.packageName?.takeIf(String::isNotBlank)?.let { append("  /  $it") }
                    append("  /  ")
                    append(project.basePath.orEmpty())
                }
                if (packageField.text.isBlank()) {
                    packageField.text = targetProject?.packageName.orEmpty()
                    syncRemotePathFromPackage()
                }
            }
        }
    }

    private fun refreshCliStatusAsync(showDialog: Boolean) {
        val configuredPath = cliPathField.text
        cliStatusLabel.text = "Checking..."
        checkCliButton.isEnabled = false
        ApplicationManager.getApplication().executeOnPooledThread {
            val result = runCatching { JankHunterCliLifecycle.status(project, configuredPath) }
            ApplicationManager.getApplication().invokeLater {
                if (disposed || project.isDisposed) return@invokeLater
                checkCliButton.isEnabled = true
                val status = result.getOrElse { error ->
                    cliStatusLabel.text = "Failed"
                    cliStatusLabel.foreground = JBColor(Color(0x8A6D00), Color(0xD0A000))
                    if (showDialog) {
                        JankHunterNotifications.error(
                            project,
                            "Jank Hunter CLI",
                            "Не удалось проверить CLI: ${error.message.orEmpty()}",
                        )
                    }
                    return@invokeLater
                }
                applyCliStatus(status, showDialog)
            }
        }
    }

    private fun applyCliStatus(status: JankHunterCliStatus, showDialog: Boolean) {
        cliPathField.text = status.cliPath
        cliStatusLabel.text = when {
            status.exists && status.executable -> "OK"
            status.exists -> "Not executable"
            else -> "Not found"
        }
        cliStatusLabel.foreground = when {
            status.exists && status.executable -> JBColor(Color(0x237804), Color(0x73D13D))
            else -> JBColor(Color(0x8A6D00), Color(0xD0A000))
        }
        if (!showDialog) return
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
            remoteLogsField.text = "/data/data/$pkg/files/jankhunter"
        }
    }

    private fun pullDeviceLogs(openInspect: Boolean) {
        syncRemotePathFromPackage()
        val device = deviceCombo.selectedItem as? JankHunterDevice
        val packageName = packageField.text.trim()
        if (packageName.isBlank()) {
            Messages.showInfoMessage(project, "Не удалось определить package. Укажите applicationId вручную.", "Jank Hunter")
            return
        }
        val localDir = ensureLogsDirectoryForPull() ?: return
        consoleArea.text = ""
        appendConsole("$ adb exec-out run-as $packageName tar files/jankhunter -> ${localDir.path}\n\n")
        JankHunterAdbIntegration.pullAppPrivateLogs(
            project,
            device?.serial.orEmpty(),
            packageName,
            localDir,
            onText = ::appendConsole,
            onDone = { ok, files ->
                ApplicationManager.getApplication().invokeLater {
                    appendConsole("\nADB run-as pull finished: $ok, logs=${files.size}\n")
                    if (files.isNotEmpty()) {
                        logsField.text = applyLogScope(files, selectedInspectLogScope()).joinToString(", ") { it.path }
                    }
                    if (ok && openInspect && files.isNotEmpty()) {
                        selectMode(JankHunterMode.INSPECT)
                        runRequest(collectRequest())
                    }
                }
            },
        )
    }

    private fun openRemoteLogFolder() {
        syncRemotePathFromPackage()
        val device = deviceCombo.selectedItem as? JankHunterDevice
        val packageName = packageField.text.trim()
        if (packageName.isBlank()) {
            Messages.showInfoMessage(project, "Не удалось определить package. Укажите applicationId вручную.", "Jank Hunter")
            return
        }
        val listing = JankHunterAdbIntegration.listAppPrivateLogs(project, device?.serial.orEmpty(), packageName)
        consoleArea.text = listing.ifBlank { "App private log folder is empty or unavailable. Is the debug app installed?\n" }
        tabs.selectedIndex = 0
    }

    private fun addHistory(request: JankHunterRunRequest, command: JankHunterCommand) {
        val state = JankHunterSettings.getInstance().state
        val timestamp = LocalDateTime.now().format(DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm:ss"))
        val entry = RecentRun.fromRequest(timestamp, command.displayText(), request, currentProjectPath())
        state.lastRun = entry
        state.recentRuns.removeAll { it.commandLine == entry.commandLine && it.projectPath == entry.projectPath }
        state.recentRuns.add(0, entry)
        while (state.recentRuns.size > MAX_HISTORY) {
            state.recentRuns.removeAt(state.recentRuns.lastIndex)
        }
        refreshHistoryTable()
    }

    private fun refreshHistoryTable() {
        historyModel.rowCount = 0
        visibleHistory.clear()
        val projectPath = currentProjectPath()
        visibleHistory += JankHunterSettings.getInstance().state.recentRuns
            .filter { run -> projectPath.isBlank() || run.projectPath == projectPath }
        visibleHistory.forEach { run ->
            historyModel.addRow(arrayOf(run.timestamp, run.mode, run.output, run.commandLine))
        }
    }

    private fun selectedHistoryEntry(): RecentRun? {
        val viewRow = historyTable.selectedRow
        if (viewRow < 0) return null
        val modelRow = historyTable.convertRowIndexToModel(viewRow)
        return visibleHistory.getOrNull(modelRow)
    }

    private fun currentProjectPath(): String = project.basePath.orEmpty()

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
        selectMode(JankHunterMode.INSPECT)
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

    private fun selectMode(mode: JankHunterMode) {
        modeCombo.selectedItem = mode
        syncModeButtons()
        updateModeVisibility()
    }

    private fun syncModeButtons() {
        val mode = selectedMode()
        modeButtons[mode]?.isSelected = true
    }

    private fun selectedMode(): JankHunterMode = modeCombo.selectedItem as? JankHunterMode ?: JankHunterMode.INSPECT

    private fun selectedInspectLogScope(): JankHunterLogScope =
        inspectLogScopeCombo.selectedItem as? JankHunterLogScope ?: JankHunterLogScope.ALL_SELECTED

    private fun selectedBaselineLogScope(): JankHunterLogScope =
        baselineLogScopeCombo.selectedItem as? JankHunterLogScope ?: JankHunterLogScope.ALL_SELECTED

    private fun selectedCandidateLogScope(): JankHunterLogScope =
        candidateLogScopeCombo.selectedItem as? JankHunterLogScope ?: JankHunterLogScope.ALL_SELECTED

    private fun applyLogScope(logs: List<File>, scope: JankHunterLogScope): List<File> {
        if (logs.isEmpty()) return emptyList()
        return when (scope) {
            JankHunterLogScope.ALL_SELECTED -> logs
            JankHunterLogScope.LATEST_LOG -> logs.maxByOrNull(File::lastModified)?.let(::listOf).orEmpty()
            JankHunterLogScope.LATEST_SESSION_GROUP -> latestSessionGroup(logs)
        }
    }

    private fun latestSessionGroup(logs: List<File>): List<File> {
        val latestByGroup = linkedMapOf<String, Long>()
        val sessionByFile = linkedMapOf<File, SessionLogPath>()
        logs.forEach { file ->
            val session = parseSessionLogPath(file) ?: return@forEach
            sessionByFile[file] = session
            latestByGroup[session.group] = maxOf(latestByGroup[session.group] ?: Long.MIN_VALUE, session.startMs)
        }
        if (sessionByFile.isEmpty()) return logs
        return logs.filter { file ->
            val session = sessionByFile[file]
            session == null || latestByGroup[session.group] == session.startMs
        }
    }

    private fun parseSessionLogPath(file: File): SessionLogPath? {
        if (!file.name.endsWith(".jhlog") || !file.name.startsWith("session-")) return null
        val name = file.name.removeSuffix(".jhlog")
        val parts = name.split('-')
        if (parts.size < 4) return null
        val startMs = parts[parts.size - 2].toLongOrNull() ?: return null
        parts.last().toLongOrNull() ?: return null
        val process = parts.subList(1, parts.size - 2).joinToString("-").takeIf { it.isNotBlank() } ?: return null
        return SessionLogPath(
            group = File(file.parentFile ?: File("."), "session-$process").path,
            startMs = startMs,
        )
    }

    private data class SessionLogPath(
        val group: String,
        val startMs: Long,
    )

    private fun browseDirectoryField(): TextFieldWithBrowseButton {
        val field = TextFieldWithBrowseButton()
        field.addActionListener {
            val selected = FileChooser.chooseFile(directoryDescriptor(), project, null) ?: return@addActionListener
            field.text = selected.path
        }
        return field
    }

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

    private fun directoryDescriptor(): FileChooserDescriptor =
        FileChooserDescriptor(false, true, false, false, false, false)

    private fun configureTooltips() {
        presetCombo.toolTipText = hint("Готовые наборы настроек для частых сценариев запуска.")
        applyPresetButton.toolTipText = hint("Применить выбранный пресет к форме.")
        profileCombo.toolTipText = hint("Именованные профили из .jankhunter/plugin.json.")
        loadProfileButton.toolTipText = hint("Загрузить выбранный именованный профиль в форму.")
        saveProfileButton.toolTipText = hint("Сохранить текущую форму как выбранный именованный профиль.")
        saveDefaultsButton.toolTipText = hint("Сохранить текущую форму как defaults проекта в .jankhunter/plugin.json.")
        modeCombo.toolTipText = hint(selectedMode().hint)
        cliPathField.toolTipText = hint(
            "Путь к бинарнику jankhunter. Если оставить пустым, плагин попробует найти ../cli/bin/jankhunter или команду jankhunter в PATH.",
        )
        cliStatusLabel.toolTipText = hint("Статус CLI. Проверка запускается вручную кнопкой Check CLI.")
        logsDirectoryField.toolTipText = hint("Локальная папка, где лежат или куда будут выгружены .jhlog файлы.")
        openLogsButton.toolTipText = hint("Выбрать папку с логами и подставить найденные .jhlog в текущий запуск.")
        generateReportButton.toolTipText = hint("Собрать inspect-отчет из выбранной папки или списка .jhlog файлов.")
        artifactCombo.toolTipText = hint("Найденные артефакты Android Gradle plugin, сгруппированные по variant.")
        scanArtifactsButton.toolTipText = hint("Просканировать проект и обновить список owner-map/class-graph/diagnostics/mapping.")
        applyArtifactsButton.toolTipText = hint("Заполнить поля артефактов выбранным набором.")
        logsField.toolTipText = hint(
            "Файлы .jhlog для inspect/problems. Можно выбрать несколько файлов, указать glob-маски или перечислить пути через запятую.",
        )
        inspectLogScopeCombo.toolTipText = hint(
            "Как собрать inspect: один самый новый лог, последняя session-группа или все выбранные логи с --all-sessions.",
        )
        baselineField.toolTipText = hint("Базовый прогон для compare/scorecard. Поддерживаются несколько файлов и glob-маски.")
        baselineLogScopeCombo.toolTipText = hint(
            "Какие baseline-логи отправить в compare/scorecard: один самый новый, последнюю session-группу или все выбранные.",
        )
        candidateField.toolTipText = hint("Кандидатный прогон для compare/scorecard. Обычно это логи после изменения.")
        candidateLogScopeCombo.toolTipText = hint(
            "Какие candidate-логи отправить в compare/scorecard: один самый новый, последнюю session-группу или все выбранные.",
        )
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
