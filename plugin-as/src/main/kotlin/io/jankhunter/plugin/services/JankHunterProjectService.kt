package io.jankhunter.plugin.services

import com.intellij.openapi.Disposable
import com.intellij.openapi.components.Service
import com.intellij.openapi.components.service
import com.intellij.openapi.project.Project
import com.intellij.openapi.wm.WindowManager
import io.jankhunter.plugin.ui.JankHunterToolWindow
import java.awt.BorderLayout
import java.awt.Dialog
import java.awt.GraphicsConfiguration
import java.awt.GraphicsEnvironment
import java.awt.Rectangle
import java.awt.Toolkit
import java.awt.Window
import java.awt.event.ComponentAdapter
import java.awt.event.ComponentEvent
import java.awt.event.WindowAdapter
import java.awt.event.WindowEvent
import javax.swing.JDialog

@Service(Service.Level.PROJECT)
class JankHunterProjectService(private val project: Project) : Disposable {
    private var view: JankHunterToolWindow? = null
    private var floatingWindow: JDialog? = null
    private var lastWindowBounds: Rectangle? = null
    private var pendingClassFilter: String? = null

    fun register(windowView: JankHunterToolWindow) {
        view = windowView
        pendingClassFilter?.let(windowView::applyClassFilter)
        pendingClassFilter = null
    }

    fun unregister(windowView: JankHunterToolWindow) {
        if (view === windowView) view = null
    }

    fun showFloatingWindow(): JankHunterToolWindow {
        val currentDialog = floatingWindow
        val currentView = view
        if (currentDialog != null && currentDialog.isDisplayable && currentView != null) {
            currentDialog.isVisible = true
            currentDialog.toFront()
            currentDialog.requestFocus()
            return currentView
        }

        val windowView = JankHunterToolWindow(project)
        val parent = WindowManager.getInstance().suggestParentWindow(project)
        val dialog = JDialog(parent, "Jank Hunter", Dialog.ModalityType.MODELESS).apply {
            contentPane.layout = BorderLayout()
            contentPane.add(windowView.component, BorderLayout.CENTER)
            defaultCloseOperation = JDialog.DISPOSE_ON_CLOSE
            applySafeBounds(this, parent)
            addComponentListener(
                object : ComponentAdapter() {
                    override fun componentMoved(event: ComponentEvent) = rememberBounds(this@apply)

                    override fun componentResized(event: ComponentEvent) = rememberBounds(this@apply)
                },
            )
            addWindowListener(
                object : WindowAdapter() {
                    override fun windowClosed(event: WindowEvent) {
                        rememberBounds(this@apply)
                        windowView.dispose()
                        if (floatingWindow === this@apply) floatingWindow = null
                    }
                },
            )
        }
        floatingWindow = dialog
        dialog.isVisible = true
        return windowView
    }

    fun toggleFloatingWindow() {
        val current = floatingWindow
        if (current != null && current.isDisplayable && current.isVisible) {
            current.dispose()
        } else {
            showFloatingWindow()
        }
    }

    fun inspectClass(className: String) {
        val current = view ?: showFloatingWindow()
        current.applyClassFilter(className)
        floatingWindow?.toFront()
    }

    override fun dispose() {
        val currentView = view
        val dialog = floatingWindow
        view = null
        floatingWindow = null
        pendingClassFilter = null
        currentView?.dispose()
        dialog?.dispose()
    }

    private fun applySafeBounds(dialog: JDialog, parent: Window?) {
        val screen = usableScreenBounds(parent?.graphicsConfiguration ?: defaultGraphicsConfiguration())
        dialog.minimumSize = java.awt.Dimension(
            MIN_WINDOW_WIDTH.coerceAtMost(screen.width),
            MIN_WINDOW_HEIGHT.coerceAtMost(screen.height),
        )
        val remembered = lastWindowBounds
        val width = (remembered?.width ?: DEFAULT_WINDOW_WIDTH)
            .coerceIn(MIN_WINDOW_WIDTH.coerceAtMost(screen.width), screen.width)
        val height = (remembered?.height ?: DEFAULT_WINDOW_HEIGHT)
            .coerceIn(MIN_WINDOW_HEIGHT.coerceAtMost(screen.height), screen.height)
        val preferredX = remembered?.x ?: (screen.x + (screen.width - width) / 2)
        val preferredY = remembered?.y ?: (screen.y + (screen.height - height) / 2)
        val x = preferredX.coerceIn(screen.x, screen.x + screen.width - width)
        val y = preferredY.coerceIn(screen.y, screen.y + screen.height - height)
        dialog.setBounds(x, y, width, height)
    }

    private fun usableScreenBounds(configuration: GraphicsConfiguration): Rectangle {
        val bounds = configuration.bounds
        val insets = Toolkit.getDefaultToolkit().getScreenInsets(configuration)
        return Rectangle(
            bounds.x + insets.left,
            bounds.y + insets.top,
            (bounds.width - insets.left - insets.right).coerceAtLeast(1),
            (bounds.height - insets.top - insets.bottom).coerceAtLeast(1),
        )
    }

    private fun defaultGraphicsConfiguration(): GraphicsConfiguration =
        GraphicsEnvironment.getLocalGraphicsEnvironment().defaultScreenDevice.defaultConfiguration

    private fun rememberBounds(dialog: JDialog) {
        if (dialog.width > 0 && dialog.height > 0) lastWindowBounds = Rectangle(dialog.bounds)
    }

    companion object {
        private const val DEFAULT_WINDOW_WIDTH = 1_100
        private const val DEFAULT_WINDOW_HEIGHT = 820
        private const val MIN_WINDOW_WIDTH = 760
        private const val MIN_WINDOW_HEIGHT = 560

        fun getInstance(project: Project): JankHunterProjectService = project.service()
    }
}
