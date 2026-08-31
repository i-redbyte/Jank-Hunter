package io.jankhunter.gradle

import org.gradle.api.Project
import org.gradle.api.Task
import org.gradle.api.tasks.TaskProvider

internal class JankHunterDiagnosticTaskRegistry private constructor(
    private val printAll: TaskProvider<PrintJankHunterConfigurationTask>,
    private val validateAll: TaskProvider<Task>,
    private val releaseReadiness: TaskProvider<Task>,
    private val exportAll: TaskProvider<ExportJankHunterConfigurationTask>,
    private val functionalParity: TaskProvider<ValidateJankHunterFunctionalParityTask>,
) {
    private val parityCandidates = linkedMapOf<String, MutableMap<String, ParityCandidate>>()

    fun registerVariant(
        project: Project,
        configuration: JankHunterResolvedConfiguration,
        requiresDebugReleaseParity: Boolean,
    ): TaskProvider<ValidateJankHunterConfigurationTask> {
        val capitalizedVariant = configuration.variant.name.capitalized()
        val effective = JankHunterConfigurationReport.effective(configuration)
        val details = JankHunterConfigurationReport.details(configuration)
        val validation = project.tasks.register(
            "jankHunterValidate${capitalizedVariant}Configuration",
            ValidateJankHunterConfigurationTask::class.java,
        ) { task ->
            task.group = TASK_GROUP
            task.description = "Validates the effective Jank Hunter configuration for ${configuration.variant.name}."
            task.variantName.set(configuration.variant.name)
            task.failures.set(JankHunterConfigurationValidation.failures(configuration))
            task.warnings.set(JankHunterConfigurationValidation.warnings(configuration))
        }
        project.tasks.register(
            "jankHunterPrint${capitalizedVariant}Configuration",
            PrintJankHunterConfigurationTask::class.java,
        ) { task ->
            task.group = TASK_GROUP
            task.description = "Prints effective Jank Hunter values for ${configuration.variant.name}."
            task.effectiveReports.set(listOf(effective))
            task.detailedReports.set(listOf(details))
        }
        project.tasks.register(
            "jankHunterPrint${capitalizedVariant}InstrumentationPlan",
            PrintJankHunterConfigurationTask::class.java,
        ) { task ->
            task.group = TASK_GROUP
            task.description = "Prints the bytecode instrumentation plan for ${configuration.variant.name}."
            task.effectiveReports.set(listOf(JankHunterConfigurationReport.instrumentationPlan(configuration)))
            task.detailedReports.set(listOf(details))
        }

        printAll.configure { task ->
            task.effectiveReports.add(effective)
            task.detailedReports.add(details)
        }
        registerFunctionalParity(configuration, requiresDebugReleaseParity)
        validation.configure { task -> task.dependsOn(functionalParity) }
        validateAll.configure { task -> task.dependsOn(validation) }
        exportAll.configure { task -> task.configurations.add(JankHunterConfigurationReport.json(configuration)) }
        if (VariantBuildTypeMatcher.isReleaseLike(configuration.variant.buildType)) {
            releaseReadiness.configure { task -> task.dependsOn(validation) }
        }
        project.tasks.matching { task -> task.name == "pre${capitalizedVariant}Build" }.configureEach { task ->
            task.dependsOn(validation)
        }
        return validation
    }

    companion object {
        fun register(project: Project): JankHunterDiagnosticTaskRegistry {
            val printAll = project.tasks.register(
                "jankHunterPrintConfiguration",
                PrintJankHunterConfigurationTask::class.java,
            ) { task ->
                task.group = TASK_GROUP
                task.description = "Prints effective Jank Hunter configuration for every enabled variant."
                task.effectiveReports.convention(emptyList())
                task.detailedReports.convention(emptyList())
            }
            val validateAll = project.tasks.register("jankHunterValidateConfiguration") { task ->
                task.group = TASK_GROUP
                task.description = "Validates Jank Hunter configuration for every enabled variant."
            }
            val releaseReadiness = project.tasks.register("jankHunterCheckReleaseReadiness") { task ->
                task.group = TASK_GROUP
                task.description = "Checks privacy, storage and performance approvals for release variants."
            }
            val exportAll = project.tasks.register(
                "jankHunterExportConfiguration",
                ExportJankHunterConfigurationTask::class.java,
            ) { task ->
                task.group = TASK_GROUP
                task.description = "Exports effective Jank Hunter configurations as JSON."
                task.configurations.convention(emptyList())
                task.outputFile.set(
                    project.layout.buildDirectory.file("reports/jankhunter/configuration.json"),
                )
            }
            val functionalParity = project.tasks.register(
                "jankHunterCheckDebugReleaseParity",
                ValidateJankHunterFunctionalParityTask::class.java,
            ) { task ->
                task.group = TASK_GROUP
                task.description = "Checks that implicit debug and release Jank Hunter behavior is identical."
                task.checkedPairs.convention(emptyList())
                task.differences.convention(emptyList())
            }
            project.tasks.register(
                "jankHunterListProfiles",
                PrintJankHunterConfigurationTask::class.java,
            ) { task ->
                task.group = TASK_GROUP
                task.description = "Describes every built-in Jank Hunter profile."
                task.effectiveReports.set(listOf(JankHunterConfigurationReport.profiles()))
                task.detailedReports.set(listOf("Profiles are base values; explicit properties override them."))
            }
            project.tasks.register(
                "jankHunterCheckEnvironment",
                PrintJankHunterConfigurationTask::class.java,
            ) { task ->
                task.group = TASK_GROUP
                task.description = "Prints the Gradle, Java, Android Gradle Plugin and Jank Hunter versions."
                task.effectiveReports.set(listOf(environmentReport(project)))
                task.detailedReports.set(listOf("Project directory: ${project.projectDir.absolutePath}"))
            }
            return JankHunterDiagnosticTaskRegistry(
                printAll,
                validateAll,
                releaseReadiness,
                exportAll,
                functionalParity,
            )
        }

        private fun environmentReport(project: Project): String = buildString {
            appendLine("Jank Hunter environment")
            appendLine("Project: ${project.path}")
            appendLine("Jank Hunter: ${JankHunterDependencyCoordinates.load().version}")
            appendLine("Gradle: ${project.gradle.gradleVersion}")
            appendLine("Java: ${System.getProperty("java.version")}")
            appendLine("Java vendor: ${System.getProperty("java.vendor")}")
            appendLine("Android Gradle Plugin: ${androidGradlePluginVersion()}")
        }.trimEnd()

        private fun androidGradlePluginVersion(): String {
            return runCatching {
                Class.forName("com.android.Version").getField("ANDROID_GRADLE_PLUGIN_VERSION").get(null).toString()
            }.getOrElse { "not available" }
        }
    }

    private fun registerFunctionalParity(
        configuration: JankHunterResolvedConfiguration,
        required: Boolean,
    ) {
        val buildType = normalizedBuildType(configuration.variant.buildType)
        if (buildType != DEBUG && buildType != RELEASE) return
        val family = configuration.variant.productFlavors.toSortedMap().entries.joinToString(",") { (dimension, name) ->
            "$dimension=$name"
        }.ifEmpty { DEFAULT_FAMILY }
        val candidates = parityCandidates.getOrPut(family, ::linkedMapOf)
        candidates[buildType] = ParityCandidate(configuration, required)
        val debug = candidates[DEBUG] ?: return
        val release = candidates[RELEASE] ?: return
        if (!debug.required || !release.required) return
        val prefix = if (family == DEFAULT_FAMILY) "default" else family
        functionalParity.configure { task ->
            task.checkedPairs.add(prefix)
            task.differences.addAll(
                JankHunterFunctionalParity.differences(debug.configuration, release.configuration)
                    .map { difference -> "$prefix: $difference" },
            )
        }
    }

    private data class ParityCandidate(
        val configuration: JankHunterResolvedConfiguration,
        val required: Boolean,
    )
}

private const val TASK_GROUP = "jank hunter"
private const val DEBUG = "debug"
private const val RELEASE = "release"
private const val DEFAULT_FAMILY = "default"
