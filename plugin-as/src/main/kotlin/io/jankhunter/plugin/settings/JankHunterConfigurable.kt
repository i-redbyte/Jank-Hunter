package io.jankhunter.plugin.settings

import com.intellij.openapi.fileChooser.FileChooser
import com.intellij.openapi.fileChooser.FileChooserDescriptor
import com.intellij.openapi.options.SearchableConfigurable
import com.intellij.openapi.ui.TextFieldWithBrowseButton
import com.intellij.ui.components.JBCheckBox
import java.awt.GridBagConstraints
import java.awt.GridBagLayout
import java.awt.Insets
import javax.swing.JComponent
import javax.swing.JLabel
import javax.swing.JPanel

class JankHunterConfigurable : SearchableConfigurable {
    private var panel: JPanel? = null
    private var cliPathField: TextFieldWithBrowseButton? = null
    private var outputDirectoryField: TextFieldWithBrowseButton? = null
    private var openInIdeCheckBox: JBCheckBox? = null
    private var openExternalCheckBox: JBCheckBox? = null
    private var presentationCheckBox: JBCheckBox? = null

    override fun getId(): String = "io.jankhunter.settings"

    override fun getDisplayName(): String = "Jank Hunter"

    override fun createComponent(): JComponent {
        val cliField = TextFieldWithBrowseButton()
        val outField = TextFieldWithBrowseButton()
        val openInIde = JBCheckBox("Open generated HTML reports inside the IDE")
        val openExternal = JBCheckBox("Open generated HTML reports in the system browser")
        val presentation = JBCheckBox("Use presentation mode by default")

        cliField.toolTipText = hint(
            "Путь к бинарнику jankhunter. Можно указать ../cli/bin/jankhunter или системную команду, доступную в PATH.",
        )
        outField.toolTipText = hint(
            "Папка для автогенерируемых отчетов. Если пусто, используется build/jankhunter внутри открытого проекта.",
        )
        openInIde.toolTipText = hint("Автоматически открывать HTML-отчет во встроенной вкладке Report после успешного запуска.")
        openExternal.toolTipText = hint("Автоматически открывать HTML-отчет в браузере по умолчанию.")
        presentation.toolTipText = hint("По умолчанию добавлять --presentation для inspect и compare.")

        addBrowseAction(
            cliField,
            FileChooserDescriptor(true, false, false, false, false, false),
        )
        addBrowseAction(
            outField,
            FileChooserDescriptor(false, true, false, false, false, false),
        )

        val createdPanel = JPanel(GridBagLayout())
        addRow(createdPanel, 0, "CLI", cliField)
        addRow(createdPanel, 1, "Output directory", outField)
        addWideRow(createdPanel, 2, openInIde)
        addWideRow(createdPanel, 3, openExternal)
        addWideRow(createdPanel, 4, presentation)

        cliPathField = cliField
        outputDirectoryField = outField
        openInIdeCheckBox = openInIde
        openExternalCheckBox = openExternal
        presentationCheckBox = presentation
        panel = createdPanel

        reset()
        return createdPanel
    }

    override fun isModified(): Boolean {
        val state = JankHunterSettings.getInstance().state
        return cliPathField?.text.orEmpty() != state.cliPath ||
            outputDirectoryField?.text.orEmpty() != state.outputDirectory ||
            openInIdeCheckBox?.isSelected != state.openReportInIde ||
            openExternalCheckBox?.isSelected != state.openReportExternally ||
            presentationCheckBox?.isSelected != state.presentationMode
    }

    override fun apply() {
        val state = JankHunterSettings.getInstance().state
        state.cliPath = cliPathField?.text.orEmpty().trim()
        state.outputDirectory = outputDirectoryField?.text.orEmpty().trim()
        state.openReportInIde = openInIdeCheckBox?.isSelected == true
        state.openReportExternally = openExternalCheckBox?.isSelected == true
        state.presentationMode = presentationCheckBox?.isSelected == true
    }

    override fun reset() {
        val state = JankHunterSettings.getInstance().state
        cliPathField?.text = state.cliPath
        outputDirectoryField?.text = state.outputDirectory
        openInIdeCheckBox?.isSelected = state.openReportInIde
        openExternalCheckBox?.isSelected = state.openReportExternally
        presentationCheckBox?.isSelected = state.presentationMode
    }

    override fun disposeUIResources() {
        panel = null
        cliPathField = null
        outputDirectoryField = null
        openInIdeCheckBox = null
        openExternalCheckBox = null
        presentationCheckBox = null
    }

    private fun addBrowseAction(field: TextFieldWithBrowseButton, descriptor: FileChooserDescriptor) {
        field.addActionListener {
            FileChooser.chooseFile(descriptor, null, null)?.let { selected ->
                field.text = selected.path
            }
        }
    }

    private fun addRow(panel: JPanel, row: Int, label: String, component: JComponent) {
        panel.add(
            JLabel(label),
            GridBagConstraints().apply {
                gridx = 0
                gridy = row
                anchor = GridBagConstraints.WEST
                insets = Insets(6, 0, 6, 8)
            },
        )
        panel.add(
            component,
            GridBagConstraints().apply {
                gridx = 1
                gridy = row
                weightx = 1.0
                fill = GridBagConstraints.HORIZONTAL
                insets = Insets(6, 0, 6, 0)
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
                anchor = GridBagConstraints.WEST
                insets = Insets(6, 0, 6, 0)
            },
        )
    }

    private fun hint(text: String): String = "<html>$text</html>"
}
