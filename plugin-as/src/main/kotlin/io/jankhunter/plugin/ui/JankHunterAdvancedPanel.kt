package io.jankhunter.plugin.ui

import com.intellij.openapi.Disposable
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.fileChooser.FileChooser
import com.intellij.openapi.fileChooser.FileChooserDescriptor
import com.intellij.openapi.project.Project
import com.intellij.openapi.ui.TextFieldWithBrowseButton
import com.intellij.ui.components.JBCheckBox
import com.intellij.ui.components.JBLabel
import com.intellij.ui.components.JBScrollPane
import com.intellij.ui.components.JBTextArea
import com.intellij.ui.components.JBTextField
import io.jankhunter.plugin.execution.JankHunterArtifactDiscovery
import io.jankhunter.plugin.execution.JankHunterArtifactSet
import io.jankhunter.plugin.settings.JankHunterSettings
import java.awt.BorderLayout
import java.awt.Dimension
import java.awt.FlowLayout
import java.awt.GridBagConstraints
import java.awt.GridBagLayout
import java.awt.Insets
import java.awt.LayoutManager
import java.awt.Rectangle
import java.util.concurrent.Future
import javax.swing.BorderFactory
import javax.swing.DefaultComboBoxModel
import javax.swing.JButton
import javax.swing.JCheckBox
import javax.swing.JComboBox
import javax.swing.JComponent
import javax.swing.JPanel
import javax.swing.JScrollPane
import javax.swing.Scrollable
import javax.swing.SwingConstants

internal data class JankHunterAdvancedOptions(
    val ownerMap: String,
    val mapping: String,
    val classGraph: String,
    val diagnostics: String,
    val diCatalog: String,
    val route: String,
    val screen: String,
    val owner: String,
    val className: String,
    val reportStyle: String,
    val presentation: Boolean,
) {
    companion object {
        val SIMPLE = JankHunterAdvancedOptions(
            ownerMap = "",
            mapping = "",
            classGraph = "",
            diagnostics = "",
            diCatalog = "",
            route = "",
            screen = "",
            owner = "",
            className = "",
            reportStyle = "modern",
            presentation = false,
        )
    }
}

