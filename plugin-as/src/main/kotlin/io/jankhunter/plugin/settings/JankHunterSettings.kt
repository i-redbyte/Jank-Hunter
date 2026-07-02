package io.jankhunter.plugin.settings

import com.intellij.openapi.components.PersistentStateComponent
import com.intellij.openapi.components.Service
import com.intellij.openapi.components.State
import com.intellij.openapi.components.Storage
import com.intellij.openapi.components.service
import io.jankhunter.plugin.execution.JankHunterMode
import io.jankhunter.plugin.execution.JankHunterLogScope
import io.jankhunter.plugin.execution.JankHunterRunRequest

@Service(Service.Level.APP)
@State(name = "JankHunterSettings", storages = [Storage("jankHunter.xml")])
class JankHunterSettings : PersistentStateComponent<JankHunterSettings.State> {
    private var currentState = State()

    override fun getState(): State = currentState

    override fun loadState(state: State) {
        currentState = state
    }

    class State {
        var cliPath: String = ""
        var logsDirectory: String = ""
        var packageName: String = ""
        var remoteLogsPath: String = ""
        var outputDirectory: String = ""
        var openReportInIde: Boolean = true
        var openReportExternally: Boolean = false
        var presentationMode: Boolean = false
        var lastRun: RecentRun? = null
        var recentRuns: MutableList<RecentRun> = mutableListOf()
    }

    companion object {
        fun getInstance(): JankHunterSettings = service()
    }
}

class RecentRun {
    var projectPath: String = ""
    var timestamp: String = ""
    var commandLine: String = ""
    var mode: String = JankHunterMode.INSPECT.name
    var cliPath: String = ""
    var logs: String = ""
    var inspectLogScope: String = JankHunterLogScope.ALL_SELECTED.name
    var baseline: String = ""
    var baselineLogScope: String = JankHunterLogScope.ALL_SELECTED.name
    var candidate: String = ""
    var candidateLogScope: String = JankHunterLogScope.ALL_SELECTED.name
    var output: String = ""
    var ownerMap: String = ""
    var mapping: String = ""
    var classGraph: String = ""
    var diagnostics: String = ""
    var heapDump: String = ""
    var heapEvidence: String = ""
    var baselineHeapDump: String = ""
    var baselineHeapEvidence: String = ""
    var candidateHeapDump: String = ""
    var candidateHeapEvidence: String = ""
    var route: String = ""
    var screen: String = ""
    var owner: String = ""
    var className: String = ""
    var dataset: String = ""
    var format: String = ""
    var json: Boolean = false
    var presentation: Boolean = false

    fun toRequest(): JankHunterRunRequest =
        JankHunterRunRequest(
            mode = runCatching { JankHunterMode.valueOf(mode) }.getOrDefault(JankHunterMode.INSPECT),
            cliPath = cliPath,
            logs = logs,
            inspectLogScope = parseLogScope(inspectLogScope),
            baseline = baseline,
            baselineLogScope = parseLogScope(baselineLogScope),
            candidate = candidate,
            candidateLogScope = parseLogScope(candidateLogScope),
            output = output,
            ownerMap = ownerMap,
            mapping = mapping,
            classGraph = classGraph,
            diagnostics = diagnostics,
            heapDump = heapDump,
            heapEvidence = heapEvidence,
            baselineHeapDump = baselineHeapDump,
            baselineHeapEvidence = baselineHeapEvidence,
            candidateHeapDump = candidateHeapDump,
            candidateHeapEvidence = candidateHeapEvidence,
            route = route,
            screen = screen,
            owner = owner,
            className = className,
            dataset = dataset,
            format = format,
            json = json,
            presentation = presentation,
        )

    companion object {
        fun fromRequest(
            timestamp: String,
            commandLine: String,
            request: JankHunterRunRequest,
            projectPath: String = "",
        ): RecentRun =
            RecentRun().apply {
                this.projectPath = projectPath
                this.timestamp = timestamp
                this.commandLine = commandLine
                mode = request.mode.name
                cliPath = request.cliPath
                logs = request.logs
                inspectLogScope = request.inspectLogScope.name
                baseline = request.baseline
                baselineLogScope = request.baselineLogScope.name
                candidate = request.candidate
                candidateLogScope = request.candidateLogScope.name
                output = request.output
                ownerMap = request.ownerMap
                mapping = request.mapping
                classGraph = request.classGraph
                diagnostics = request.diagnostics
                heapDump = request.heapDump
                heapEvidence = request.heapEvidence
                baselineHeapDump = request.baselineHeapDump
                baselineHeapEvidence = request.baselineHeapEvidence
                candidateHeapDump = request.candidateHeapDump
                candidateHeapEvidence = request.candidateHeapEvidence
                route = request.route
                screen = request.screen
                owner = request.owner
                className = request.className
                dataset = request.dataset
                format = request.format
                json = request.json
                presentation = request.presentation
            }

        private fun parseLogScope(value: String): JankHunterLogScope =
            runCatching { JankHunterLogScope.valueOf(value) }.getOrDefault(JankHunterLogScope.ALL_SELECTED)
    }
}
