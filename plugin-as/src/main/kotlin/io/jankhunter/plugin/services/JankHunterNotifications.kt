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
        openReport: () -> Unit,
        rerun: () -> Unit,
    ) {
        NotificationGroupManager.getInstance()
            .getNotificationGroup(GROUP_ID)
            .createNotification(
                "Jank Hunter",
                "Результат готов: ${File(outputPath).name}",
                NotificationType.INFORMATION,
            )
            .addAction(NotificationAction.createSimple("Open Output", openReport))
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
