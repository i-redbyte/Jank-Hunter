package io.jankhunter.plugin.run

import com.intellij.openapi.options.SettingsEditor
import com.intellij.openapi.project.Project
import com.intellij.openapi.ui.TextFieldWithBrowseButton
import com.intellij.ui.components.JBCheckBox
import com.intellij.ui.components.JBTextField
import io.jankhunter.plugin.execution.JankHunterLogScope
import io.jankhunter.plugin.execution.JankHunterMode
import java.awt.GridBagConstraints
import java.awt.GridBagLayout
import java.awt.Insets
import javax.swing.JComboBox
import javax.swing.JComponent
import javax.swing.JLabel
import javax.swing.JPanel

class JankHunterRunSettingsEditor(private val project: Project) : SettingsEditor<JankHunterRunConfiguration>() {
    private val panel = JPanel(GridBagLayout())
    private val mode = JComboBox(JankHunterMode.entries.toTypedArray())
    private val cliPath = TextFieldWithBrowseButton()
    private val logs = JBTextField()
    private val inspectLogScope = JComboBox(JankHunterLogScope.entries.toTypedArray())
    private val baseline = JBTextField()
    private val baselineLogScope = JComboBox(JankHunterLogScope.entries.toTypedArray())
    private val candidate = JBTextField()
    private val candidateLogScope = JComboBox(JankHunterLogScope.entries.toTypedArray())
    private val output = JBTextField()
    private val ownerMap = JBTextField()
    private val mapping = JBTextField()
    private val classGraph = JBTextField()
    private val diagnostics = JBTextField()
    private val heapDump = JBTextField()
    private val heapEvidence = JBTextField()
    private val route = JBTextField()
    private val screen = JBTextField()
    private val owner = JBTextField()
    private val className = JBTextField()
    private val dataset = JComboBox(arrayOf("code-problems", "leaks", "influence", "math-findings"))
    private val format = JComboBox(arrayOf("csv", "json"))
    private val json = JBCheckBox("JSON")
    private val presentation = JBCheckBox("Presentation")

    init {
        var row = 0
        add(row++, "Mode", mode)
        add(row++, "CLI", cliPath)
        add(row++, "Logs", logs)
        add(row++, "Inspect scope", inspectLogScope)
        add(row++, "Baseline", baseline)
        add(row++, "Baseline scope", baselineLogScope)
        add(row++, "Candidate", candidate)
        add(row++, "Candidate scope", candidateLogScope)
        add(row++, "Output", output)
        add(row++, "Owner map", ownerMap)
        add(row++, "Mapping", mapping)
        add(row++, "Class graph", classGraph)
        add(row++, "Diagnostics", diagnostics)
        add(row++, "Heap dump", heapDump)
        add(row++, "Heap evidence", heapEvidence)
        add(row++, "Route", route)
        add(row++, "Screen", screen)
        add(row++, "Owner", owner)
        add(row++, "Class", className)
        add(row++, "Dataset", dataset)
        add(row++, "Format", format)
        add(row++, "", json)
        add(row++, "", presentation)
    }

    override fun resetEditorFrom(configuration: JankHunterRunConfiguration) {
        mode.selectedItem = configuration.mode
        cliPath.text = configuration.cliPath
        logs.text = configuration.logs
        inspectLogScope.selectedItem = configuration.inspectLogScope
        baseline.text = configuration.baseline
        baselineLogScope.selectedItem = configuration.baselineLogScope
        candidate.text = configuration.candidate
        candidateLogScope.selectedItem = configuration.candidateLogScope
        output.text = configuration.output
        ownerMap.text = configuration.ownerMap
        mapping.text = configuration.mapping
        classGraph.text = configuration.classGraph
        diagnostics.text = configuration.diagnostics
        heapDump.text = configuration.heapDump
        heapEvidence.text = configuration.heapEvidence
        route.text = configuration.route
        screen.text = configuration.screen
        owner.text = configuration.owner
        className.text = configuration.className
        dataset.selectedItem = configuration.dataset
        format.selectedItem = configuration.format
        json.isSelected = configuration.json
        presentation.isSelected = configuration.presentation
    }

    override fun applyEditorTo(configuration: JankHunterRunConfiguration) {
        configuration.mode = mode.selectedItem as? JankHunterMode ?: JankHunterMode.INSPECT
        configuration.cliPath = cliPath.text
        configuration.logs = logs.text
        configuration.inspectLogScope = inspectLogScope.selectedItem as? JankHunterLogScope ?: JankHunterLogScope.ALL_SELECTED
        configuration.baseline = baseline.text
        configuration.baselineLogScope = baselineLogScope.selectedItem as? JankHunterLogScope ?: JankHunterLogScope.ALL_SELECTED
        configuration.candidate = candidate.text
        configuration.candidateLogScope = candidateLogScope.selectedItem as? JankHunterLogScope ?: JankHunterLogScope.ALL_SELECTED
        configuration.output = output.text
        configuration.ownerMap = ownerMap.text
        configuration.mapping = mapping.text
        configuration.classGraph = classGraph.text
        configuration.diagnostics = diagnostics.text
        configuration.heapDump = heapDump.text
        configuration.heapEvidence = heapEvidence.text
        configuration.route = route.text
        configuration.screen = screen.text
        configuration.owner = owner.text
        configuration.className = className.text
        configuration.dataset = dataset.selectedItem?.toString().orEmpty()
        configuration.format = format.selectedItem?.toString().orEmpty()
        configuration.json = json.isSelected
        configuration.presentation = presentation.isSelected
    }

    override fun createEditor(): JComponent = panel

    private fun add(row: Int, label: String, component: JComponent) {
        panel.add(
            JLabel(label),
            GridBagConstraints().apply {
                gridx = 0
                gridy = row
                anchor = GridBagConstraints.WEST
                insets = Insets(3, 0, 3, 8)
            },
        )
        panel.add(
            component,
            GridBagConstraints().apply {
                gridx = 1
                gridy = row
                weightx = 1.0
                fill = GridBagConstraints.HORIZONTAL
                insets = Insets(3, 0, 3, 0)
            },
        )
    }
}
