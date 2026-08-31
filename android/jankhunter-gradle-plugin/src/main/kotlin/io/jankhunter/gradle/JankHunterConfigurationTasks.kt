package io.jankhunter.gradle

import org.gradle.api.DefaultTask
import org.gradle.api.GradleException
import org.gradle.api.file.RegularFileProperty
import org.gradle.api.provider.ListProperty
import org.gradle.api.provider.Property
import org.gradle.api.tasks.Input
import org.gradle.api.tasks.OutputFile
import org.gradle.api.tasks.TaskAction
import org.gradle.api.tasks.CacheableTask
import org.gradle.api.tasks.options.Option
import org.gradle.work.DisableCachingByDefault
import java.io.File
import java.util.Locale

internal object JankHunterConfigurationReport {
    fun effective(configuration: JankHunterResolvedConfiguration): String = buildString(2_048) {
        appendLine("Jank Hunter effective configuration")
        appendLine("Variant: ${configuration.variant.name}")
        appendLine("Build type: ${configuration.variant.buildType}")
        appendLine("Profile: ${configuration.profile}")
        appendLine("Purpose: ${configuration.profile.definition.description}")
        appendLine("Validation: run jankHunterValidate${configuration.variant.name.capitalized()}Configuration")
        appendLine()
        appendLine("Core:")
        appendLine("  collection = ${configuration.collection.name.lowercase(Locale.US)}")
        appendLine("  processes = ${configuration.processes.name.lowercase(Locale.US)}")
        appendLine("  tuning.queueCapacity = ${configuration.maxQueueSize}")
        appendLine("  storage = ${configuration.storage.render()}")
        appendLine("  growthAnalytics = ${configuration.growthAnalytics}")
        appendLine("  autoInit = ${configuration.autoInit}")
        appendLine("  deleteObsoleteLogs = ${configuration.deleteObsoleteLogs}")
        appendLine()
        appendLine("Features:")
        JankHunterFeature.entries.forEach { feature ->
            appendLine("  ${feature.name.lowercase(Locale.US)} = ${feature in configuration.features}")
        }
        appendLine()
        appendLine("Scope:")
        appendLine("  mode = ${configuration.instrumentation.scope.name.lowercase(Locale.US)}")
        appendLine("  includePackages = ${configuration.includePackages.sorted().renderList()}")
        appendLine("  excludePackages = ${configuration.excludePackages.sorted().renderList()}")
        appendLine()
        appendLine("Runtime:")
        appendLine("  mainThreadStallThresholdMs = ${configuration.runtime.mainThreadStallThresholdMs}")
        appendLine("  ownerBlockThresholdMs = ${configuration.runtime.ownerBlockThresholdMs}")
        appendLine("  httpSlowThresholdMs = ${configuration.runtime.httpSlowThresholdMs}")
        appendLine("  jankFrameThresholdMs = ${configuration.runtime.jankFrameThresholdMs}")
        appendLine("  uiWindowP95ThresholdMs = ${configuration.runtime.uiWindowP95ThresholdMs}")
        appendLine("  mainLooperDispatchMonitor = ${configuration.runtime.mainLooperDispatchMonitor}")
        appendLine("  jankStats = ${configuration.runtime.jankStats}")
        appendLine("  ioTracing = ${configuration.runtime.ioTracing}")
        appendLine()
        appendLine("Instrumentation:")
        appendLine("  dependencyInjectionAnalysis = ${configuration.instrumentation.dependencyInjectionAnalysis}")
        appendLine("  methodCounters = ${configuration.instrumentation.methodCounters}")
        appendLine("  methodFiltering = ${configuration.instrumentation.methodFilterMode}")
        appendLine("  okhttp = ${configuration.instrumentation.okhttp}")
        appendLine("  webSockets = ${configuration.instrumentation.webSockets}")
        appendLine("  handlers = ${configuration.instrumentation.handlers}")
        appendLine("  executors = ${configuration.instrumentation.executors}")
        appendLine("  coroutines = ${configuration.instrumentation.coroutines}")
        appendLine("  interactionOperations = ${configuration.instrumentation.interactionOperations}")
        appendLine("  lifecycleLeaks = ${configuration.instrumentation.lifecycleLeaks}")
        appendLine("  logSpam = ${configuration.instrumentation.logSpam}")
        appendLine("  classGraph = ${configuration.instrumentation.classGraph}")
        appendLine("  runtimeCallGraph = ${configuration.instrumentation.runtimeCallGraph}")
        appendLine("  composeTracing = ${configuration.instrumentation.composeTracing}")
        appendLine("  roomTracing = ${configuration.instrumentation.roomTracing}")
        appendLine("  databaseTracing = ${configuration.instrumentation.databaseTracing}")
        appendLine("  workerTracing = ${configuration.instrumentation.workerTracing}")
        appendLine("  ioTracing = ${configuration.instrumentation.ioTracing}")
        appendLine("  androidComponents = ${configuration.instrumentation.androidComponents}")
        appendLine("  binderIPC = ${configuration.instrumentation.binderIPC}")
        val warnings = JankHunterConfigurationValidation.warnings(configuration)
        if (warnings.isNotEmpty()) {
            appendLine()
            appendLine("Warnings:")
            warnings.forEach { warning -> appendLine("  - $warning") }
        }
        if (VariantBuildTypeMatcher.isReleaseLike(configuration.variant.buildType)) {
            appendLine()
            appendLine("Release approvals:")
            appendLine("  instrumentationApproved = ${configuration.releaseSafety.instrumentationApproved}")
            appendLine("  privacyReviewed = ${configuration.releaseSafety.privacyReviewed}")
            appendLine("  heapDumpsAllowed = ${configuration.releaseSafety.allowHeapDumps}")
            appendLine("  secondaryProcessesAllowed = ${configuration.releaseSafety.allowSecondaryProcesses}")
            appendLine("  unlimitedStorageAllowed = ${configuration.releaseSafety.allowUnlimitedStorage}")
            appendLine("  performanceBudgetEvidence = ${configuration.releaseSafety.performanceBudgetEvidence}")
        }
    }.trimEnd()

