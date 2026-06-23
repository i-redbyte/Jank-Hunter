package io.jankhunter.plugin.services

import com.intellij.openapi.components.Service
import com.intellij.openapi.components.service
import com.intellij.openapi.project.Project
import io.jankhunter.plugin.ui.JankHunterToolWindow

@Service(Service.Level.PROJECT)
class JankHunterProjectService(private val project: Project) {
    private var toolWindow: JankHunterToolWindow? = null
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

    fun inspectClass(className: String) {
        val current = toolWindow
        if (current != null) {
            current.applyClassFilter(className)
        } else {
            pendingClassFilter = className
        }
    }

    companion object {
        fun getInstance(project: Project): JankHunterProjectService = project.service()
    }
}
