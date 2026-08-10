package io.jankhunter.plugin.ui

import com.intellij.openapi.Disposable
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.fileChooser.FileChooser
import com.intellij.openapi.fileChooser.FileChooserDescriptor
import com.intellij.openapi.project.Project
import com.intellij.openapi.ui.TextFieldWithBrowseButton
import com.intellij.ui.components.JBLabel
import com.intellij.ui.components.JBList
import com.intellij.ui.components.JBScrollPane
import io.jankhunter.plugin.execution.JankHunterDiscoveredLog
import io.jankhunter.plugin.execution.JankHunterLogDirectorySnapshot
import io.jankhunter.plugin.execution.JankHunterLogDiscovery
import io.jankhunter.plugin.execution.JankHunterUserPaths
import java.awt.BorderLayout
import java.awt.Component
import java.awt.FlowLayout
import java.awt.event.MouseAdapter
import java.awt.event.MouseEvent
import java.io.File
import java.text.DecimalFormat
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.concurrent.Future
import javax.swing.BorderFactory
import javax.swing.DefaultListModel
import javax.swing.JButton
import javax.swing.JCheckBox
import javax.swing.JList
import javax.swing.JPanel
import javax.swing.ListCellRenderer
import javax.swing.ListSelectionModel

internal class JankHunterLogSelectionPanel(
    private val project: Project,
    title: String,
    initialDirectory: String,
    private val processedFingerprints: () -> Set<String>,
    private val onDirectoryChanged: (String) -> Unit,
    private val onSelectionChanged: () -> Unit,
) : Disposable {
    val component: JPanel

    private val directoryField = TextFieldWithBrowseButton()
    private val heapField = TextFieldWithBrowseButton()
    private val model = DefaultListModel<SelectableLog>()
    private val list = JBList(model)
    private val statusLabel = JBLabel("Выберите папку с .jhlog")
    private val refreshButton = JButton("Обновить")
    private val useDetectedHeapButton = JButton("Подставить HPROF")
    private var heapCandidates = emptyList<File>()
    private var scanTask: Future<*>? = null
    private var scanGeneration = 0L
    private var lastScannedDirectory: String? = null
    private var disposed = false

    init {
        directoryField.text = initialDirectory
        directoryField.addActionListener { chooseDirectory() }
        heapField.addActionListener { chooseHeapInput() }

        list.selectionMode = ListSelectionModel.SINGLE_SELECTION
        list.cellRenderer = LogRenderer()
        list.emptyText.text = "В выбранной папке нет .jhlog"
        list.addMouseListener(
            object : MouseAdapter() {
                override fun mouseClicked(event: MouseEvent) {
                    val index = list.locationToIndex(event.point)
                    if (index < 0) return
                    val bounds = list.getCellBounds(index, index) ?: return
                    if (!bounds.contains(event.point)) return
                    val item = model.getElementAt(index)
                    item.selected = !item.selected
                    list.repaint(bounds)
                    updateStatus()
                    onSelectionChanged()
                }
            },
        )

        refreshButton.addActionListener { scan() }
        useDetectedHeapButton.addActionListener {
            heapCandidates.singleOrNull()?.let { heapField.text = it.path }
            updateStatus()
            onSelectionChanged()
        }
        useDetectedHeapButton.isEnabled = false

        component = JPanel(BorderLayout(0, 8)).apply {
            border = BorderFactory.createTitledBorder(title)
            add(
                JPanel(BorderLayout(7, 0)).apply {
                    add(directoryField, BorderLayout.CENTER)
                    add(refreshButton, BorderLayout.EAST)
                },
                BorderLayout.NORTH,
            )
            add(JBScrollPane(list), BorderLayout.CENTER)
            add(
                JPanel(BorderLayout()).apply {
                    add(
                        JPanel(FlowLayout(FlowLayout.LEFT, 6, 0)).apply {
                            add(selectionButton("Новые") { item -> !item.log.processed })
                            add(selectionButton("Последние 4") { item -> model.indexOf(item) in 0..3 })
                            add(selectionButton("Все") { true })
                            add(selectionButton("Снять выбор") { false })
                        },
                        BorderLayout.NORTH,
                    )
                    add(
                        JPanel(BorderLayout(7, 0)).apply {
                            border = BorderFactory.createEmptyBorder(7, 0, 0, 0)
                            add(JBLabel("HPROF / heap evidence"), BorderLayout.WEST)
                            add(heapField, BorderLayout.CENTER)
                            add(useDetectedHeapButton, BorderLayout.EAST)
                        },
                        BorderLayout.CENTER,
                    )
                    add(statusLabel, BorderLayout.SOUTH)
                },
                BorderLayout.SOUTH,
            )
        }
    }

    fun directory(): File? = directoryField.text.trim()
        .takeIf(String::isNotEmpty)
        ?.let(JankHunterUserPaths::expandHome)
        ?.let(::File)

    fun selectedLogs(): List<JankHunterDiscoveredLog> = items()
        .filter(SelectableLog::selected)
        .map(SelectableLog::log)

    fun heapInput(): String = JankHunterUserPaths.expandHome(heapField.text)

    fun setDirectory(path: String) {
        directoryField.text = path
        scan()
    }

    fun setHeapInput(path: String) {
        heapField.text = path
        updateStatus()
        onSelectionChanged()
    }

    fun selectAll() = updateSelection { true }

    fun scan() {
        val directory = directory()
        val generation = ++scanGeneration
        scanTask?.cancel(true)
        val directoryPath = directory?.absoluteFile?.normalize()?.path
        if (directoryPath != lastScannedDirectory) {
            lastScannedDirectory = directoryPath
            model.clear()
            heapCandidates = emptyList()
            onSelectionChanged()
        }
        refreshButton.isEnabled = false
        statusLabel.text = "Сканирование…"
        scanTask = ApplicationManager.getApplication().executeOnPooledThread {
            val snapshot = if (directory == null) {
                JankHunterLogDirectorySnapshot(emptyList(), emptyList())
            } else {
                JankHunterLogDiscovery.scan(directory, processedFingerprints())
            }
            ApplicationManager.getApplication().invokeLater {
                if (disposed || project.isDisposed || generation != scanGeneration) return@invokeLater
                refreshButton.isEnabled = true
                applySnapshot(snapshot)
                directory?.path?.let(onDirectoryChanged)
            }
        }
    }

    override fun dispose() {
        if (disposed) return
        disposed = true
        scanTask?.cancel(true)
        model.clear()
    }

    fun chooseDirectory() {
        val descriptor = FileChooserDescriptor(false, true, false, false, false, false)
        val selected = FileChooser.chooseFile(descriptor, project, null) ?: return
        directoryField.text = selected.path
        scan()
    }

    private fun chooseHeapInput() {
        val descriptor = FileChooserDescriptor(true, false, false, false, false, false)
            .withFileFilter { file ->
                file.extension.equals("hprof", ignoreCase = true) || file.extension.equals("json", ignoreCase = true)
            }
        val selected = FileChooser.chooseFile(descriptor, project, null) ?: return
        heapField.text = selected.path
        updateStatus()
        onSelectionChanged()
    }

    private fun applySnapshot(snapshot: JankHunterLogDirectorySnapshot) {
        val selectedBefore = items()
            .filter(SelectableLog::selected)
            .mapTo(mutableSetOf()) { it.log.fingerprint }
        model.clear()
        val hasProcessedHistory = snapshot.logs.any(JankHunterDiscoveredLog::processed)
        snapshot.logs.forEach { log ->
            val selected = when {
                log.fingerprint in selectedBefore -> true
                selectedBefore.isNotEmpty() -> false
                hasProcessedHistory -> !log.processed
                else -> true
            }
            model.addElement(SelectableLog(log, selected))
        }
        heapCandidates = snapshot.heapCandidates
        updateStatus(snapshot.heapCandidates.size)
        onSelectionChanged()
    }

    private fun selectionButton(text: String, predicate: (SelectableLog) -> Boolean): JButton =
        JButton(text).apply { addActionListener { updateSelection(predicate) } }

    private fun updateSelection(predicate: (SelectableLog) -> Boolean) {
        items().forEach { item -> item.selected = predicate(item) }
        list.repaint()
        updateStatus()
        onSelectionChanged()
    }

    private fun updateStatus(heapCandidates: Int? = null) {
        val logs = items()
        val selected = logs.count(SelectableLog::selected)
        val fresh = logs.count { !it.log.processed }
        useDetectedHeapButton.isEnabled = this.heapCandidates.size == 1 && heapField.text.isBlank()
        statusLabel.text = buildString {
            append("Выбрано: $selected из ${logs.size}")
            if (fresh > 0) append(" · новых: $fresh")
            if (heapCandidates != null && heapCandidates > 0 && heapField.text.isBlank()) {
                append(" · найдено HPROF: $heapCandidates, подключите явно")
            }
        }
    }

    private fun items(): List<SelectableLog> = (0 until model.size).map(model::getElementAt)

    private data class SelectableLog(val log: JankHunterDiscoveredLog, var selected: Boolean)

    private class LogRenderer : JPanel(BorderLayout(8, 0)), ListCellRenderer<SelectableLog> {
        private val checkbox = JCheckBox()
        private val metadata = JBLabel()

        init {
            isOpaque = true
            checkbox.isOpaque = false
            add(checkbox, BorderLayout.CENTER)
            add(metadata, BorderLayout.EAST)
        }

        override fun getListCellRendererComponent(
            list: JList<out SelectableLog>,
            value: SelectableLog,
            index: Int,
            isSelected: Boolean,
            cellHasFocus: Boolean,
        ): Component {
            checkbox.text = value.log.file.name
            checkbox.isSelected = value.selected
            metadata.text = "${formatTime(value.log.file)} · ${formatSize(value.log.file.length())} · ${
                if (value.log.processed) "обработан" else "новый"
            }"
            background = if (isSelected) list.selectionBackground else list.background
            foreground = if (isSelected) list.selectionForeground else list.foreground
            checkbox.foreground = foreground
            metadata.foreground = foreground
            return this
        }

        private fun formatTime(file: File): String = TIME_FORMAT.format(
            Instant.ofEpochMilli(file.lastModified()).atZone(ZoneId.systemDefault()),
        )

        private fun formatSize(bytes: Long): String {
            if (bytes < 1024) return "$bytes B"
            val megabytes = bytes.toDouble() / (1024.0 * 1024.0)
            return "${SIZE_FORMAT.format(megabytes)} MB"
        }

        companion object {
            private val TIME_FORMAT = DateTimeFormatter.ofPattern("dd.MM HH:mm")
            private val SIZE_FORMAT = DecimalFormat("0.0")
        }
    }
}
