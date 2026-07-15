package io.jankhunter.plugin.actions

import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.project.DumbAwareAction
import io.jankhunter.plugin.services.JankHunterProjectService

class JankHunterOpenToolWindowAction : DumbAwareAction() {
    override fun actionPerformed(event: AnActionEvent) {
        val project = event.project ?: return
        JankHunterProjectService.getInstance(project).toggleFloatingWindow()
    }
}
