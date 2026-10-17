package io.jankhunter.gradle

import com.android.build.api.instrumentation.FramesComputationMode
import com.android.build.api.instrumentation.InstrumentationScope
import com.android.build.api.variant.AndroidComponentsExtension
import com.android.build.api.variant.ApplicationVariant
import com.android.build.api.artifact.SingleArtifact
import com.android.build.api.artifact.ScopedArtifact
import com.android.build.api.variant.ScopedArtifacts
import com.android.build.gradle.internal.publishing.AndroidArtifacts
import org.gradle.api.GradleException
import org.gradle.api.Plugin
import org.gradle.api.Project
import org.gradle.api.artifacts.component.ModuleComponentIdentifier
import org.gradle.api.artifacts.component.ProjectComponentIdentifier
import java.util.Locale
class JankHunterPlugin : Plugin<Project> {
    override fun apply(project: Project) {
        val readerLifetime = project.gradle.sharedServices.registerIfAbsent("jankHunterMetadataReader", MetadataReaderLifetime::class.java) {}
        project.tasks.configureEach { it.usesService(readerLifetime) }
        // usesService declares ownership; resolving a scalar input also instantiates the lazy service.
        val readerIdentity = readerLifetime.map { MetadataReaderCapabilities.identity }
        val extension = project.extensions.create("jankHunter", JankHunterExtension::class.java)
        val diagnosticTasks = JankHunterDiagnosticTaskRegistry.register(project)

        project.pluginManager.withPlugin("com.android.application") {
            JankHunterAutomaticDependencies.addAnnotations(project)
            configureAndroidProject(
                project,
                extension,
                diagnosticTasks,
                applicationProject = true,
                readerIdentity = readerIdentity,
            )
        }
        project.pluginManager.withPlugin("com.android.library") {
            JankHunterAutomaticDependencies.addAnnotations(project)
            configureAndroidProject(
                project,
                extension,
                diagnosticTasks,
                applicationProject = false,
                readerIdentity = readerIdentity,
            )
        }
    }