    fun details(configuration: JankHunterResolvedConfiguration): String = buildString(2_048) {
        appendLine("Resolution details for ${configuration.variant.name}")
        appendLine("Profile:")
        appendLine("  effective value = ${configuration.profile}")
        appendLine("  purpose = ${configuration.profile.definition.description}")
        appendLine()
        appendLine("Feature resolution:")
        JankHunterFeature.entries.forEach { feature ->
            appendLine("  ${feature.name.lowercase(Locale.US)}:")
            appendLine("    effective value = ${feature in configuration.features}")
            appendLine(
                "    source = ${configuration.featureSources[feature] ?: "profile(${configuration.profile})"}",
            )
        }
        appendLine()
        appendLine("Precedence: global < build type < flavor < exact variant")
        appendLine("Conflicting explicit values at the same precedence level are rejected.")
    }.trimEnd()

    fun instrumentationPlan(configuration: JankHunterResolvedConfiguration): String = buildString(1_024) {
        val instrumentation = configuration.instrumentation
        appendLine("Jank Hunter instrumentation plan")
        appendLine("Variant: ${configuration.variant.name}")
        appendLine("Bytecode scope: ${instrumentation.scope.name.lowercase(Locale.US)}")
        appendLine("Packages: ${configuration.includePackages.sorted().renderList()}")
        appendLine("Runtime hooks enabled: ${instrumentation.hasRuntimeHooks}")
        appendLine("Database boundaries: ${instrumentation.databaseTracing}")
        appendLine("Room boundaries: ${instrumentation.roomTracing}")
        appendLine("Network boundaries: ${instrumentation.okhttp}")
        appendLine("WebSocket boundaries: ${instrumentation.webSockets}")
        appendLine("Dependency injection analysis: ${instrumentation.dependencyInjectionAnalysis}")
        appendLine("Class graph: ${instrumentation.classGraph}")
        appendLine("Runtime call graph: ${instrumentation.runtimeCallGraph}")
        appendLine("Lifecycle transform: ${instrumentation.lifecycleLeaks}")
        appendLine("Android component lifecycle: ${instrumentation.androidComponents}")
        appendLine("Binder/AIDL boundaries: ${instrumentation.binderIPC}")
    }.trimEnd()

