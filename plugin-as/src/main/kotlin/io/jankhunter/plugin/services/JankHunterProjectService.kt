package io.jankhunter.plugin.services

import com.intellij.openapi.Disposable
import com.intellij.openapi.components.Service
import com.intellij.openapi.components.service
import com.intellij.openapi.project.Project
import com.intellij.openapi.wm.WindowManager
import io.jankhunter.plugin.ui.JankHunterToolWindow
import java.awt.BorderLayout
import java.awt.event.WindowAdapter
import java.awt.event.WindowEvent
import javax.swing.JDialog

@Service(Service.Level.PROJECT)
class JankHunterProjectService(private val project: Project) : Disposable {
    private var toolWindow: JankHunterToolWindow? = null
    private var floatingWindow: JDialog? = null
    private var pendingClassFilter: String? = null

    fun register(toolWindow: JankHunterToolWindow) {
        this.toolWindow = toolWindow
        pendingClassFilter?.let {
            toolWindow.applyClassFilter(it)
            pendingClassFilter = null
        }
    }

    fun unregister(toolWindow: JankHunterToolWindow) {
        if (this.toolWindow === toolWindow) {
            this.toolWindow = null
        }
    }

    fun showFloatingWindow(): JankHunterToolWindow {
        val currentDialog = floatingWindow
        val currentView = toolWindow
        if (currentDialog != null && currentDialog.isDisplayable && currentView != null) {
            currentDialog.isVisible = true
            currentDialog.toFront()
            return currentView
        }

        val view = JankHunterToolWindow(project)
        val parent = WindowManager.getInstance().suggestParentWindow(project)
        val dialog = JDialog(parent, "Jank Hunter", java.awt.Dialog.ModalityType.MODELESS).apply {
            contentPane.layout = BorderLayout()
            contentPane.add(view.component, BorderLayout.CENTER)
            setSize(920, 720)
            setLocationRelativeTo(parent)
            defaultCloseOperation = JDialog.DISPOSE_ON_CLOSE
            addWindowListener(
                object : WindowAdapter() {
                    override fun windowClosed(event: WindowEvent) {
                        view.dispose()
                        if (floatingWindow === this@apply) {
                            floatingWindow = null
                        }
                    }
                },
            )
        }
        floatingWindow = dialog
        dialog.isVisible = true
        return view
    }

    fun toggleFloatingWindow() {
        val currentDialog = floatingWindow
        if (currentDialog != null && currentDialog.isDisplayable && currentDialog.isVisible) {
            currentDialog.dispose()
            return
        }
        showFloatingWindow()
    }

    fun inspectClass(className: String) {
        val current = toolWindow ?: showFloatingWindow()
        current.applyClassFilter(className)
    }

    override fun dispose() {
        val view = toolWindow
        val dialog = floatingWindow
        toolWindow = null
        floatingWindow = null
        pendingClassFilter = null
        view?.dispose()
        dialog?.dispose()
    }

    companion object {
        fun getInstance(project: Project): JankHunterProjectService = project.service()
    }
}
