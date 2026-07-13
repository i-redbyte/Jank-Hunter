package io.jankhunter.gradle

import com.android.build.api.instrumentation.FramesComputationMode
import com.android.build.api.instrumentation.InstrumentationScope
import com.android.build.api.variant.AndroidComponentsExtension
import org.gradle.api.GradleException
import org.gradle.api.Plugin
import org.gradle.api.Project
import java.util.Locale

class JankHunterPlugin : Plugin<Project> {
    override fun apply(project: Project) {
        val extension = project.extensions.create("jankHunter", JankHunterExtension::class.java)

        project.pluginManager.withPlugin("com.android.application") {
            JankHunterAutomaticDependencies.configure(project, extension, addAndroidSdk = true)
            configureAndroidProject(
                project,
                extension,
                instrumentationScope = InstrumentationScope.ALL,
                generateRuntimeManifest = true,
            )
        }
        project.pluginManager.withPlugin("com.android.library") {
            JankHunterAutomaticDependencies.configure(project, extension, addAndroidSdk = false)
            configureAndroidProject(
                project,
                extension,
                instrumentationScope = InstrumentationScope.PROJECT,
                generateRuntimeManifest = false,
            )
        }
    }

    private fun configureAndroidProject(
        project: Project,
        extension: JankHunterExtension,
        instrumentationScope: InstrumentationScope,
        generateRuntimeManifest: Boolean,
    ) {
        val androidComponents = project.extensions.findByType(AndroidComponentsExtension::class.java)
        if (androidComponents == null) {
            project.logger.warn("Jank Hunter could not find AndroidComponentsExtension.")
            return
        }

        androidComponents.onVariants { variant ->
            if (!extension.isVariantEnabled(variant.name)) {
                if (!extension.enabled && extension.verboseLogs) {
                    project.logger.lifecycle(
                        "Jank Hunter variant {} skipped because jankHunter.enabled=false.",
                        variant.name,
                    )
                }
                return@onVariants
            }
            val logBucket = normalizedLogBucket(extension.logBucket)
            val releaseVariant = VariantBuildTypeMatcher.isReleaseLike(variant.name)
            if (releaseVariant) {
                validateReleaseSafety(project, extension, variant.name)
            }
            val effectiveInstrumentationScope = instrumentationScope
            val shouldGenerateRuntimeManifest = generateRuntimeManifest
            if (shouldGenerateRuntimeManifest) {
                val runtimeManifest = project.tasks.register(
                    "generate${variant.name.capitalized()}JankHunterRuntimeManifest",
                    GenerateJankHunterRuntimeManifestTask::class.java,
                ) {
                    if (!extension.autoInit) {
                        it.runtimeEnabled.set(false)
                    }
                    it.mainThreadStallThresholdMs.set(extension.runtime.mainThreadStallThresholdMs)
                    it.ownerBlockThresholdMs.set(extension.runtime.ownerBlockThresholdMs)
                    it.httpSlowThresholdMs.set(extension.runtime.httpSlowThresholdMs)
                    it.mainLooperDispatchMonitorEnabled.set(extension.runtime.mainLooperDispatchMonitor)
                    it.retainedHeapDumpEnabled.set(extension.retainedHeapDump.enabled)
                    it.retainedHeapDumpPrivacyApproved.set(extension.retainedHeapDump.privacyApproved)
                    it.retainedHeapDumpMinIntervalMs.set(extension.retainedHeapDump.minIntervalMs)
                    it.retainedHeapDumpMaxCount.set(extension.retainedHeapDump.maxCount)
                    it.retainedHeapDumpMinRetainedAgeMs.set(extension.retainedHeapDump.minRetainedAgeMs)
                    it.jankStatsEnabled.set(extension.runtime.jankStats)
                    it.jankFrameThresholdMs.set(extension.runtime.jankFrameThresholdMs)
                    it.uiWindowP95ThresholdMs.set(extension.runtime.uiWindowP95ThresholdMs)
                    it.mainProcessOnly.set(extension.runtime.mainProcessOnly)
                    it.logBucket.set(logBucket)
                }
                variant.sources.manifests.addGeneratedManifestFile(
                    runtimeManifest,
                    GenerateJankHunterRuntimeManifestTask::outputFile,
                )
            }

            val artifactRoot = project.layout.buildDirectory.dir(
                "intermediates/jankhunter/${variant.name}/instrumentation-artifacts",
            )
            val ownerMapEntriesDirectory = artifactRoot.map { it.dir("owner-map-entries") }
            val classGraphDirectory = artifactRoot.map { it.dir("class-graph") }
            val diagnosticsDirectory = artifactRoot.map { it.dir("diagnostics") }
            val includeWholeApplication = extension.instrument.includeWholeApplication
            val manualIncludes = extension.instrument.includePackages.toList()
            val androidNamespace = variant.namespace.orElse("")
            val effectiveIncludePackages = androidNamespace.map { namespace ->
                InstrumentationPackages.effectiveIncludes(
                    manualIncludes,
                    includeWholeApplication,
                    namespace,
                )
            }

            val ownerMap = project.tasks.register(
                "generate${variant.name.capitalized()}JankHunterOwnerMap",
                GenerateJankHunterOwnerMapTask::class.java,
            ) {
                it.outputs.upToDateWhen { false }
                it.variantName.set(variant.name)
                it.methodCounters.set(extension.instrument.methodCounters)
                it.okhttp.set(extension.instrument.okhttp)
                it.webSockets.set(extension.instrument.webSockets)
                it.handlers.set(extension.instrument.handlers)
                it.executors.set(extension.instrument.executors)
                it.coroutines.set(extension.instrument.coroutines)
                it.flowInteractions.set(extension.instrument.flowInteractions)
                it.lifecycleLeaks.set(extension.instrument.lifecycleLeaks)
                it.logSpam.set(extension.instrument.logSpam)
                it.classGraph.set(extension.instrument.classGraph)
                it.runtimeCallGraph.set(extension.instrument.runtimeCallGraph)
                it.generatedOwners.set(extension.instrument.methodCounters || extension.instrument.runtimeCallGraph)
                it.allowEmptyIncludePackages.set(extension.instrument.allowEmptyIncludePackages)
                it.includeWholeApplication.set(includeWholeApplication)
                it.androidNamespace.set(androidNamespace)
                it.includePackages.set(effectiveIncludePackages)
                it.excludePackages.set(extension.instrument.excludePackages.toList())
                it.entriesDirectory.set(ownerMapEntriesDirectory)
                it.entryFiles.from(ownerMapEntriesDirectory.map { directory ->
                    directory.asFileTree.matching { pattern ->
                        pattern.include("**/*.jsonl")
                    }
                })
                it.outputFile.set(
                    project.layout.buildDirectory.file("generated/jankhunter/${variant.name}/owner-map.json"),
                )
            }

            val classGraphOutput = project.layout.buildDirectory.file(
                "generated/jankhunter/${variant.name}/class-graph.jsonl",
            )
            val instrumentationDiagnosticsOutput = project.layout.buildDirectory.file(
                "generated/jankhunter/${variant.name}/instrumentation-diagnostics.jsonl",
            )
            val mergeArtifacts = project.tasks.register(
                "merge${variant.name.capitalized()}JankHunterInstrumentationArtifacts",
                MergeJankHunterInstrumentationArtifactsTask::class.java,
            ) {
                it.classGraphDirectory.set(classGraphDirectory)
                it.diagnosticsDirectory.set(diagnosticsDirectory)
                it.classGraphFiles.from(classGraphDirectory.map { directory ->
                    directory.asFileTree.matching { pattern ->
                        pattern.include("**/*.jsonl")
                    }
                })
                it.diagnosticsFiles.from(diagnosticsDirectory.map { directory ->
                    directory.asFileTree.matching { pattern ->
                        pattern.include("**/*.jsonl")
                    }
                })
                it.classGraphOutputFile.set(classGraphOutput)
                it.diagnosticsOutputFile.set(instrumentationDiagnosticsOutput)
            }
            project.afterEvaluate {
                JankHunterDependencyValidator.validateDeclaredOkHttpHelper(
                    project,
                    variant.name,
                    hooksEnabled = extension.instrument.okhttp || extension.instrument.webSockets,
                )
            }
            project.tasks.matching { it.name == "assemble${variant.name.capitalized()}" }.configureEach {
                it.finalizedBy(ownerMap)
                it.finalizedBy(mergeArtifacts)
            }
            project.tasks.matching { it.name == "transform${variant.name.capitalized()}ClassesWithAsm" }.configureEach {
                it.finalizedBy(ownerMap)
                it.finalizedBy(mergeArtifacts)
            }

            variant.instrumentation.transformClassesWith(
                JankHunterClassVisitorFactory::class.java,
                effectiveInstrumentationScope,
            ) { params ->
                params.methodCounters.set(extension.instrument.methodCounters)
                params.okhttp.set(extension.instrument.okhttp)
                params.webSockets.set(extension.instrument.webSockets)
                params.handlers.set(extension.instrument.handlers)
                params.executors.set(extension.instrument.executors)
                params.coroutines.set(extension.instrument.coroutines)
                params.flowInteractions.set(extension.instrument.flowInteractions)
                params.lifecycleLeaks.set(extension.instrument.lifecycleLeaks)
                params.logSpam.set(extension.instrument.logSpam)
                params.classGraph.set(extension.instrument.classGraph)
                params.runtimeCallGraph.set(extension.instrument.runtimeCallGraph)
                params.classGraphDirectory.set(classGraphDirectory.map { it.asFile.absolutePath })
                params.instrumentationDiagnosticsDirectory.set(
                    diagnosticsDirectory.map { it.asFile.absolutePath },
                )
                params.ownerMapEntriesDirectory.set(ownerMapEntriesDirectory.map { it.asFile.absolutePath })
                params.allowEmptyIncludePackages.set(extension.instrument.allowEmptyIncludePackages)
                params.asmProgressLog.set(extension.instrument.asmProgressLog)
                params.progressLabel.set(project.progressLabel(variant.name))
                params.includePackages.set(effectiveIncludePackages)
                params.excludePackages.set(extension.instrument.excludePackages.toList())
            }
            variant.instrumentation.setAsmFramesComputationMode(
                FramesComputationMode.COMPUTE_FRAMES_FOR_INSTRUMENTED_METHODS,
            )

            if (extension.verboseLogs) {
                project.logger.lifecycle(
                    "Jank Hunter variant {} configured. " +
                        "methodCounters={} okhttp={} webSockets={} handlers={} executors={} coroutines={} " +
                        "flowInteractions={} lifecycleLeaks={} logSpam={} classGraph={} runtimeCallGraph={} " +
                        "allowEmptyIncludePackages={} includeWholeApplication={} asmProgressLog={} autoInit={} " +
                        "retainedHeapDump={} retainedHeapDumpMinIntervalMs={} retainedHeapDumpMaxCount={} " +
                        "retainedHeapDumpMinRetainedAgeMs={} instrumentationScope={} generateRuntimeManifest={} " +
                        "logBucket={} ownerMapTask={} mergeArtifactsTask={}",
                    variant.name,
                    extension.instrument.methodCounters,
                    extension.instrument.okhttp,
                    extension.instrument.webSockets,
                    extension.instrument.handlers,
                    extension.instrument.executors,
                    extension.instrument.coroutines,
                    extension.instrument.flowInteractions,
                    extension.instrument.lifecycleLeaks,
                    extension.instrument.logSpam,
                    extension.instrument.classGraph,
                    extension.instrument.runtimeCallGraph,
                    extension.instrument.allowEmptyIncludePackages,
                    extension.instrument.includeWholeApplication,
                    extension.instrument.asmProgressLog,
                    extension.autoInit,
                    extension.retainedHeapDump.enabled,
                    extension.retainedHeapDump.minIntervalMs,
                    extension.retainedHeapDump.maxCount,
                    extension.retainedHeapDump.minRetainedAgeMs,
                    effectiveInstrumentationScope,
                    generateRuntimeManifest,
                    logBucket,
                    ownerMap.name,
                    mergeArtifacts.name,
                )
            }
        }
    }