internal class JankHunterAdvancedPanel(
    private val project: Project,
    onBack: () -> Unit,
    onRun: () -> Unit,
    onScorecard: () -> Unit,
    onSelectReport: () -> Unit = {},
    onSelectCompare: () -> Unit = {},
    onChooseReportDirectory: () -> Unit = {},
    onChooseBaselineDirectory: () -> Unit = {},
    onChooseCandidateDirectory: () -> Unit = {},
    private val artifactFinder: (Project) -> List<JankHunterArtifactSet> = JankHunterArtifactDiscovery::findArtifactSets,
) : Disposable {
    val component: JComponent

    private val contextLabel = JBLabel()
    private val artifactCombo = JComboBox<String>()
    private val scanArtifactsButton = JButton("Найти")
    private val manualArtifactsCheckBox = JBCheckBox("Настроить отдельные файлы вручную")
    private val ownerMapField = fileField()
    private val mappingField = fileField()
    private val classGraphField = fileField()
    private val diagnosticsField = fileField()
    private val diCatalogField = fileField()
    private val routeField = JBTextField()
    private val screenField = JBTextField()
    private val ownerField = JBTextField()
    private val classField = JBTextField()
    private val reportStyleCombo = JComboBox(arrayOf("modern", "legacy"))
    private val presentationCheckBox = JBCheckBox("Presentation mode")
    private val cliPathField = fileField()
    private val consoleArea = JBTextArea()
    private val commandArea = JBTextArea("Команда будет показана перед запуском")
    private val scorecardButton = JButton("Оценить качество сравнения")
    private val reportTaskButton = JButton("Отчёт")
    private val compareTaskButton = JButton("Сравнение")
    private val reportDirectoryButton = JButton("Выбрать папку логов…")
    private val baselineDirectoryButton = JButton("Выбрать baseline…")
    private val candidateDirectoryButton = JButton("Выбрать candidate…")
    private val directoryButtonsPanel = JPanel(FlowLayout(FlowLayout.LEFT, 7, 0))
    private val runButton = JButton("Сгенерировать отчёт")
    private val artifactSets = mutableListOf<JankHunterArtifactSet>()
    private val manualFields: List<JComponent>
    private var scanTask: Future<*>? = null
    private var artifactScanRequested = false
    private var disposed = false

    init {
        val settings = JankHunterSettings.getInstance().state
        cliPathField.text = settings.cliPath
        reportStyleCombo.selectedItem = settings.reportStyle.ifBlank { "modern" }
        presentationCheckBox.isSelected = settings.presentationMode

        reportTaskButton.addActionListener { onSelectReport() }
        compareTaskButton.addActionListener { onSelectCompare() }
        reportDirectoryButton.addActionListener { onChooseReportDirectory() }
        baselineDirectoryButton.addActionListener { onChooseBaselineDirectory() }
        candidateDirectoryButton.addActionListener { onChooseCandidateDirectory() }
        directoryButtonsPanel.add(reportDirectoryButton)
        directoryButtonsPanel.add(baselineDirectoryButton)
        directoryButtonsPanel.add(candidateDirectoryButton)
        baselineDirectoryButton.isVisible = false
        candidateDirectoryButton.isVisible = false

        consoleArea.isEditable = false
        consoleArea.lineWrap = true
        consoleArea.wrapStyleWord = false
        consoleArea.rows = 8
        commandArea.isEditable = false
        commandArea.lineWrap = true
        commandArea.wrapStyleWord = true
        commandArea.rows = 3

        val artifactsForm = formPanel()
        var artifactsRow = 0
        addRow(artifactsForm, artifactsRow++, "Найденный набор", JPanel(BorderLayout(7, 0)).apply {
            add(artifactCombo, BorderLayout.CENTER)
            add(scanArtifactsButton, BorderLayout.EAST)
        })
        addWideRow(artifactsForm, artifactsRow++, manualArtifactsCheckBox)
        addRow(artifactsForm, artifactsRow++, "Owner map", ownerMapField)
        addRow(artifactsForm, artifactsRow++, "R8 mapping", mappingField)
        addRow(artifactsForm, artifactsRow++, "Class graph", classGraphField)
        addRow(artifactsForm, artifactsRow++, "Diagnostics", diagnosticsField)
        addRow(artifactsForm, artifactsRow, "DI catalog", diCatalogField)
        manualFields = listOf(ownerMapField, mappingField, classGraphField, diagnosticsField, diCatalogField)

        manualArtifactsCheckBox.addActionListener { updateManualFields() }
        artifactCombo.addActionListener { applySelectedArtifactSet() }
        scanArtifactsButton.addActionListener { scanArtifacts() }
        updateManualFields()

        val filtersForm = formPanel()
        addRow(filtersForm, 0, "Route", routeField)
        addRow(filtersForm, 1, "Screen", screenField)
        addRow(filtersForm, 2, "Owner", ownerField)
        addRow(filtersForm, 3, "Class", classField)

        val appearanceForm = formPanel()
        addRow(appearanceForm, 0, "Стиль отчёта", reportStyleCombo)
        addWideRow(appearanceForm, 1, presentationCheckBox)

        val diagnosticsForm = formPanel()
        addRow(diagnosticsForm, 0, "CLI", cliPathField)
        addRow(diagnosticsForm, 1, "Команда", textAreaScrollPane(commandArea))
        addRow(diagnosticsForm, 2, "Вывод", textAreaScrollPane(consoleArea))

        val content = VerticalScrollablePanel().apply {
            layout = javax.swing.BoxLayout(this, javax.swing.BoxLayout.Y_AXIS)
            border = BorderFactory.createEmptyBorder(0, 0, 8, 0)
            add(contextPanel(onBack))
            add(group("Артефакты и деобфускация", artifactsForm))
            add(group("Фильтры анализа", filtersForm))
            add(group("Вид отчёта", appearanceForm))
            add(group("Диагностика запуска", diagnosticsForm))
        }

        component = JPanel(BorderLayout(0, 8)).apply {
            border = BorderFactory.createEmptyBorder(10, 10, 10, 10)
            add(JBScrollPane(content).apply {
                horizontalScrollBarPolicy = JScrollPane.HORIZONTAL_SCROLLBAR_NEVER
                verticalScrollBarPolicy = JScrollPane.VERTICAL_SCROLLBAR_AS_NEEDED
                border = null
            }, BorderLayout.CENTER)
            add(
                JPanel(BorderLayout()).apply {
                    add(
                        JPanel(FlowLayout(FlowLayout.LEFT, 7, 0)).apply {
                            add(scorecardButton.apply {
                                toolTipText = "Проверить качество baseline/candidate и готовность сравнения к использованию."
                                addActionListener { onScorecard() }
                            })
                        },
                        BorderLayout.WEST,
                    )
                    add(runButton.apply { addActionListener { onRun() } }, BorderLayout.EAST)
                },
                BorderLayout.SOUTH,
            )
        }

        initializeArtifacts()
    }

    fun ensureArtifactsLoaded() {
        if (artifactScanRequested) return
        artifactScanRequested = true
        scanArtifacts()
    }

    fun options(): JankHunterAdvancedOptions = JankHunterAdvancedOptions(
        ownerMap = ownerMapField.text.trim(),
        mapping = mappingField.text.trim(),
        classGraph = classGraphField.text.trim(),
        diagnostics = diagnosticsField.text.trim(),
        diCatalog = diCatalogField.text.trim(),
        route = routeField.text.trim(),
        screen = screenField.text.trim(),
        owner = ownerField.text.trim(),
        className = classField.text.trim(),
        reportStyle = reportStyleCombo.selectedItem?.toString().orEmpty().ifBlank { "modern" },
        presentation = presentationCheckBox.isSelected,
    )

    fun cliPath(): String = cliPathField.text.trim()

    fun setCliPath(path: String) {
        cliPathField.text = path
    }

    fun setContext(text: String, compare: Boolean) {
        contextLabel.text = text
        scorecardButton.isEnabled = compare
        reportTaskButton.isEnabled = compare
        compareTaskButton.isEnabled = !compare
        reportDirectoryButton.isVisible = !compare
        baselineDirectoryButton.isVisible = compare
        candidateDirectoryButton.isVisible = compare
        runButton.text = if (compare) "Сравнить отчёты" else "Сгенерировать отчёт"
        directoryButtonsPanel.revalidate()
        directoryButtonsPanel.repaint()
    }

    fun setClassFilter(className: String) {
        classField.text = className
    }

    fun setCommandPreview(text: String) {
        commandArea.text = text
        commandArea.caretPosition = 0
    }

    fun replaceConsole(text: String) {
        consoleArea.text = text
        consoleArea.caretPosition = consoleArea.document.length
    }

    fun appendConsole(text: String) {
        consoleArea.append(text)
        val excess = consoleArea.document.length - MAX_CONSOLE_CHARS
        if (excess > 0) runCatching { consoleArea.document.remove(0, excess) }
        consoleArea.caretPosition = consoleArea.document.length
    }

    override fun dispose() {
        if (disposed) return
        disposed = true
        scanTask?.cancel(true)
        artifactSets.clear()
    }

    private fun contextPanel(onBack: () -> Unit): JComponent = PreferredHeightPanel(BorderLayout(8, 0)).apply {
        alignmentX = ComponentAlignment.LEFT
        border = BorderFactory.createTitledBorder("Текущий выбор")
        add(
            JPanel(BorderLayout(0, 5)).apply {
                add(contextLabel, BorderLayout.NORTH)
                add(
                    JPanel(FlowLayout(FlowLayout.LEFT, 7, 0)).apply {
                        add(reportTaskButton)
                        add(compareTaskButton)
                    },
                    BorderLayout.CENTER,
                )
                add(directoryButtonsPanel, BorderLayout.SOUTH)
            },
            BorderLayout.CENTER,
        )
        add(JButton("← Простой режим").apply { addActionListener { onBack() } }, BorderLayout.EAST)
    }

    private fun initializeArtifacts() {
        artifactCombo.model = DefaultComboBoxModel(arrayOf("Нажмите «Найти» для поиска"))
    }

    private fun scanArtifacts() {
        scanTask?.cancel(true)
        scanArtifactsButton.isEnabled = false
        artifactCombo.model = DefaultComboBoxModel(arrayOf("Сканирование…"))
        scanTask = ApplicationManager.getApplication().executeOnPooledThread {
            val result = runCatching { artifactFinder(project) }
            ApplicationManager.getApplication().invokeLater {
                if (disposed || project.isDisposed) return@invokeLater
                scanArtifactsButton.isEnabled = true
                val sets = result.getOrDefault(emptyList())
                artifactSets.clear()
                artifactSets += sets
                artifactCombo.model = DefaultComboBoxModel(
                    if (sets.isEmpty()) arrayOf("Артефакты не найдены") else sets.map(JankHunterArtifactSet::displayName).toTypedArray(),
                )
                if (sets.isNotEmpty()) {
                    artifactCombo.selectedIndex = 0
                    applySelectedArtifactSet()
                }
            }
        }
    }

    private fun applySelectedArtifactSet() {
        if (manualArtifactsCheckBox.isSelected) return
        val set = artifactSets.getOrNull(artifactCombo.selectedIndex) ?: return
        ownerMapField.text = set.ownerMap
        mappingField.text = set.mapping
        classGraphField.text = set.classGraph
        diagnosticsField.text = set.diagnostics
        diCatalogField.text = set.diCatalog
    }

    private fun updateManualFields() {
        val manual = manualArtifactsCheckBox.isSelected
        manualFields.forEach { it.isEnabled = manual }
        if (!manual) applySelectedArtifactSet()
    }

    private fun fileField(): TextFieldWithBrowseButton = TextFieldWithBrowseButton().apply {
        addActionListener {
            val descriptor = FileChooserDescriptor(true, true, false, false, false, false)
            FileChooser.chooseFile(descriptor, project, null)?.let { selected -> text = selected.path }
        }
    }

    private fun group(title: String, content: JComponent): JComponent = PreferredHeightPanel(BorderLayout()).apply {
        alignmentX = ComponentAlignment.LEFT
        border = BorderFactory.createCompoundBorder(
            BorderFactory.createTitledBorder(title),
            BorderFactory.createEmptyBorder(4, 6, 7, 6),
        )
        add(content, BorderLayout.CENTER)
    }

    private fun formPanel(): JPanel = JPanel(GridBagLayout()).apply { alignmentX = ComponentAlignment.LEFT }

    private fun textAreaScrollPane(textArea: JBTextArea): JBScrollPane = JBScrollPane(textArea).apply {
        horizontalScrollBarPolicy = JScrollPane.HORIZONTAL_SCROLLBAR_NEVER
        verticalScrollBarPolicy = JScrollPane.VERTICAL_SCROLLBAR_AS_NEEDED
        minimumSize = Dimension(0, preferredSize.height)
    }

    private fun addRow(panel: JPanel, row: Int, label: String, component: JComponent) {
        panel.add(
            JBLabel(label),
            GridBagConstraints().apply {
                gridx = 0
                gridy = row
                anchor = GridBagConstraints.WEST
                insets = Insets(4, 0, 4, 10)
            },
        )
        panel.add(
            component,
            GridBagConstraints().apply {
                gridx = 1
                gridy = row
                weightx = 1.0
                fill = GridBagConstraints.HORIZONTAL
                insets = Insets(4, 0, 4, 0)
            },
        )
    }

    private fun addWideRow(panel: JPanel, row: Int, component: JComponent) {
        panel.add(
            component,
            GridBagConstraints().apply {
                gridx = 0
                gridy = row
                gridwidth = 2
                weightx = 1.0
                fill = GridBagConstraints.HORIZONTAL
                insets = Insets(4, 0, 4, 0)
            },
        )
    }

    private object ComponentAlignment {
        const val LEFT = 0.0f
    }

    private class PreferredHeightPanel(layout: LayoutManager) : JPanel(layout) {
        override fun getMaximumSize(): Dimension = Dimension(Int.MAX_VALUE, preferredSize.height)
    }

    private class VerticalScrollablePanel : JPanel(), Scrollable {
        override fun getPreferredScrollableViewportSize(): Dimension = preferredSize

        override fun getScrollableUnitIncrement(visibleRect: Rectangle, orientation: Int, direction: Int): Int = 16

        override fun getScrollableBlockIncrement(visibleRect: Rectangle, orientation: Int, direction: Int): Int =
            if (orientation == SwingConstants.VERTICAL) visibleRect.height.coerceAtLeast(16) else visibleRect.width.coerceAtLeast(16)

        override fun getScrollableTracksViewportWidth(): Boolean = true

        override fun getScrollableTracksViewportHeight(): Boolean = false
    }

    companion object {
        private const val MAX_CONSOLE_CHARS = 500_000
    }
}
