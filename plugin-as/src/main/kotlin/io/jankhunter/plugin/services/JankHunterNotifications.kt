package io.jankhunter.plugin.services

import com.intellij.notification.NotificationAction
import com.intellij.notification.NotificationGroupManager
import com.intellij.notification.NotificationType
import com.intellij.openapi.project.Project
import java.io.File

object JankHunterNotifications {
    private const val GROUP_ID = "Jank Hunter"

    fun reportReady(
        project: Project,
        outputPath: String,
        problemCount: Int?,
        openReport: () -> Unit,
        openProblems: (() -> Unit)?,
        rerun: () -> Unit,
    ) {
        val message = buildString {
            append("Результат готов: ")
            append(File(outputPath).name)
            if (problemCount != null) {
                append(". Problems: ")
                append(problemCount)
            }
        }
        val notification = NotificationGroupManager.getInstance()
            .getNotificationGroup(GROUP_ID)
            .createNotification("Jank Hunter", message, NotificationType.INFORMATION)
            .addAction(NotificationAction.createSimple("Open Output", openReport))
        if (openProblems != null) {
            notification.addAction(NotificationAction.createSimple("Open Problems", openProblems))
        }
        notification
            .addAction(NotificationAction.createSimple("Rerun", rerun))
            .notify(project)
    }

    fun scorecardFailed(project: Project, message: String, openOutput: () -> Unit, rerun: () -> Unit) {
        NotificationGroupManager.getInstance()
            .getNotificationGroup(GROUP_ID)
            .createNotification("Jank Hunter scorecard failed", message, NotificationType.WARNING)
            .addAction(NotificationAction.createSimple("Open Output", openOutput))
            .addAction(NotificationAction.createSimple("Rerun", rerun))
            .notify(project)
    }

    fun error(project: Project, title: String, message: String) {
        NotificationGroupManager.getInstance()
            .getNotificationGroup(GROUP_ID)
            .createNotification(title, message, NotificationType.ERROR)
            .notify(project)
    }
}