    private fun JankHunterExtension.isVariantEnabled(variantName: String): Boolean {
        return VariantBuildTypeMatcher.isEnabled(
            variantName = variantName,
            enabledBuildTypes = enabledBuildTypes,
            pluginEnabled = enabled,
        )
    }

    private fun String.capitalized(): String {
        return replaceFirstChar {
            if (it.isLowerCase()) it.titlecase(Locale.US) else it.toString()
        }
    }

    private fun Project.progressLabel(variantName: String): String {
        return if (path == ":") ":$variantName" else "$path:$variantName"
    }

    private fun normalizedLogBucket(value: String): String {
        val normalized = value.trim().lowercase(Locale.US)
        if (normalized == "daily" || normalized == "session") return normalized
        throw GradleException(
            "jankHunter.logBucket must be either 'daily' or 'session', but was '$value'.",
        )
    }

    private fun validateReleaseSafety(project: Project, extension: JankHunterExtension, variantName: String) {
        val safety = extension.releaseSafety
        if (!safety.allowInstrumentation) {
            throw GradleException(
                "Jank Hunter instrumentation is enabled for release-like variant '$variantName'. " +
                    "Set jankHunter.releaseSafety.allowInstrumentation=true after an explicit release review.",
            )
        }
        if (!safety.privacyReviewed) {
            throw GradleException(
                "Jank Hunter release instrumentation for '$variantName' requires " +
                    "jankHunter.releaseSafety.privacyReviewed=true.",
            )
        }
        validatePerformanceBudget(project, safety.performanceBudgetEvidence, variantName)
        if (extension.retainedHeapDump.enabled && !extension.retainedHeapDump.privacyApproved) {
            throw GradleException(
                "retainedHeapDump.enabled=true for '$variantName' requires " +
                    "jankHunter.retainedHeapDump.privacyApproved=true.",
            )
        }
        if (extension.retainedHeapDump.enabled && !safety.allowHeapDumps) {
            throw GradleException(
                "retainedHeapDump.enabled=true for release-like variant '$variantName' requires " +
                    "jankHunter.releaseSafety.allowHeapDumps=true.",
            )
        }
        if (!extension.runtime.mainProcessOnly && !safety.allowSecondaryProcesses) {
            throw GradleException(
                "runtime.mainProcessOnly=false for release-like variant '$variantName' requires " +
                    "jankHunter.releaseSafety.allowSecondaryProcesses=true.",
            )
        }
    }

