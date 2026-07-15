package io.jankhunter.plugin.run

import com.intellij.execution.Executor
import com.intellij.execution.configurations.ConfigurationFactory
import com.intellij.execution.configurations.LocatableConfigurationBase
import com.intellij.execution.configurations.RunProfileState
import com.intellij.execution.runners.ExecutionEnvironment
import com.intellij.openapi.project.Project
import com.intellij.openapi.util.InvalidDataException
import com.intellij.openapi.util.WriteExternalException
import io.jankhunter.plugin.execution.JankHunterLogScope
import io.jankhunter.plugin.execution.JankHunterMode
import io.jankhunter.plugin.execution.JankHunterRunRequest
import org.jdom.Element

class JankHunterRunConfiguration(
    project: Project,
    factory: ConfigurationFactory,
    name: String,
) : LocatableConfigurationBase<RunProfileState>(project, factory, name) {
    var mode: JankHunterMode = JankHunterMode.INSPECT
    var cliPath: String = ""
    var logs: String = ""
    var inspectLogScope: JankHunterLogScope = JankHunterLogScope.ALL_SELECTED
    var baseline: String = ""
    var baselineLogScope: JankHunterLogScope = JankHunterLogScope.ALL_SELECTED
    var candidate: String = ""
    var candidateLogScope: JankHunterLogScope = JankHunterLogScope.ALL_SELECTED
    var output: String = ""
    var ownerMap: String = ""
    var mapping: String = ""
    var classGraph: String = ""
    var diagnostics: String = ""
    var diCatalog: String = ""
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
    var dataset: String = "code-problems"
    var format: String = "csv"
    var json: Boolean = false
    var presentation: Boolean = false

    override fun getConfigurationEditor(): JankHunterRunSettingsEditor = JankHunterRunSettingsEditor(project)

    override fun getState(executor: Executor, environment: ExecutionEnvironment): RunProfileState =
        JankHunterRunProfileState(project, environment, toRequest())

    fun toRequest(): JankHunterRunRequest =
        JankHunterRunRequest(
            mode = mode,
            cliPath = cliPath,
            logs = logs,
            inspectLogScope = inspectLogScope,
            baseline = baseline,
            baselineLogScope = baselineLogScope,
            candidate = candidate,
            candidateLogScope = candidateLogScope,
            output = output,
            ownerMap = ownerMap,
            mapping = mapping,
            classGraph = classGraph,
            diagnostics = diagnostics,
            diCatalog = diCatalog,
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

    fun load(request: JankHunterRunRequest) {
        mode = request.mode
        cliPath = request.cliPath
        logs = request.logs
        inspectLogScope = request.inspectLogScope
        baseline = request.baseline
        baselineLogScope = request.baselineLogScope
        candidate = request.candidate
        candidateLogScope = request.candidateLogScope
        output = request.output
        ownerMap = request.ownerMap
        mapping = request.mapping
        classGraph = request.classGraph
        diagnostics = request.diagnostics
        diCatalog = request.diCatalog
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

    @Throws(WriteExternalException::class)
    override fun writeExternal(element: Element) {
        super.writeExternal(element)
        val data = Element("jankHunter")
        data.setAttribute("mode", mode.name)
        data.setAttribute("cliPath", cliPath)
        data.setAttribute("logs", logs)
        data.setAttribute("inspectLogScope", inspectLogScope.name)
        data.setAttribute("baseline", baseline)
        data.setAttribute("baselineLogScope", baselineLogScope.name)
        data.setAttribute("candidate", candidate)
        data.setAttribute("candidateLogScope", candidateLogScope.name)
        data.setAttribute("output", output)
        data.setAttribute("ownerMap", ownerMap)
        data.setAttribute("mapping", mapping)
        data.setAttribute("classGraph", classGraph)
        data.setAttribute("diagnostics", diagnostics)
        data.setAttribute("diCatalog", diCatalog)
        data.setAttribute("heapDump", heapDump)
        data.setAttribute("heapEvidence", heapEvidence)
        data.setAttribute("baselineHeapDump", baselineHeapDump)
        data.setAttribute("baselineHeapEvidence", baselineHeapEvidence)
        data.setAttribute("candidateHeapDump", candidateHeapDump)
        data.setAttribute("candidateHeapEvidence", candidateHeapEvidence)
        data.setAttribute("route", route)
        data.setAttribute("screen", screen)
        data.setAttribute("owner", owner)
        data.setAttribute("className", className)
        data.setAttribute("dataset", dataset)
        data.setAttribute("format", format)
        data.setAttribute("json", json.toString())
        data.setAttribute("presentation", presentation.toString())
        element.addContent(data)
    }

    @Throws(InvalidDataException::class)
    override fun readExternal(element: Element) {
        super.readExternal(element)
        val data = element.getChild("jankHunter") ?: return
        mode = runCatching { JankHunterMode.valueOf(data.getAttributeValue("mode", JankHunterMode.INSPECT.name)) }
            .getOrDefault(JankHunterMode.INSPECT)
        cliPath = data.getAttributeValue("cliPath", "")
        logs = data.getAttributeValue("logs", "")
        inspectLogScope = parseLogScope(data.getAttributeValue("inspectLogScope"), JankHunterLogScope.ALL_SELECTED)
        baseline = data.getAttributeValue("baseline", "")
        baselineLogScope = parseLogScope(data.getAttributeValue("baselineLogScope"), JankHunterLogScope.ALL_SELECTED)
        candidate = data.getAttributeValue("candidate", "")
        candidateLogScope = parseLogScope(data.getAttributeValue("candidateLogScope"), JankHunterLogScope.ALL_SELECTED)
        output = data.getAttributeValue("output", "")
        ownerMap = data.getAttributeValue("ownerMap", "")
        mapping = data.getAttributeValue("mapping", "")
        classGraph = data.getAttributeValue("classGraph", "")
        diagnostics = data.getAttributeValue("diagnostics", "")
        diCatalog = data.getAttributeValue("diCatalog", "")
        heapDump = data.getAttributeValue("heapDump", "")
        heapEvidence = data.getAttributeValue("heapEvidence", "")
        baselineHeapDump = data.getAttributeValue("baselineHeapDump", "")
        baselineHeapEvidence = data.getAttributeValue("baselineHeapEvidence", "")
        candidateHeapDump = data.getAttributeValue("candidateHeapDump", "")
        candidateHeapEvidence = data.getAttributeValue("candidateHeapEvidence", "")
        route = data.getAttributeValue("route", "")
        screen = data.getAttributeValue("screen", "")
        owner = data.getAttributeValue("owner", "")
        className = data.getAttributeValue("className", "")
        dataset = data.getAttributeValue("dataset", "code-problems")
        format = data.getAttributeValue("format", "csv")
        json = data.getAttributeValue("json", "false").toBoolean()
        presentation = data.getAttributeValue("presentation", "false").toBoolean()
    }

    private fun parseLogScope(value: String?, fallback: JankHunterLogScope): JankHunterLogScope =
        runCatching { JankHunterLogScope.valueOf(value ?: fallback.name) }.getOrDefault(fallback)
}
