package io.jankhunter.plugin.profiles

import com.google.gson.GsonBuilder
import com.intellij.openapi.project.Project
import io.jankhunter.plugin.execution.JankHunterMode
import io.jankhunter.plugin.execution.JankHunterRunRequest
import java.io.File

data class JankHunterProfileFile(
    val profiles: MutableMap<String, JankHunterStoredProfile> = linkedMapOf(),
)

data class JankHunterStoredProfile(
    val mode: String = JankHunterMode.INSPECT.name,
    val logs: String = "",
    val baseline: String = "",
    val candidate: String = "",
    val output: String = "",
    val ownerMap: String = "",
    val mapping: String = "",
    val classGraph: String = "",
    val diagnostics: String = "",
    val heapDump: String = "",
    val heapEvidence: String = "",
    val baselineHeapDump: String = "",
    val baselineHeapEvidence: String = "",
    val candidateHeapDump: String = "",
    val candidateHeapEvidence: String = "",
    val route: String = "",
    val screen: String = "",
    val owner: String = "",
    val className: String = "",
    val dataset: String = "code-problems",
    val format: String = "csv",
    val json: Boolean = false,
    val presentation: Boolean = false,
) {
    fun toRequest(cliPath: String): JankHunterRunRequest =
        JankHunterRunRequest(
            mode = runCatching { JankHunterMode.valueOf(mode) }.getOrDefault(JankHunterMode.INSPECT),
            cliPath = cliPath,
            logs = logs,
            baseline = baseline,
            candidate = candidate,
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
        fun fromRequest(request: JankHunterRunRequest): JankHunterStoredProfile =
            JankHunterStoredProfile(
                mode = request.mode.name,
                logs = request.logs,
                baseline = request.baseline,
                candidate = request.candidate,
                output = request.output,
                ownerMap = request.ownerMap,
                mapping = request.mapping,
                classGraph = request.classGraph,
                diagnostics = request.diagnostics,
                heapDump = request.heapDump,
                heapEvidence = request.heapEvidence,
                baselineHeapDump = request.baselineHeapDump,
                baselineHeapEvidence = request.baselineHeapEvidence,
                candidateHeapDump = request.candidateHeapDump,
                candidateHeapEvidence = request.candidateHeapEvidence,
                route = request.route,
                screen = request.screen,
                owner = request.owner,
                className = request.className,
                dataset = request.dataset,
                format = request.format,
                json = request.json,
                presentation = request.presentation,
            )
    }
}

class JankHunterProfileStore(private val project: Project) {
    private val gson = GsonBuilder().setPrettyPrinting().create()
    private val file: File
        get() = File(project.basePath ?: ".", ".jankhunter/plugin.json")

    fun load(): JankHunterProfileFile {
        val target = file
        if (!target.isFile) return defaults()
        return runCatching { gson.fromJson(target.readText(), JankHunterProfileFile::class.java) }
            .getOrNull()
            ?.withDefaults()
            ?: defaults()
    }

    fun save(profileFile: JankHunterProfileFile) {
        val target = file
        target.parentFile?.mkdirs()
        target.writeText(gson.toJson(profileFile.withDefaults()))
    }

    fun saveProfile(name: String, request: JankHunterRunRequest) {
        val profiles = load()
        profiles.profiles[name] = JankHunterStoredProfile.fromRequest(request)
        save(profiles)
    }

    private fun JankHunterProfileFile.withDefaults(): JankHunterProfileFile {
        val defaults = defaults()
        defaults.profiles.putAll(profiles)
        return defaults
    }

    private fun defaults(): JankHunterProfileFile =
        JankHunterProfileFile(
            linkedMapOf(
                "debug" to JankHunterStoredProfile(mode = JankHunterMode.INSPECT.name, presentation = false),
                "release" to JankHunterStoredProfile(mode = JankHunterMode.COMPARE.name, presentation = true),
                "ci" to JankHunterStoredProfile(mode = JankHunterMode.SCORECARD.name, json = true, format = "json"),
                "local heap" to JankHunterStoredProfile(mode = JankHunterMode.INSPECT.name, presentation = true),
            ),
        )
}