    private fun validatePerformanceBudget(project: Project, evidencePath: String?, variantName: String) {
        val path = evidencePath?.trim().orEmpty()
        if (path.isEmpty()) {
            throw GradleException(
                "Jank Hunter release instrumentation for '$variantName' requires " +
                    "jankHunter.releaseSafety.performanceBudgetEvidence pointing to a benchmark evidence file.",
            )
        }
        val evidence = project.file(path)
        if (!evidence.isFile) {
            throw GradleException("Jank Hunter performance budget evidence file does not exist: ${evidence.path}")
        }
        if (!evidence.readText().contains(PERFORMANCE_BUDGET_MARKER)) {
            throw GradleException(
                "Jank Hunter performance budget evidence for '$variantName' must contain " +
                    PERFORMANCE_BUDGET_MARKER,
            )
        }
    }

    private companion object {
        private const val PERFORMANCE_BUDGET_MARKER = "jankhunter_release_performance_budget_v1"
    }
}

internal object VariantBuildTypeMatcher {
    fun isEnabled(
        variantName: String,
        enabledBuildTypes: Iterable<String>,
        pluginEnabled: Boolean = true,
    ): Boolean {
        if (!pluginEnabled) return false
        val normalizedVariant = variantName.lowercase(Locale.US)
        return enabledBuildTypes
            .map { it.trim().lowercase(Locale.US) }
            .filter { it.isNotEmpty() }
            .any { buildType ->
                normalizedVariant == buildType || normalizedVariant.endsWith(buildType)
            }
    }

    fun isReleaseLike(variantName: String): Boolean {
        val normalizedVariant = variantName.lowercase(Locale.US)
        return normalizedVariant == "release" || normalizedVariant.endsWith("release")
    }
}
