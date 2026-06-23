package io.jankhunter.plugin.ui

import com.intellij.openapi.Disposable
import com.intellij.openapi.project.DumbAware
import com.intellij.openapi.project.Project
import com.intellij.openapi.util.Disposer
import com.intellij.openapi.wm.ToolWindow
import com.intellij.openapi.wm.ToolWindowFactory
import com.intellij.ui.content.ContentFactory

class JankHunterToolWindowFactory : ToolWindowFactory, DumbAware {
    override fun createToolWindowContent(project: Project, toolWindow: ToolWindow) {
        val view = JankHunterToolWindow(project)
        val content = ContentFactory.getInstance().createContent(view.component, "Run", false)
        Disposer.register(content, view as Disposable)
        toolWindow.contentManager.addContent(content)
    }
}