    fun json(configuration: JankHunterResolvedConfiguration): String = buildString(1_024) {
        append('{')
        append("\"variant\":\"").append(configuration.variant.name.jsonEscape()).append("\",")
        append("\"buildType\":\"").append(configuration.variant.buildType.jsonEscape()).append("\",")
        append("\"profile\":\"").append(configuration.profile).append("\",")
        append("\"collection\":\"").append(configuration.collection).append("\",")
        append("\"processes\":\"").append(configuration.processes).append("\",")
        append("\"scope\":\"").append(configuration.instrumentation.scope).append("\",")
        append("\"maxQueueSize\":").append(configuration.maxQueueSize).append(',')
        append("\"growthAnalytics\":").append(configuration.growthAnalytics).append(',')
        append("\"storage\":\"").append(configuration.storage.render().jsonEscape()).append("\",")
        append("\"features\":{")
        JankHunterFeature.entries.forEachIndexed { index, feature ->
            if (index > 0) append(',')
            append('"').append(feature.name.lowercase(Locale.US)).append("\":")
            append(feature in configuration.features)
        }
        append("},\"warnings\":[")
        JankHunterConfigurationValidation.warnings(configuration).forEachIndexed { index, warning ->
            if (index > 0) append(',')
            append('"').append(warning.jsonEscape()).append('"')
        }
        append("],\"includePackages\":[")
        configuration.includePackages.sorted().forEachIndexed { index, value ->
            if (index > 0) append(',')
            append('"').append(value.jsonEscape()).append('"')
        }
        append("]}")
    }

    fun profiles(): String = buildString(1_024) {
        appendLine("Jank Hunter profiles")
        JankHunterProfile.entries.forEach { profile ->
            val definition = profile.definition
            appendLine()
            appendLine(profile.name)
            appendLine("  Purpose: ${definition.description}")
            appendLine("  Collection: ${definition.collection}")
            appendLine("  Processes: ${definition.processes}")
            appendLine("  Queue capacity: ${definition.queueCapacity}")
            appendLine("  Features: ${definition.features.sortedBy(JankHunterFeature::ordinal).renderList()}")
        }
    }.trimEnd()

    private fun JankHunterStorage.render(): String = when (this) {
        is JankHunterStorage.Limited -> "limited(${maxSessionMiB} MiB/session)"
        JankHunterStorage.Unlimited -> "unlimited"
    }

    private fun Iterable<*>.renderList(): String = joinToString(prefix = "[", postfix = "]")

    private fun String.jsonEscape(): String = buildString(length + 8) {
        this@jsonEscape.forEach { character ->
            when (character) {
                '\\' -> append("\\\\")
                '"' -> append("\\\"")
                '\n' -> append("\\n")
                '\r' -> append("\\r")
                '\t' -> append("\\t")
                else -> append(character)
            }
        }
    }
}

internal object JankHunterConfigurationValidation {
    fun warnings(configuration: JankHunterResolvedConfiguration): List<String> {
        if (configuration.processes != JankHunterProcesses.MAIN_ONLY) return emptyList()
        val instrumentation = configuration.instrumentation
        if (!instrumentation.androidComponents && !instrumentation.binderIPC) return emptyList()
        return listOf(
            "Android Components/IPC analysis is partial with processes=MAIN_ONLY: " +
                "secondary-process lifecycle and Binder peers are absent. " +
                "Use processes.set(ALL) (and allowSecondaryProcesses() for release) for full cross-process analysis.",
        )
    }

