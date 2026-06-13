package io.jankhunter.gradle

import com.android.build.api.instrumentation.FramesComputationMode
import com.android.build.api.instrumentation.InstrumentationScope
import com.android.build.api.variant.AndroidComponentsExtension
import org.gradle.api.Plugin
import org.gradle.api.Project
import java.util.Locale

class JankHunterPlugin : Plugin<Project> {
    override fun apply(project: Project) {
        val extension = project.extensions.create("jankHunter", JankHunterExtension::class.java)

        project.pluginManager.withPlugin("com.android.application") {
            configureAndroidProject(project, extension)
        }
        project.pluginManager.withPlugin("com.android.library") {
            configureAndroidProject(project, extension)
        }
    }

    private fun configureAndroidProject(project: Project, extension: JankHunterExtension) {
        val androidComponents = project.extensions.findByType(AndroidComponentsExtension::class.java)
        if (androidComponents == null) {
            project.logger.warn("Jank Hunter could not find AndroidComponentsExtension.")
            return
        }

        androidComponents.onVariants { variant ->
            if (!extension.isVariantEnabled(variant.name)) return@onVariants
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
                it.variantName.set(variant.name)
                it.methodCounters.set(extension.instrument.methodCounters)
                it.okhttp.set(extension.instrument.okhttp)
                it.webSockets.set(extension.instrument.webSockets)
                it.handlers.set(extension.instrument.handlers)
                it.executors.set(extension.instrument.executors)
                it.coroutines.set(extension.instrument.coroutines)
                it.allowEmptyIncludePackages.set(extension.instrument.allowEmptyIncludePackages)
                it.includeWholeApplication.set(includeWholeApplication)
                it.androidNamespace.set(androidNamespace)
                it.includePackages.set(effectiveIncludePackages)
                it.excludePackages.set(extension.instrument.excludePackages.toList())
                it.outputFile.set(
                    project.layout.buildDirectory.file("generated/jankhunter/${variant.name}/owner-map.json"),
                )
            }

            variant.instrumentation.transformClassesWith(
                JankHunterClassVisitorFactory::class.java,
                InstrumentationScope.ALL,
            ) { params ->
                params.methodCounters.set(extension.instrument.methodCounters)
                params.okhttp.set(extension.instrument.okhttp)
                params.webSockets.set(extension.instrument.webSockets)
                params.handlers.set(extension.instrument.handlers)
                params.executors.set(extension.instrument.executors)
                params.coroutines.set(extension.instrument.coroutines)
                params.allowEmptyIncludePackages.set(extension.instrument.allowEmptyIncludePackages)
                params.asmProgressLog.set(extension.instrument.asmProgressLog)
                params.progressLabel.set(project.progressLabel(variant.name))
                params.includePackages.set(effectiveIncludePackages)
                params.excludePackages.set(extension.instrument.excludePackages.toList())
            }
            variant.instrumentation.setAsmFramesComputationMode(FramesComputationMode.COPY_FRAMES)

            project.logger.lifecycle(
                "Jank Hunter variant {} configured. methodCounters={} okhttp={} webSockets={} handlers={} executors={} coroutines={} allowEmptyIncludePackages={} includeWholeApplication={} asmProgressLog={} ownerMapTask={}",
                variant.name,
                extension.instrument.methodCounters,
                extension.instrument.okhttp,
                extension.instrument.webSockets,
                extension.instrument.handlers,
                extension.instrument.executors,
                extension.instrument.coroutines,
                extension.instrument.allowEmptyIncludePackages,
                extension.instrument.includeWholeApplication,
                extension.instrument.asmProgressLog,
                ownerMap.name,
            )
        }
    }

    private fun JankHunterExtension.isVariantEnabled(variantName: String): Boolean {
        return enabledBuildTypes.any { enabled ->
            variantName.lowercase(Locale.US).contains(enabled.lowercase(Locale.US))
        }
    }

    private fun String.capitalized(): String {
        return replaceFirstChar {
            if (it.isLowerCase()) it.titlecase(Locale.US) else it.toString()
        }
    }

    private fun Project.progressLabel(variantName: String): String {
        return if (path == ":") ":$variantName" else "$path:$variantName"
    }
}