    private fun configureAndroidProject(
        project: Project,
        extension: JankHunterExtension,
        diagnosticTasks: JankHunterDiagnosticTaskRegistry,
        applicationProject: Boolean,
        readerIdentity: org.gradle.api.provider.Provider<String>,
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
            val sdkGroup = JankHunterDependencyCoordinates.load().group
            val sdkModules = setOf("jankhunter-runtime", "jankhunter-okhttp3", "jankhunter-workmanager")
            val sdkCompileFiles = variant.compileConfiguration.incoming.artifactView { view ->
                view.attributes.attribute(AndroidArtifacts.ARTIFACT_TYPE, AndroidArtifacts.ArtifactType.CLASSES_JAR.type)
                view.componentFilter { component ->
                    component is ModuleComponentIdentifier && component.group == sdkGroup && component.module in sdkModules ||
                        component is ProjectComponentIdentifier && component.projectName in sdkModules
                }
            }.files
            val sdkRuntimeFiles = variant.runtimeConfiguration.incoming.artifactView { view ->
                view.attributes.attribute(AndroidArtifacts.ARTIFACT_TYPE, AndroidArtifacts.ArtifactType.CLASSES_JAR.type)
                view.componentFilter { component ->
                    component is ModuleComponentIdentifier && component.group == sdkGroup && component.module in sdkModules ||
                        component is ProjectComponentIdentifier && component.projectName in sdkModules
                }
            }.files
            val requiredHookOwners = HookAbiRequirements.requiredOwners(instrumentation, configuration.autoInit)
            val sdkAbi = project.tasks.register("validate${variant.name.capitalized()}JankHunterHookAbi", ValidateJankHunterHookAbiTask::class.java) { task ->
                task.compileSdk.from(sdkCompileFiles)
                task.runtimeSdk.from(sdkRuntimeFiles)
                task.requireSdk.set(applicationProject && (instrumentation.hasRuntimeHooks || configuration.autoInit))
                task.requiredOwners.set(requiredHookOwners)
                task.outputFile.set(project.layout.buildDirectory.file("intermediates/jankhunter/${variant.name}/sdk-hook-abi.bin"))
            }
            val sdkAbiIndex = sdkAbi.flatMap(ValidateJankHunterHookAbiTask::outputFile).map { HookAbiIndex.read(it.asFile) }
            // AGP may isolate ASM parameters while a different project artifact is transformed.
            // Materialize project SDK outputs before those consumers inspect the SDK classpath.
            project.tasks.matching { it.name == "pre${variant.name.capitalized()}Build" }.configureEach {
                it.dependsOn(sdkAbi)
            }
            val storage = configuration.storage
            val sessionLogSizeLimitEnabled = storage is JankHunterStorage.Limited
            val maxSessionLogSizeMiB = (storage as? JankHunterStorage.Limited)?.maxSessionMiB ?: 50
            val symbolNamespace = JankHunterSymbolNamespace.current()
            val buildIdentity = if (variant is ApplicationVariant) {
                val task = project.tasks.register(
                    "generate${variant.name.capitalized()}JankHunterBuildIdentity",
                    GenerateJankHunterBuildIdentityTask::class.java,
                ) { task ->
                    task.minified.set(variant.isMinifyEnabled)
                    task.symbolNamespace.set(symbolNamespace)
                    if (variant.isMinifyEnabled) task.mappingFile.set(variant.artifacts.get(SingleArtifact.OBFUSCATION_MAPPING_FILE))
                    task.assetsDirectory.set(project.layout.buildDirectory.dir("generated/jankhunter/${variant.name}/identity-assets"))
                }
                variant.sources.assets?.addGeneratedSourceDirectory(task, GenerateJankHunterBuildIdentityTask::assetsDirectory)
                task
            } else null
            if (applicationProject && instrumentation.workerTracing) {
                val catalog = project.tasks.register(
                    "generate${variant.name.capitalized()}JankHunterWorkerCatalog",
                    GenerateJankHunterWorkerCatalogTask::class.java,
                ) { task ->
                    task.symbolNamespace.set(symbolNamespace)
                    task.assetsDirectory.set(project.layout.buildDirectory.dir("generated/jankhunter/${variant.name}/worker-assets"))
                }
                variant.artifacts.forScope(ScopedArtifacts.Scope.ALL).use(catalog).toGet(
                    ScopedArtifact.CLASSES,
                    GenerateJankHunterWorkerCatalogTask::inputJars,
                    GenerateJankHunterWorkerCatalogTask::inputDirectories,
                )
                variant.sources.assets?.addGeneratedSourceDirectory(catalog, GenerateJankHunterWorkerCatalogTask::assetsDirectory)
            }
            val effectiveInstrumentationScope = instrumentationScope(applicationProject)
            val shouldGenerateRuntimeManifest = applicationProject
            if (shouldGenerateRuntimeManifest) {
                val runtimeManifest = project.tasks.register(
                    "generate${variant.name.capitalized()}JankHunterRuntimeManifest",
                    GenerateJankHunterRuntimeManifestTask::class.java,
                ) {
                    it.dependsOn(validationTask)
                    it.autoInit.set(configuration.autoInit)
                    it.threadSamplerEnabled.set(runtime.threadSamplerEnabled)
                    it.threadSampleIntervalMs.set(runtime.threadSampleIntervalMs)
                    it.concurrencyDetailedEnabled.set(runtime.concurrencyDetailedEnabled)
                    it.concurrencyMaxTrackedEntities.set(runtime.concurrencyMaxTrackedEntities)
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
                if (com.android.build.api.variant.Sources::class.java.methods.any { it.name == "getManifests" }) {
                    variant.sources.manifests.addGeneratedManifestFile(runtimeManifest, GenerateJankHunterRuntimeManifestTask::outputFile)
                } else {
                    val legacyManifest = project.tasks.register("merge${variant.name.capitalized()}JankHunterRuntimeManifest", LegacyRuntimeManifestTask::class.java) {
                        it.runtimeManifest.set(runtimeManifest.flatMap { task -> task.outputFile })
                        it.applicationId.set((variant as ApplicationVariant).applicationId)
                    }
                    variant.artifacts.use(legacyManifest).wiredWithFiles(LegacyRuntimeManifestTask::inputManifest,
                        LegacyRuntimeManifestTask::outputManifest).toTransform(SingleArtifact.MERGED_MANIFEST)
                }
            }

            val artifactRoot = project.layout.buildDirectory.dir(
                ArtifactSchemas.instrumentationArtifactsPath(variant.name),
            )
            val classGraphDirectory = artifactRoot.map { it.dir("class-graph") }
            val lambdaCaptureDirectory = artifactRoot.map { it.dir("lambda-captures") }
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
                it.threadLifecycle.set(instrumentation.threadLifecycle)
                it.concurrencyDiagnostics.set(instrumentation.concurrencyDiagnostics)
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
                it.metadataCompatibilityMode.set(instrumentation.metadataCompatibilityMode)
                it.metadataReaderIdentity.set(readerIdentity)
                it.outputFile.set(
                    project.layout.buildDirectory.file(
                        "generated/jankhunter/${variant.name}/artifact-metadata.json",
                    ),
                )
            }

            val classGraphOutput = project.layout.buildDirectory.file(
                "generated/jankhunter/${variant.name}/class-graph.jsonl",
            )
            val lambdaCaptureOutput = project.layout.buildDirectory.file(
                "generated/jankhunter/${variant.name}/lambda-captures.jsonl",
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
                it.lambdaCaptureDirectory.set(lambdaCaptureDirectory)
                it.diagnosticsDirectory.set(diagnosticsDirectory)
                it.metadataCompatibilityOutputFile.set(project.layout.buildDirectory.file(
                    "generated/jankhunter/${variant.name}/metadata-compatibility.json",
                ))
                it.androidComponentCatalogDirectory.set(androidComponentCatalogDirectory)
                it.classGraphOutputFile.set(classGraphOutput)
                it.lambdaCaptureOutputFile.set(lambdaCaptureOutput)
                it.diagnosticsOutputFile.set(instrumentationDiagnosticsOutput)
                it.dependencyInjectionRecordsFile.set(project.layout.buildDirectory.file(
                    "intermediates/jankhunter/${variant.name}/di-records.jsonl"))
                it.androidComponentCatalogOutputFile.set(androidComponentCatalogOutput)
            }
            val mergeDependencyInjectionCatalog = project.tasks.register(
                "merge${variant.name.capitalized()}JankHunterDependencyInjectionCatalog",
                MergeJankHunterDependencyInjectionCatalogTask::class.java,
            ) {
                it.analysisEnabled.set(dependencyInjectionAnalysisEnabled)
                it.variantName.set(variant.name)
                it.shardsDirectory.set(dependencyInjectionCatalogDirectory)
                it.shardFiles.from(mergeArtifacts.flatMap { task -> task.dependencyInjectionRecordsFile })
                it.outputFile.set(dependencyInjectionCatalogOutput)
            }
            if (variant is ApplicationVariant && buildIdentity != null) {
                for (kind in listOf("apk", "aab")) {
                    val manifest = project.tasks.register(
                        "generate${variant.name.capitalized()}JankHunter${kind.capitalized()}BuildManifest",
                        GenerateJankHunterBuildManifestTask::class.java,
                    ) { task ->
                        task.variantName.set(variant.name)
                        task.packageKind.set(kind)
                        task.symbolNamespace.set(symbolNamespace)
                        task.identityAsset.set(buildIdentity.flatMap { it.assetsDirectory.file(GenerateJankHunterBuildIdentityTask.ASSET_PATH) })
                        if (variant.isMinifyEnabled) task.mappingFile.set(variant.artifacts.get(SingleArtifact.OBFUSCATION_MAPPING_FILE))
                        task.artifacts.from(
                            artifactMetadata.flatMap { it.outputFile },
                            mergeArtifacts.flatMap { it.classGraphOutputFile },
                            mergeArtifacts.flatMap { it.lambdaCaptureOutputFile },
                            mergeArtifacts.flatMap { it.diagnosticsOutputFile },
                            mergeArtifacts.flatMap { it.androidComponentCatalogOutputFile },
                        )
                        if (dependencyInjectionAnalysisEnabled) {
                            task.artifacts.from(mergeDependencyInjectionCatalog.flatMap { it.outputFile })
                        }
                        task.outputFile.set(project.layout.buildDirectory.file("generated/jankhunter/${variant.name}/build-manifest-$kind.json"))
                    }
                    // Follow the actual artifact producer, including direct package tasks and
                    // downstream transforms, without depending on AGP's internal task names.
                    val supportsListener = com.android.build.api.artifact.OutOperationRequest::class.java.methods.any { it.name == "toListenTo" }
                    if (!supportsListener && kind == "apk") {
                        variant.artifacts.use(manifest).wiredWithDirectories(GenerateJankHunterBuildManifestTask::apkDirectory,
                            GenerateJankHunterBuildManifestTask::copiedApkDirectory).toTransform(SingleArtifact.APK)
                    } else if (!supportsListener) {
                        variant.artifacts.use(manifest).wiredWithFiles(GenerateJankHunterBuildManifestTask::bundleFile,
                            GenerateJankHunterBuildManifestTask::copiedBundleFile).toTransform(SingleArtifact.BUNDLE)
                    } else if (kind == "apk") {
                        variant.artifacts.use(manifest)
                            .wiredWith(GenerateJankHunterBuildManifestTask::apkDirectory).toListenTo(SingleArtifact.APK)
                    } else {
                        variant.artifacts.use(manifest)
                            .wiredWith(GenerateJankHunterBuildManifestTask::bundleFile).toListenTo(SingleArtifact.BUNDLE)
                    }
                }
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
            if (!applicationProject || com.android.build.api.artifact.OutOperationRequest::class.java.methods.any { it.name == "toListenTo" }) {
                project.tasks.matching { it.name == "assemble${variant.name.capitalized()}" }.configureEach {
                    it.finalizedBy(artifactMetadata)
                    it.finalizedBy(mergeArtifacts)
                    it.finalizedBy(mergeDependencyInjectionCatalog)
                }
            }
            project.tasks.matching { it.name == "transform${variant.name.capitalized()}ClassesWithAsm" }.configureEach {
                it.finalizedBy(artifactMetadata)
                it.finalizedBy(mergeArtifacts)
                it.finalizedBy(mergeDependencyInjectionCatalog)
            }

            if (instrumentation.okhttp && okHttpHelperAvailable) {
                val capability = project.tasks.register(
                    "resolve${variant.name.capitalized()}JankHunterTransportCapability",
                    ResolveJankHunterTransportCapabilityTask::class.java,
                ) { task ->
                    task.supportClasspath.from(variant.compileConfiguration.incoming.artifactView {
                        it.attributes.attribute(AndroidArtifacts.ARTIFACT_TYPE, AndroidArtifacts.ArtifactType.CLASSES_JAR.type)
                    }.files)
                    task.outputFile.set(project.layout.buildDirectory.file(
                        "intermediates/jankhunter/${variant.name}/transport-capability.txt",
                    ))
                }
                val transport = project.tasks.register(
                    "instrument${variant.name.capitalized()}JankHunterTransport",
                    InstrumentJankHunterTransportTask::class.java,
                ) {
                    it.capabilityFile.set(capability.flatMap { task -> task.outputFile })
                    it.excludePackages.set(effectiveExcludePackages)
                }
                variant.artifacts.forScope(
                    if (applicationProject) ScopedArtifacts.Scope.ALL else ScopedArtifacts.Scope.PROJECT,
                ).use(transport).toTransform(
                    ScopedArtifact.CLASSES,
                    InstrumentJankHunterTransportTask::inputJars,
                    InstrumentJankHunterTransportTask::inputDirectories,
                    InstrumentJankHunterTransportTask::outputFile,
                )
                transport.configure { task ->
                    task.outputFile.set(project.layout.buildDirectory.file(
                        "intermediates/classes/${variant.name}/${if (applicationProject) "ALL" else "PROJECT"}/${task.name}/classes.jar",
                    ))
                }
            }

            val codeOriginCatalog = if (applicationProject && instrumentation.threadLifecycle) {
                val prepass = project.tasks.register(
                    "capture${variant.name.capitalized()}JankHunterProjectCodeOrigin",
                    CausalProvenancePrepassTask::class.java,
                ) { task ->
                    task.projectClasses.set(project.layout.buildDirectory.file(
                        "intermediates/jankhunter/${variant.name}/project-classes.txt",
                    ))
                }
                variant.artifacts.forScope(ScopedArtifacts.Scope.PROJECT).use(prepass).toTransform(
                    ScopedArtifact.CLASSES,
                    CausalProvenancePrepassTask::inputJars,
                    CausalProvenancePrepassTask::inputDirectories,
                    CausalProvenancePrepassTask::outputFile,
                )
                prepass.configure { task ->
                    task.outputFile.set(project.layout.buildDirectory.file(
                        "intermediates/classes/${variant.name}/PROJECT/${task.name}/classes.jar",
                    ))
                }
                project.tasks.register(
                    "generate${variant.name.capitalized()}JankHunterCodeOriginCatalog",
                    GenerateCausalCodeOriginCatalogTask::class.java,
                ) { task ->
                    task.projectClasses.set(prepass.flatMap(CausalProvenancePrepassTask::projectClasses))
                    val classes = variant.runtimeConfiguration.incoming.artifactView { view ->
                        view.attributes.attribute(
                            AndroidArtifacts.ARTIFACT_TYPE,
                            AndroidArtifacts.ArtifactType.CLASSES_JAR.type,
                        )
                        view.componentFilter { it is ProjectComponentIdentifier }
                    }
                    task.localProjectArtifacts.from(classes.files)
                    task.externalArtifacts.from(variant.runtimeConfiguration.incoming.artifactView { view ->
                        view.attributes.attribute(
                            AndroidArtifacts.ARTIFACT_TYPE,
                            AndroidArtifacts.ArtifactType.CLASSES_JAR.type,
                        )
                        view.componentFilter { it is ModuleComponentIdentifier }
                    }.files)
                    task.unknownArtifacts.from(variant.runtimeConfiguration.incoming.artifactView { view ->
                        view.attributes.attribute(
                            AndroidArtifacts.ARTIFACT_TYPE,
                            AndroidArtifacts.ArtifactType.CLASSES_JAR.type,
                        )
                        view.componentFilter {
                            it !is ProjectComponentIdentifier && it !is ModuleComponentIdentifier
                        }
                    }.files)
                    task.outputFile.set(project.layout.buildDirectory.file(
                        "intermediates/jankhunter/${variant.name}/code-origin-v1.txt",
                    ))
                }
            } else null

            variant.instrumentation.transformClassesWith(
                JankHunterClassVisitorFactory::class.java,
                effectiveInstrumentationScope,
            ) { params ->
                params.sdkCompileFiles.from(sdkCompileFiles)
                params.sdkRuntimeFiles.from(sdkRuntimeFiles)
                params.sdkRequiredOwners.set(requiredHookOwners)
                params.requireSdk.set(applicationProject && (instrumentation.hasRuntimeHooks || configuration.autoInit))
                params.metadataCompatibilityMode.set(instrumentation.metadataCompatibilityMode)
                params.metadataReaderIdentity.set(readerIdentity)
                params.autoInit.set(configuration.autoInit)
                params.dependencyInjectionAnalysis.set(dependencyInjectionAnalysisEnabled)
                params.methodCounters.set(instrumentation.methodCounters)
                params.methodFilterMode.set(instrumentation.methodFilterMode)
                params.okhttp.set(instrumentation.okhttp)
                params.webSockets.set(instrumentation.webSockets)
                params.okHttpHelperAvailable.set(okHttpHelperAvailable)
                params.handlers.set(instrumentation.handlers && codeOriginCatalog == null)
                params.executors.set(instrumentation.executors && codeOriginCatalog == null)
                params.threadLifecycle.set(instrumentation.threadLifecycle && codeOriginCatalog == null)
                params.concurrencyDiagnostics.set(instrumentation.concurrencyDiagnostics && codeOriginCatalog == null)
                params.coroutines.set(instrumentation.coroutines && codeOriginCatalog == null)
                params.interactionOperations.set(instrumentation.interactionOperations)
                params.lifecycleLeaks.set(false)
                params.logSpam.set(instrumentation.logSpam)
                params.classGraph.set(instrumentation.classGraph)
                params.runtimeCallGraph.set(instrumentation.runtimeCallGraph)
                params.composeTracing.set(instrumentation.composeTracing)
                params.roomTracing.set(instrumentation.roomTracing)
                params.databaseTracing.set(instrumentation.databaseTracing)
                params.workerTracing.set(instrumentation.workerTracing && codeOriginCatalog == null)
                params.androidComponents.set(instrumentation.androidComponents)
                params.binderIPC.set(instrumentation.binderIPC)
                params.ioTracing.set(instrumentation.ioTracing)
                params.classGraphDirectory.set("")
                params.lambdaCaptureDirectory.set("")
                params.instrumentationDiagnosticsDirectory.set("")
                params.androidComponentCatalogDirectory.set("")
                params.dependencyInjectionCatalogDirectory.set("")
                params.includeWholeApplication.set(instrumentation.includeWholeApplication)
                params.networkWholeApplication.set(instrumentation.networkWholeApplication)
                params.databaseWholeApplication.set(instrumentation.databaseWholeApplication)
                params.includePackages.set(effectiveIncludePackages)
                params.excludePackages.set(effectiveExcludePackages)
            }
            if (codeOriginCatalog != null) {
                val causal = project.tasks.register(
                    "instrument${variant.name.capitalized()}JankHunterCausalProvenance",
                    CausalProvenanceTransformTask::class.java,
                ) { task ->
                    task.codeOriginCatalog.set(codeOriginCatalog.flatMap(GenerateCausalCodeOriginCatalogTask::outputFile))
                    task.sdkHookAbi.set(sdkAbiIndex)
                    task.sdkHookAbi.finalizeValueOnRead()
                    task.metadataCompatibilityMode.set(instrumentation.metadataCompatibilityMode)
                    task.metadataReaderIdentity.set(readerIdentity)
                    task.supportClasspath.from(androidComponents.sdkComponents.bootClasspath)
                    task.supportClasspath.from(variant.compileConfiguration.incoming.artifactView {
                        it.attributes.attribute(AndroidArtifacts.ARTIFACT_TYPE, AndroidArtifacts.ArtifactType.CLASSES_JAR.type)
                    }.files)
                    task.includeWholeApplication.set(instrumentation.includeWholeApplication)
                    task.includePackages.set(effectiveIncludePackages)
                    task.excludePackages.set(effectiveExcludePackages)
                    task.executors.set(instrumentation.executors)
                    task.handlers.set(instrumentation.handlers)
                    task.threadLifecycle.set(instrumentation.threadLifecycle)
                    task.concurrencyDiagnostics.set(instrumentation.concurrencyDiagnostics)
                    task.coroutines.set(instrumentation.coroutines)
                    task.workerTracing.set(instrumentation.workerTracing)
                    task.diagnosticsDirectory.set(project.layout.buildDirectory.dir(
                        "intermediates/jankhunter/${variant.name}/causal-diagnostics",
                    ))
                    task.outputFile.set(project.layout.buildDirectory.file(
                        "intermediates/jankhunter/${variant.name}/causal-provenance-classes.jar",
                    ))
                }
                variant.artifacts.forScope(ScopedArtifacts.Scope.ALL).use(causal).toTransform(
                    ScopedArtifact.CLASSES,
                    CausalProvenanceTransformTask::inputJars,
                    CausalProvenanceTransformTask::inputDirectories,
                    CausalProvenanceTransformTask::outputFile,
                )
                causal.configure { task ->
                    task.outputFile.set(project.layout.buildDirectory.file(
                        "intermediates/classes/${variant.name}/ALL/${task.name}/classes.jar",
                    ))
                }
            }
            if (instrumentation.lifecycleLeaks) {
                val lifecycle = project.tasks.register(
                    "instrument${variant.name.capitalized()}JankHunterLifecycle",
                    InstrumentJankHunterLifecycleTask::class.java,
                ) { task ->
                    task.sdkHookAbi.set(sdkAbiIndex)
                    task.sdkHookAbi.finalizeValueOnRead()
                    task.supportClasspath.from(androidComponents.sdkComponents.bootClasspath)
                    task.metadataCompatibilityMode.set(instrumentation.metadataCompatibilityMode)
                    task.metadataReaderIdentity.set(readerIdentity)
                    task.supportClasspath.from(variant.compileConfiguration.incoming.artifactView {
                        it.attributes.attribute(AndroidArtifacts.ARTIFACT_TYPE, AndroidArtifacts.ArtifactType.CLASSES_JAR.type)
                    }.files)
                    task.includeWholeApplication.set(instrumentation.includeWholeApplication)
                    task.validateRuntimeAbi.set(applicationProject)
                    task.includePackages.set(effectiveIncludePackages)
                    task.excludePackages.set(effectiveExcludePackages)
                    task.diagnosticsDirectory.set(project.layout.buildDirectory.dir(
                        "intermediates/jankhunter/${variant.name}/lifecycle-diagnostics",
                    ))
                }
                variant.artifacts.forScope(
                    if (applicationProject) ScopedArtifacts.Scope.ALL else ScopedArtifacts.Scope.PROJECT,
                ).use(lifecycle).toTransform(
                    ScopedArtifact.CLASSES,
                    InstrumentJankHunterLifecycleTask::inputJars,
                    InstrumentJankHunterLifecycleTask::inputDirectories,
                    InstrumentJankHunterLifecycleTask::outputFile,
                )
                lifecycle.configure { task ->
                    task.outputFile.set(project.layout.buildDirectory.file(
                        "intermediates/classes/${variant.name}/${if (applicationProject) "ALL" else "PROJECT"}/${task.name}/classes.jar",
                    ))
                }
            }
            variant.artifacts.forScope(if (applicationProject) ScopedArtifacts.Scope.ALL else ScopedArtifacts.Scope.PROJECT)
                .use(mergeArtifacts).toGet(ScopedArtifact.CLASSES,
                    MergeJankHunterInstrumentationArtifactsTask::inputJars,
                    MergeJankHunterInstrumentationArtifactsTask::inputDirectories)
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