    fun failures(configuration: JankHunterResolvedConfiguration): List<String> {
        val failures = ArrayList<String>(8)
        positive(failures, "maxQueueSize", configuration.maxQueueSize.toLong())
        positive(failures, "mainThreadStallThresholdMs", configuration.runtime.mainThreadStallThresholdMs)
        positive(failures, "ownerBlockThresholdMs", configuration.runtime.ownerBlockThresholdMs)
        positive(failures, "httpSlowThresholdMs", configuration.runtime.httpSlowThresholdMs)
        positive(failures, "jankFrameThresholdMs", configuration.runtime.jankFrameThresholdMs)
        positive(failures, "uiWindowP95ThresholdMs", configuration.runtime.uiWindowP95ThresholdMs)
        nonNegative(failures, "mainThreadAdmissionWaitMs", configuration.runtime.mainThreadAdmissionWaitMs)
        nonNegative(failures, "backgroundAdmissionWaitMs", configuration.runtime.backgroundAdmissionWaitMs)
        positive(failures, "retainedHeapDumpMinIntervalMs", configuration.retainedHeapDump.minIntervalMs)
        positive(failures, "retainedHeapDumpMaxCount", configuration.retainedHeapDump.maxCount.toLong())
        nonNegative(failures, "retainedHeapDumpMinRetainedAgeMs", configuration.retainedHeapDump.minRetainedAgeMs)
        if (configuration.retainedHeapDump.enabled && !configuration.releaseSafety.privacyReviewed) {
            failures += "HEAP_DUMPS requires privacyReviewed()."
        }
        val ambiguousPackages = configuration.includePackages.intersect(configuration.excludePackages)
        if (ambiguousPackages.isNotEmpty()) {
            failures += "Packages cannot be included and excluded simultaneously: ${ambiguousPackages.sorted()}"
        }
        if (VariantBuildTypeMatcher.isReleaseLike(configuration.variant.buildType)) {
            validateRelease(configuration, failures)
        }
        return failures
    }

    private fun validateRelease(
        configuration: JankHunterResolvedConfiguration,
        failures: MutableList<String>,
    ) {
        val safety = configuration.releaseSafety
        if (!safety.instrumentationApproved) {
            failures += "Release instrumentation must be approved with release { ... }."
        }
        if (!safety.privacyReviewed) {
            failures += "Release instrumentation requires privacyReviewed()."
        }
        validatePerformanceBudget(safety.performanceBudgetEvidence, failures)
        if (configuration.retainedHeapDump.enabled && !safety.allowHeapDumps) {
            failures += "HEAP_DUMPS in release requires allowHeapDumps()."
        }
        if (configuration.processes == JankHunterProcesses.ALL && !safety.allowSecondaryProcesses) {
            failures += "processes=ALL in release requires allowSecondaryProcesses()."
        }
        if (configuration.storage == JankHunterStorage.Unlimited && !safety.allowUnlimitedStorage) {
            failures += "Unlimited release storage requires allowUnlimitedStorage()."
        }
    }

    private fun validatePerformanceBudget(path: String?, failures: MutableList<String>) {
        val normalized = path?.trim().orEmpty()
        if (normalized.isEmpty()) {
            failures += "Release instrumentation requires performanceBudget(file(...))."
            return
        }
        val file = File(normalized)
        if (!file.isFile) {
            failures += "Performance budget evidence does not exist: ${file.path}"
        } else if (!file.readText().contains(PERFORMANCE_BUDGET_MARKER)) {
            failures += "Performance budget evidence must contain $PERFORMANCE_BUDGET_MARKER."
        }
    }

    private fun positive(failures: MutableList<String>, name: String, value: Long) {
        if (value <= 0L) failures += "$name must be greater than zero, but was $value."
    }

    private fun nonNegative(failures: MutableList<String>, name: String, value: Long) {
        if (value < 0L) failures += "$name must not be negative, but was $value."
    }

