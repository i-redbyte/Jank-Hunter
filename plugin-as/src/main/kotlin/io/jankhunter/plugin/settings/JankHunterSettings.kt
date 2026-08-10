package io.jankhunter.plugin.settings

import com.intellij.openapi.components.PersistentStateComponent
import com.intellij.openapi.components.Service
import com.intellij.openapi.components.State
import com.intellij.openapi.components.Storage
import com.intellij.openapi.components.service
import io.jankhunter.plugin.execution.JankHunterUserPaths
import java.io.File

@Service(Service.Level.APP)
@State(name = "JankHunterSettings", storages = [Storage("jankHunter.xml")])
class JankHunterSettings : PersistentStateComponent<JankHunterSettings.State> {
    private var currentState = migrate(State())

    override fun getState(): State = currentState

    override fun loadState(state: State) {
        currentState = migrate(state)
    }

    class State {
        var schemaVersion: Int = 0
        var cliPath: String = ""
        var logsDirectory: String = ""
        var baselineLogsDirectory: String = ""
        var candidateLogsDirectory: String = ""
        var outputDirectory: String = defaultOutputDirectory()
        var openReportExternally: Boolean = true
        var presentationMode: Boolean = false
        var reportStyle: String = "modern"
        var processedLogFingerprints: MutableList<String> = mutableListOf()
        var packageName: String = ""
        var openReportInIde: Boolean = true
        var lastRun: JankHunterRecentRun? = null
        var recentRuns: MutableList<JankHunterRecentRun> = mutableListOf()
    }

    companion object {
        fun getInstance(): JankHunterSettings = service()

        fun defaultOutputDirectory(): String = File(JankHunterUserPaths.homeDirectory(), "JankHunter/reports").path

        private fun migrate(state: State): State = state.apply {
            if (schemaVersion < CURRENT_SCHEMA_VERSION) {
                outputDirectory = outputDirectory.ifBlank { defaultOutputDirectory() }
                openReportExternally = openReportExternally || openReportInIde
                schemaVersion = CURRENT_SCHEMA_VERSION
            }
        }

        internal const val CURRENT_SCHEMA_VERSION = 1
    }
}
