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
        val diagnosticTasks = JankHunterDiagnosticTaskRegistry.register(project)

        project.pluginManager.withPlugin("com.android.application") {
            JankHunterAutomaticDependencies.addAnnotations(project)
            configureAndroidProject(
                project,
                extension,
                diagnosticTasks,
                applicationProject = true,
            )
        }
        project.pluginManager.withPlugin("com.android.library") {
            JankHunterAutomaticDependencies.addAnnotations(project)
            configureAndroidProject(
                project,
                extension,
                diagnosticTasks,
                applicationProject = false,
            )
        }
    }

    private fun configureAndroidProject(
        project: Project,
        extension: JankHunterExtension,
        diagnosticTasks: JankHunterDiagnosticTaskRegistry,
        applicationProject: Boolean,
    ) {
        val androidComponents = project.extensions.findByType(AndroidComponentsExtension::class.java)
        if (androidComponents == null) {
            project.logger.warn("Jank Hunter could not find AndroidComponentsExtension.")
            return
        }

        androidComponents.onVariants { variant ->
            val identity = JankHunterVariantIdentity(
                name = variant.name,
                buildType = variant.buildType.orEmpty(),
                productFlavors = variant.productFlavors.toMap(),
            )
            if (!extension.isEnabledFor(identity)) return@onVariants
            val configuration = extension.resolve(identity)
            val runtime = configuration.runtime
            val instrumentation = configuration.instrumentation
            val retainedHeapDump = configuration.retainedHeapDump
            val validationTask = diagnosticTasks.registerVariant(
                project,
                configuration,
                extension.requiresDebugReleaseParity(identity),
            )
            if (applicationProject) {
                JankHunterAutomaticDependencies.addRuntime(project, variant.name)
                configureBuildBanner(project, variant.name)
            }
            val storage = configuration.storage
            val sessionLogSizeLimitEnabled = storage is JankHunterStorage.Limited
            val maxSessionLogSizeMiB = (storage as? JankHunterStorage.Limited)?.maxSessionMiB ?: 50
            val symbolNamespace = JankHunterSymbolNamespace.current()
            val effectiveInstrumentationScope = instrumentationScope(applicationProject)
            val shouldGenerateRuntimeManifest = applicationProject
            if (shouldGenerateRuntimeManifest) {
                val runtimeManifest = project.tasks.register(
                    "generate${variant.name.capitalized()}JankHunterRuntimeManifest",
                    GenerateJankHunterRuntimeManifestTask::class.java,
                ) {
                    it.dependsOn(validationTask)
                    it.autoInit.set(configuration.autoInit)
                    it.mainThreadStallThresholdMs.set(runtime.mainThreadStallThresholdMs)
                    it.ownerBlockThresholdMs.set(runtime.ownerBlockThresholdMs)
                    it.httpSlowThresholdMs.set(runtime.httpSlowThresholdMs)
                    it.mainLooperDispatchMonitorEnabled.set(runtime.mainLooperDispatchMonitor)
                    it.retainedHeapDumpEnabled.set(retainedHeapDump.enabled)
                    it.retainedHeapDumpMinIntervalMs.set(retainedHeapDump.minIntervalMs)
                    it.retainedHeapDumpMaxCount.set(retainedHeapDump.maxCount)
                    it.retainedHeapDumpMinRetainedAgeMs.set(retainedHeapDump.minRetainedAgeMs)
                    it.jankStatsEnabled.set(runtime.jankStats)
                    it.ioTracingEnabled.set(runtime.ioTracing)
                    it.jankFrameThresholdMs.set(runtime.jankFrameThresholdMs)
                    it.uiWindowP95ThresholdMs.set(runtime.uiWindowP95ThresholdMs)
                    it.exactEventCollectionEnabled.set(configuration.collection == JankHunterCollection.EXACT)
                    it.maxQueueSize.set(configuration.maxQueueSize)
                    it.mainThreadAdmissionWaitMs.set(runtime.mainThreadAdmissionWaitMs)
                    it.backgroundAdmissionWaitMs.set(runtime.backgroundAdmissionWaitMs)
                    it.runtimeCallGraphEnabled.set(instrumentation.runtimeCallGraph)
                    it.availableRuntimeFeatures.set(
                        configuration.features.asSequence()
                            .map(JankHunterFeature::name)
                            .sorted()
                            .joinToString(","),
                    )
                    it.composeTracingEnabled.set(instrumentation.composeTracing)
                    it.roomTracingEnabled.set(instrumentation.roomTracing)
                    it.databaseTracingEnabled.set(instrumentation.databaseTracing)
                    it.workerTracingEnabled.set(instrumentation.workerTracing)
                    it.mainProcessOnly.set(configuration.processes == JankHunterProcesses.MAIN_ONLY)
                    it.sessionLogSizeLimitEnabled.set(sessionLogSizeLimitEnabled)
                    it.maxSessionLogSizeMiB.set(maxSessionLogSizeMiB)
                    it.logGrowthAnalyticsEnabled.set(configuration.growthAnalytics)
                    it.deleteObsoleteJhlogFormats.set(configuration.deleteObsoleteLogs)
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
            val classGraphDirectory = artifactRoot.map { it.dir("class-graph") }
            val diagnosticsDirectory = artifactRoot.map { it.dir("diagnostics") }
            val dependencyInjectionCatalogDirectory = artifactRoot.map { it.dir("dependency-injection") }
            val androidComponentCatalogDirectory = artifactRoot.map { it.dir("android-components") }
            val dependencyInjectionAnalysisEnabled = instrumentation.dependencyInjectionAnalysis
            val runtimeHooksEnabled = instrumentation.hasRuntimeHooks
            val includeWholeApplication = instrumentation.includeWholeApplication
            val manualIncludes = configuration.includePackages
            val effectiveExcludePackages = InstrumentationPackages.normalizedPackages(
                configuration.excludePackages,
            )
            val androidNamespace = variant.namespace.orElse("")
            val effectiveIncludePackages = androidNamespace.map { namespace ->
                val includes = InstrumentationPackages.effectiveIncludes(
                    manualIncludes,
                    namespace.takeIf { instrumentation.includeAndroidNamespace },
                )
                if (includes.isEmpty() && !includeWholeApplication) {
                    throw GradleException(
                        "Jank Hunter cannot determine a safe instrumentation boundary for variant " +
                            "'${variant.name}'. Set the Android namespace or add " +
                            "jankHunter.packages(...).",
                    )
                }
                includes
            }
            val artifactMetadata = project.tasks.register(
                "generate${variant.name.capitalized()}JankHunterArtifactMetadata",
                GenerateJankHunterArtifactMetadataTask::class.java,
            ) {
                it.variantName.set(variant.name)
                it.methodCounters.set(instrumentation.methodCounters)
                it.okhttp.set(instrumentation.okhttp)
                it.webSockets.set(instrumentation.webSockets)
                it.handlers.set(instrumentation.handlers)
                it.executors.set(instrumentation.executors)
                it.coroutines.set(instrumentation.coroutines)
                it.interactionOperations.set(instrumentation.interactionOperations)
                it.lifecycleLeaks.set(instrumentation.lifecycleLeaks)
                it.logSpam.set(instrumentation.logSpam)
                it.classGraph.set(instrumentation.classGraph)
                it.runtimeCallGraph.set(instrumentation.runtimeCallGraph)
                it.symbolNamespace.set(symbolNamespace)
                it.includeWholeApplication.set(instrumentation.includeWholeApplication)
                it.networkWholeApplication.set(instrumentation.networkWholeApplication)
                it.databaseWholeApplication.set(instrumentation.databaseWholeApplication)
                it.databaseTracing.set(instrumentation.databaseTracing)
                it.ioTracing.set(instrumentation.ioTracing)
                it.androidNamespace.set(androidNamespace)
                it.includePackages.set(effectiveIncludePackages)
                it.excludePackages.set(effectiveExcludePackages)
                it.outputFile.set(
                    project.layout.buildDirectory.file(
                        "generated/jankhunter/${variant.name}/artifact-metadata.json",
                    ),
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
            val androidComponentCatalogOutput = project.layout.buildDirectory.file(
                "generated/jankhunter/${variant.name}/android-components-catalog.jsonl",
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
                it.androidComponentCatalogDirectory.set(androidComponentCatalogDirectory)
                it.androidComponentCatalogFiles.from(androidComponentCatalogDirectory.map { directory ->
                    directory.asFileTree.matching { pattern ->
                        pattern.include("**/*.jsonl")
                    }
                })
                it.classGraphOutputFile.set(classGraphOutput)
                it.diagnosticsOutputFile.set(instrumentationDiagnosticsOutput)
                it.androidComponentCatalogOutputFile.set(androidComponentCatalogOutput)
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
                hooksEnabled = instrumentation.okhttp || instrumentation.webSockets,
            )
            project.tasks.matching { it.name == "assemble${variant.name.capitalized()}" }.configureEach {
                it.finalizedBy(artifactMetadata)
                it.finalizedBy(mergeArtifacts)
                it.finalizedBy(mergeDependencyInjectionCatalog)
            }
            project.tasks.matching { it.name == "transform${variant.name.capitalized()}ClassesWithAsm" }.configureEach {
                it.finalizedBy(artifactMetadata)
                it.finalizedBy(mergeArtifacts)
                it.finalizedBy(mergeDependencyInjectionCatalog)
            }

            variant.instrumentation.transformClassesWith(
                JankHunterClassVisitorFactory::class.java,
                effectiveInstrumentationScope,
            ) { params ->
                params.autoInit.set(configuration.autoInit)
                params.dependencyInjectionAnalysis.set(dependencyInjectionAnalysisEnabled)
                params.methodCounters.set(instrumentation.methodCounters)
                params.methodFilterMode.set(instrumentation.methodFilterMode)
                params.okhttp.set(instrumentation.okhttp)
                params.webSockets.set(instrumentation.webSockets)
                params.okHttpHelperAvailable.set(okHttpHelperAvailable)
                params.handlers.set(instrumentation.handlers)
                params.executors.set(instrumentation.executors)
                params.coroutines.set(instrumentation.coroutines)
                params.interactionOperations.set(instrumentation.interactionOperations)
                params.lifecycleLeaks.set(false)
                params.logSpam.set(instrumentation.logSpam)
                params.classGraph.set(instrumentation.classGraph)
                params.runtimeCallGraph.set(instrumentation.runtimeCallGraph)
                params.composeTracing.set(instrumentation.composeTracing)
                params.roomTracing.set(instrumentation.roomTracing)
                params.databaseTracing.set(instrumentation.databaseTracing)
                params.workerTracing.set(instrumentation.workerTracing)
                params.androidComponents.set(instrumentation.androidComponents)
                params.binderIPC.set(instrumentation.binderIPC)
                params.ioTracing.set(instrumentation.ioTracing)
                params.classGraphDirectory.set(classGraphDirectory.map { it.asFile.absolutePath })
                params.instrumentationDiagnosticsDirectory.set(
                    diagnosticsDirectory.map { it.asFile.absolutePath },
                )
                params.androidComponentCatalogDirectory.set(
                    androidComponentCatalogDirectory.map { it.asFile.absolutePath },
                )
                params.dependencyInjectionCatalogDirectory.set(
                    dependencyInjectionCatalogDirectory.map { it.asFile.absolutePath },
                )
                params.includeWholeApplication.set(instrumentation.includeWholeApplication)
                params.networkWholeApplication.set(instrumentation.networkWholeApplication)
                params.databaseWholeApplication.set(instrumentation.databaseWholeApplication)
                params.includePackages.set(effectiveIncludePackages)
                params.excludePackages.set(effectiveExcludePackages)
            }
            variant.instrumentation.transformClassesWith(
                JankHunterLifecycleClassVisitorFactory::class.java,
                effectiveInstrumentationScope,
            ) { params ->
                params.enabled.set(instrumentation.lifecycleLeaks)
                params.instrumentationDiagnosticsDirectory.set(
                    diagnosticsDirectory.map { directory ->
                        directory.dir("lifecycle").asFile.absolutePath
                    },
                )
                params.includeWholeApplication.set(instrumentation.includeWholeApplication)
                params.includePackages.set(effectiveIncludePackages)
                params.excludePackages.set(effectiveExcludePackages)
            }
            variant.instrumentation.setAsmFramesComputationMode(
                FramesComputationMode.COMPUTE_FRAMES_FOR_INSTRUMENTED_METHODS,
            )

            project.logger.info(
                "Jank Hunter variant {} configured: profile={}, features={}, collection={}, " +
                    "processes={}, maxQueueSize={}, storage={}, growthAnalytics={}, " +
                    "instrumentationScope={}, artifactMetadataTask={}, mergeArtifactsTask={}",
                variant.name,
                configuration.profile,
                configuration.features.sortedBy(JankHunterFeature::ordinal),
                configuration.collection,
                configuration.processes,
                configuration.maxQueueSize,
                configuration.storage,
                configuration.growthAnalytics,
                effectiveInstrumentationScope,
                artifactMetadata.name,
                mergeArtifacts.name,
            )
        }
    }

    internal fun instrumentationScope(applicationProject: Boolean): InstrumentationScope {
        return if (applicationProject) InstrumentationScope.ALL else InstrumentationScope.PROJECT
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

    private fun String.capitalized(): String {
        return replaceFirstChar {
            if (it.isLowerCase()) it.titlecase(Locale.US) else it.toString()
        }
    }

    private companion object {
        private const val BUILD_BANNER_SERVICE_NAME = "io.jankhunter.build-banner"
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