    const val PERFORMANCE_BUDGET_MARKER = "jankhunter_release_performance_budget_v1"
}

@DisableCachingByDefault(because = "Diagnostic task only prints already resolved configuration")
abstract class PrintJankHunterConfigurationTask : DefaultTask() {
    @get:Input
    abstract val effectiveReports: ListProperty<String>

    @get:Input
    abstract val detailedReports: ListProperty<String>

    private var showDetails: Boolean = false

    @Option(option = "details", description = "Print precedence and source details after effective values.")
    fun setShowDetails(value: Boolean) {
        showDetails = value
    }

    @TaskAction
    fun printConfiguration() {
        effectiveReports.get().forEachIndexed { index, report ->
            if (index > 0) logger.lifecycle(SECTION_SEPARATOR)
            logger.lifecycle(report)
            if (showDetails) {
                logger.lifecycle("")
                logger.lifecycle(detailedReports.get()[index])
            }
        }
    }
}

@DisableCachingByDefault(because = "Validation must run before every requested Android build")
abstract class ValidateJankHunterConfigurationTask : DefaultTask() {
    @get:Input
    abstract val variantName: Property<String>

    @get:Input
    abstract val failures: ListProperty<String>

    @get:Input
    abstract val warnings: ListProperty<String>

    @TaskAction
    fun validateConfiguration() {
        val problems = failures.get()
        if (problems.isNotEmpty()) {
            throw GradleException(
                buildString {
                    append("Invalid Jank Hunter configuration for variant '")
                    append(variantName.get())
                    append("':")
                    problems.forEach { problem -> append("\n- ").append(problem) }
                    append("\nRun jankHunterPrint")
                    append(variantName.get().capitalized())
                    append("Configuration --details to inspect the resolved values.")
                },
            )
        }
        warnings.get().forEach { warning -> logger.warn("Jank Hunter: {}", warning) }
        logger.lifecycle("Jank Hunter configuration for {} is valid.", variantName.get())
    }
}

@DisableCachingByDefault(because = "Validation must run before every requested Android build")
abstract class ValidateJankHunterFunctionalParityTask : DefaultTask() {
    @get:Input
    abstract val checkedPairs: ListProperty<String>

    @get:Input
    abstract val differences: ListProperty<String>

    @TaskAction
    fun validateParity() {
        val mismatches = differences.get()
        if (mismatches.isNotEmpty()) {
            throw GradleException(
                buildString {
                    append("Jank Hunter debug/release functional parity check failed:")
                    mismatches.forEach { mismatch -> append("\n- ").append(mismatch) }
                    append("\nRelease approvals are ignored by this comparison.")
                    append("\nUse an explicit buildType(...) override when the difference is intentional.")
                },
            )
        }
        val pairs = checkedPairs.get()
        if (pairs.isEmpty()) {
            logger.lifecycle("No implicit Jank Hunter debug/release pair requires a parity check.")
        } else {
            logger.lifecycle(
                "Jank Hunter debug/release functional configurations are consistent for {}.",
                pairs.joinToString(),
            )
        }
    }
}

@CacheableTask
abstract class ExportJankHunterConfigurationTask : DefaultTask() {
    @get:Input
    abstract val configurations: ListProperty<String>

    @get:OutputFile
    abstract val outputFile: RegularFileProperty

    @TaskAction
    fun export() {
        val destination = outputFile.get().asFile
        destination.parentFile.mkdirs()
        destination.writeText(
            configurations.get().joinToString(
                separator = ",\n",
                prefix = "{\n  \"configurations\": [\n",
                postfix = "\n  ]\n}\n",
            ) { configuration -> "    $configuration" },
        )
        logger.lifecycle("Jank Hunter configuration exported to {}", destination.path)
    }
}

internal fun String.capitalized(): String {
    return replaceFirstChar { character ->
        if (character.isLowerCase()) character.titlecase(Locale.US) else character.toString()
    }
}

private const val SECTION_SEPARATOR = "------------------------------------------------------------"
