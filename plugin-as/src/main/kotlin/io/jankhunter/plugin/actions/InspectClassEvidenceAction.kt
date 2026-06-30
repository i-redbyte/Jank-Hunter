package io.jankhunter.plugin.actions

import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.actionSystem.CommonDataKeys
import com.intellij.openapi.project.DumbAwareAction
import io.jankhunter.plugin.services.JankHunterProjectService

class InspectClassEvidenceAction : DumbAwareAction() {
    override fun update(event: AnActionEvent) {
        event.presentation.isEnabledAndVisible = event.project != null && event.getData(CommonDataKeys.EDITOR) != null
    }

    override fun actionPerformed(event: AnActionEvent) {
        val project = event.project ?: return
        val editor = event.getData(CommonDataKeys.EDITOR) ?: return
        val documentText = editor.document.text
        val offset = editor.caretModel.offset.coerceIn(0, documentText.length)
        val className = classNameNearOffset(documentText, offset) ?: return
        JankHunterProjectService.getInstance(project).inspectClass(className)
    }

    private fun classNameNearOffset(text: String, offset: Int): String? {
        val prefix = text.substring(0, offset.coerceAtMost(text.length))
        val packageName = Regex("""(?m)^\s*package\s+([\w.]+)""").find(text)?.groupValues?.getOrNull(1).orEmpty()
        val classMatches = Regex("""\b(class|object|interface)\s+([A-Za-z_][\w]*)|enum\s+class\s+([A-Za-z_][\w]*)""")
            .findAll(prefix)
            .toList()
        val simple = classMatches.lastOrNull()?.let { match ->
            match.groupValues.getOrNull(2)?.takeIf(String::isNotBlank)
                ?: match.groupValues.getOrNull(3)?.takeIf(String::isNotBlank)
        } ?: return null
        return listOf(packageName, simple).filter(String::isNotBlank).joinToString(".")
    }
}
