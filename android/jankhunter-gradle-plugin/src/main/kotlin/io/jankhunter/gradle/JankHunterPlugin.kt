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
            JankHunterAutomaticDependencies.addAnnotations(project)
            configureAndroidProject(
                project,
                extension,
                instrumentationScope = InstrumentationScope.PROJECT,
                applicationProject = true,
            )
        }
        project.pluginManager.withPlugin("com.android.library") {
            JankHunterAutomaticDependencies.addAnnotations(project)
            configureAndroidProject(
                project,
                extension,
                instrumentationScope = InstrumentationScope.PROJECT,
                applicationProject = false,
            )
        }
    }

    private fun configureAndroidProject(
        project: Project,
        extension: JankHunterExtension,
        instrumentationScope: InstrumentationScope,
        applicationProject: Boolean,
    ) {
        val androidComponents = project.extensions.findByType(AndroidComponentsExtension::class.java)
        if (androidComponents == null) {
            project.logger.warn("Jank Hunter could not find AndroidComponentsExtension.")
            return
        }

        androidComponents.onVariants { variant ->
            if (!extension.isVariantEnabled(variant.name)) {
                if (!extension.enabled.getOrElse(true) && extension.verboseLogs.getOrElse(false)) {
                    project.logger.lifecycle(
                        "Jank Hunter variant {} skipped because jankHunter.enabled=false.",
                        variant.name,
                    )
                }
                return@onVariants
            }
            if (applicationProject) {
                JankHunterAutomaticDependencies.addRuntime(project, variant.name)
                configureBuildBanner(project, variant.name)
            }
            val maxSessionLogSizeMiB = validatedPositiveMiB(
                "jankHunter.maxSessionLogSizeMiB",
                extension.maxSessionLogSizeMiB.get(),
            )
            val releaseVariant = VariantBuildTypeMatcher.isReleaseLike(variant.name)
            if (releaseVariant) {
                validateReleaseSafety(project, extension, variant.name)
            }
            val symbolNamespace = JankHunterSymbolNamespace.current()
            val effectiveInstrumentationScope = instrumentationScope
            val shouldGenerateRuntimeManifest = applicationProject
            if (shouldGenerateRuntimeManifest) {
                val runtimeManifest = project.tasks.register(
                    "generate${variant.name.capitalized()}JankHunterRuntimeManifest",
                    GenerateJankHunterRuntimeManifestTask::class.java,
                ) {
                    it.autoInit.set(extension.autoInit)
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
                    it.sessionLogSizeLimitEnabled.set(extension.sessionLogSizeLimitEnabled)
                    it.maxSessionLogSizeMiB.set(maxSessionLogSizeMiB)
                    it.symbolNamespace.set(symbolNamespace)
                    it.outputFile.set(
                        project.layout.buildDirectory.file(
                            "generated/jankhunterRuntimeManifest/${variant.name}/AndroidManifest.xml",
                        ),
                    )
                }
                variant.sources.manifests.addGeneratedManifestFile(
                    runtimeManifest,
                    GenerateJankHunterRuntimeManifestTask::outputFile,
                )
            }

            val artifactRoot = project.layout.buildDirectory.dir(
                ArtifactSchemas.instrumentationArtifactsPath(variant.name),
            )
            val ownerMapEntriesDirectory = artifactRoot.map { it.dir("owner-map-entries") }
            val classGraphDirectory = artifactRoot.map { it.dir("class-graph") }
            val diagnosticsDirectory = artifactRoot.map { it.dir("diagnostics") }
            val dependencyInjectionCatalogDirectory = artifactRoot.map { it.dir("dependency-injection") }
            val dependencyInjectionAnalysisEnabled =
                extension.dependencyInjectionAnalysis.getOrElse(JankHunterFeatureMode.DISABLED) ==
                    JankHunterFeatureMode.ENABLED
            val runtimeHooksEnabled = extension.instrument.hasRuntimeHooksEnabled()
            val manualIncludes = extension.instrument.includePackages.getOrElse(emptySet())
            val effectiveExcludePackages = extension.instrument.excludePackages.map { packages ->
                InstrumentationPackages.normalizedPackages(packages)
            }
            val androidNamespace = variant.namespace.orElse("")
            val effectiveIncludePackages = androidNamespace.map { namespace ->
                val includes = InstrumentationPackages.effectiveIncludes(
                    manualIncludes,
                    namespace,
                )
                if (includes.isEmpty()) {
                    throw GradleException(
                        "Jank Hunter cannot determine a safe instrumentation boundary for variant " +
                            "'${variant.name}'. Set the Android namespace or add " +
                            "jankHunter.instrument.includePackages(...).",
                    )
                }
                includes
            }
            val ownerMap = project.tasks.register(
                "generate${variant.name.capitalized()}JankHunterOwnerMap",
                GenerateJankHunterOwnerMapTask::class.java,
            ) {
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
                it.generatedOwners.set(runtimeHooksEnabled)
                it.symbolNamespace.set(symbolNamespace)
                it.androidNamespace.set(androidNamespace)
                it.includePackages.set(effectiveIncludePackages)
                it.excludePackages.set(effectiveExcludePackages)
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
            val dependencyInjectionCatalogOutput = project.layout.buildDirectory.file(
                "generated/jankhunter/${variant.name}/di-catalog.jsonl",
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
            val mergeDependencyInjectionCatalog = project.tasks.register(
                "merge${variant.name.capitalized()}JankHunterDependencyInjectionCatalog",
                MergeJankHunterDependencyInjectionCatalogTask::class.java,
            ) {
                it.analysisEnabled.set(dependencyInjectionAnalysisEnabled)
                it.variantName.set(variant.name)
                it.shardsDirectory.set(dependencyInjectionCatalogDirectory)
                it.shardFiles.from(dependencyInjectionCatalogDirectory.map { directory ->
                    directory.asFileTree.matching { pattern ->
                        pattern.include("**/*.jsonl")
                    }
                })
                it.outputFile.set(dependencyInjectionCatalogOutput)
            }
            if (applicationProject) {
                JankHunterDependencyValidator.validateDeclaredRuntime(
                    project,
                    variant.name,
                    hooksEnabled = runtimeHooksEnabled,
                )
            }
            val okHttpHelperAvailable = JankHunterDependencyValidator.validateDeclaredOkHttpHelper(
                project,
                variant.name,
                hooksEnabled = extension.instrument.okhttp.get() || extension.instrument.webSockets.get(),
            )
            project.tasks.matching { it.name == "assemble${variant.name.capitalized()}" }.configureEach {
                it.finalizedBy(ownerMap)
                it.finalizedBy(mergeArtifacts)
                it.finalizedBy(mergeDependencyInjectionCatalog)
            }
            project.tasks.matching { it.name == "transform${variant.name.capitalized()}ClassesWithAsm" }.configureEach {
                it.finalizedBy(ownerMap)
                it.finalizedBy(mergeArtifacts)
                it.finalizedBy(mergeDependencyInjectionCatalog)
            }

            variant.instrumentation.transformClassesWith(
                JankHunterClassVisitorFactory::class.java,
                effectiveInstrumentationScope,
            ) { params ->
                params.embeddedSymbols.set(
                    extension.symbolMode.map { mode -> mode == JankHunterSymbolMode.EMBEDDED },
                )
                params.dependencyInjectionAnalysis.set(dependencyInjectionAnalysisEnabled)
                params.methodCounters.set(extension.instrument.methodCounters)
                params.okhttp.set(extension.instrument.okhttp)
                params.webSockets.set(extension.instrument.webSockets)
                params.okHttpHelperAvailable.set(okHttpHelperAvailable)
                params.handlers.set(extension.instrument.handlers)
                params.executors.set(extension.instrument.executors)
                params.coroutines.set(extension.instrument.coroutines)
                params.flowInteractions.set(extension.instrument.flowInteractions)
                params.lifecycleLeaks.set(false)
                params.logSpam.set(extension.instrument.logSpam)
                params.classGraph.set(extension.instrument.classGraph)
                params.runtimeCallGraph.set(extension.instrument.runtimeCallGraph)
                params.classGraphDirectory.set(classGraphDirectory.map { it.asFile.absolutePath })
                params.instrumentationDiagnosticsDirectory.set(
                    diagnosticsDirectory.map { it.asFile.absolutePath },
                )
                params.ownerMapEntriesDirectory.set(ownerMapEntriesDirectory.map { it.asFile.absolutePath })
                params.dependencyInjectionCatalogDirectory.set(
                    dependencyInjectionCatalogDirectory.map { it.asFile.absolutePath },
                )
                params.asmProgressLog.set(extension.instrument.asmProgressLog)
                params.progressLabel.set(project.progressLabel(variant.name))
                params.includePackages.set(effectiveIncludePackages)
                params.excludePackages.set(effectiveExcludePackages)
            }
            variant.instrumentation.transformClassesWith(
                JankHunterLifecycleClassVisitorFactory::class.java,
                if (applicationProject) InstrumentationScope.ALL else InstrumentationScope.PROJECT,
            ) { params ->
                params.enabled.set(extension.instrument.lifecycleLeaks)
                params.instrumentationDiagnosticsDirectory.set(
                    diagnosticsDirectory.map { directory ->
                        directory.dir("lifecycle").asFile.absolutePath
                    },
                )
                params.asmProgressLog.set(extension.instrument.asmProgressLog)
                params.progressLabel.set(project.progressLabel("${variant.name}:lifecycle"))
                params.includePackages.set(effectiveIncludePackages)
                params.excludePackages.set(effectiveExcludePackages)
            }
            variant.instrumentation.setAsmFramesComputationMode(
                FramesComputationMode.COMPUTE_FRAMES_FOR_INSTRUMENTED_METHODS,
            )

            if (extension.verboseLogs.getOrElse(false)) {
                project.logger.lifecycle(
                    "Jank Hunter variant {} configured. " +
                        "methodCounters={} okhttp={} webSockets={} handlers={} executors={} coroutines={} " +
                        "flowInteractions={} lifecycleLeaks={} logSpam={} classGraph={} runtimeCallGraph={} " +
                        "dependencyInjectionAnalysis={} " +
                        "asmProgressLog={} autoInit={} " +
                        "retainedHeapDump={} retainedHeapDumpMinIntervalMs={} retainedHeapDumpMaxCount={} " +
                        "retainedHeapDumpMinRetainedAgeMs={} instrumentationScope={} generatedRuntimeManifest={} " +
                        "sessionLogSizeLimitEnabled={} maxSessionLogSizeMiB={} symbolMode={} " +
                        "ownerMapTask={} mergeArtifactsTask={}",
                    variant.name,
                    extension.instrument.methodCounters.get(),
                    extension.instrument.okhttp.get(),
                    extension.instrument.webSockets.get(),
                    extension.instrument.handlers.get(),
                    extension.instrument.executors.get(),
                    extension.instrument.coroutines.get(),
                    extension.instrument.flowInteractions.get(),
                    extension.instrument.lifecycleLeaks.get(),
                    extension.instrument.logSpam.get(),
                    extension.instrument.classGraph.get(),
                    extension.instrument.runtimeCallGraph.get(),
                    extension.dependencyInjectionAnalysis.get(),
                    extension.instrument.asmProgressLog.get(),
                    extension.autoInit.get(),
                    extension.retainedHeapDump.enabled.get(),
                    extension.retainedHeapDump.minIntervalMs.get(),
                    extension.retainedHeapDump.maxCount.get(),
                    extension.retainedHeapDump.minRetainedAgeMs.get(),
                    effectiveInstrumentationScope,
                    shouldGenerateRuntimeManifest,
                    extension.sessionLogSizeLimitEnabled.get(),
                    maxSessionLogSizeMiB,
                    extension.symbolMode.get(),
                    ownerMap.name,
                    mergeArtifacts.name,
                )
            }
        }
    }

    private fun JankHunterExtension.isVariantEnabled(variantName: String): Boolean {
        return VariantBuildTypeMatcher.isEnabled(
            variantName = variantName,
            enabledBuildTypes = enabledBuildTypes.getOrElse(emptySet()),
            pluginEnabled = enabled.getOrElse(true),
        )
    }

    internal fun configureBuildBanner(project: Project, variantName: String) {
        val bannerService = project.gradle.sharedServices.registerIfAbsent(
            BUILD_BANNER_SERVICE_NAME,
            JankHunterBuildBannerService::class.java,
        ) {
            it.parameters.versionName.set(JankHunterDependencyCoordinates.load().version)
        }
        val capitalizedVariant = variantName.capitalized()
        val bannerTask = project.tasks.register(
            "print${capitalizedVariant}JankHunterBuildBanner",
            PrintJankHunterBuildBannerTask::class.java,
        ) {
            it.bannerService.set(bannerService)
            it.usesService(bannerService)
        }
        project.tasks.matching { it.name == "pre${capitalizedVariant}Build" }.configureEach {
            it.dependsOn(bannerTask)
        }
    }

    private fun JankHunterExtension.Instrumentation.hasRuntimeHooksEnabled(): Boolean {
        return methodCounters.getOrElse(false) ||
            okhttp.getOrElse(false) ||
            webSockets.getOrElse(false) ||
            handlers.getOrElse(false) ||
            executors.getOrElse(false) ||
            coroutines.getOrElse(false) ||
            flowInteractions.getOrElse(false) ||
            lifecycleLeaks.getOrElse(false) ||
            logSpam.getOrElse(false) ||
            // Annotation context boundaries are emitted while classGraph scans methods.
            classGraph.getOrElse(false) ||
            runtimeCallGraph.getOrElse(false)
    }

    private fun String.capitalized(): String {
        return replaceFirstChar {
            if (it.isLowerCase()) it.titlecase(Locale.US) else it.toString()
        }
    }

    private fun Project.progressLabel(variantName: String): String {
        return if (path == ":") ":$variantName" else "$path:$variantName"
    }

    private fun validatedPositiveMiB(name: String, value: Int): Int {
        if (value > 0) return value
        throw GradleException("$name must be greater than zero, but was $value.")
    }

    private fun validateReleaseSafety(project: Project, extension: JankHunterExtension, variantName: String) {
        val safety = extension.releaseSafety
        if (!safety.allowInstrumentation.getOrElse(false)) {
            throw GradleException(
                "Jank Hunter instrumentation is enabled for release-like variant '$variantName'. " +
                    "Set jankHunter.releaseSafety.allowInstrumentation=true after an explicit release review.",
            )
        }
        if (!safety.privacyReviewed.getOrElse(false)) {
            throw GradleException(
                "Jank Hunter release instrumentation for '$variantName' requires " +
                    "jankHunter.releaseSafety.privacyReviewed=true.",
            )
        }
        validatePerformanceBudget(project, safety.performanceBudgetEvidence.orNull, variantName)
        if (
            extension.retainedHeapDump.enabled.getOrElse(false) &&
            !extension.retainedHeapDump.privacyApproved.getOrElse(false)
        ) {
            throw GradleException(
                "retainedHeapDump.enabled=true for '$variantName' requires " +
                    "jankHunter.retainedHeapDump.privacyApproved=true.",
            )
        }
        if (extension.retainedHeapDump.enabled.getOrElse(false) && !safety.allowHeapDumps.getOrElse(false)) {
            throw GradleException(
                "retainedHeapDump.enabled=true for release-like variant '$variantName' requires " +
                    "jankHunter.releaseSafety.allowHeapDumps=true.",
            )
        }
        if (
            !extension.runtime.mainProcessOnly.getOrElse(true) &&
            !safety.allowSecondaryProcesses.getOrElse(false)
        ) {
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
        private const val BUILD_BANNER_SERVICE_NAME = "io.jankhunter.build-banner"
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
