package io.jankhunter.gradle

import org.gradle.api.GradleException
import org.gradle.api.Project
import org.gradle.api.artifacts.ProjectDependency

internal object JankHunterDependencyValidator {
    fun validateDeclaredOkHttpHelper(project: Project, variantName: String, hooksEnabled: Boolean) {
        validateOkHttpHelper(
            variantName = variantName,
            hooksEnabled = hooksEnabled,
            displayNames = declaredDependencyDisplayNames(project, variantName),
        )
    }

    fun validateOkHttpHelper(variantName: String, hooksEnabled: Boolean, displayNames: Iterable<String>) {
        if (!hooksEnabled) return

        if (!hasOkHttp(displayNames) || hasJankHunterOkHttp3(displayNames)) return

        throw GradleException(
            "Jank Hunter okhttp/webSockets ASM hooks are enabled for variant '$variantName', " +
                "and OkHttp is present on the classpath, but io.jankhunter:jankhunter-okhttp3 is missing. " +
                "Add debugImplementation(\"io.jankhunter:jankhunter-okhttp3:<version>\") " +
                "or disable jankHunter.instrument.okhttp/webSockets.",
        )
    }

    fun hasOkHttp(displayNames: Iterable<String>): Boolean {
        return displayNames.any { name ->
            val normalized = name.lowercase()
            normalized.contains("com.squareup.okhttp3:okhttp") ||
                normalized == "okhttp.jar" ||
                normalized.startsWith("okhttp-") ||
                normalized.contains("/okhttp-") ||
                normalized.contains("project :okhttp") ||
                normalized.contains("project ':okhttp")
        }
    }

    fun hasJankHunterOkHttp3(displayNames: Iterable<String>): Boolean {
        return displayNames.any { name ->
            name.lowercase().contains("jankhunter-okhttp3")
        }
    }

    private fun declaredDependencyDisplayNames(project: Project, variantName: String): Set<String> {
        val variantPrefix = variantName.replaceFirstChar { it.titlecase() }
        val configurationNames = listOf(
            "api",
            "implementation",
            "compileOnly",
            "runtimeOnly",
            "${variantName}Api",
            "${variantName}Implementation",
            "${variantName}CompileOnly",
            "${variantName}RuntimeOnly",
            "${variantPrefix}Api",
            "${variantPrefix}Implementation",
            "${variantPrefix}CompileOnly",
            "${variantPrefix}RuntimeOnly",
        )
        return configurationNames
            .asSequence()
            .mapNotNull { project.configurations.findByName(it) }
            .flatMap { it.dependencies.asSequence() }
            .mapTo(linkedSetOf()) { dependency ->
                when (dependency) {
                    is ProjectDependency -> "project ${dependency.path}"
                    else -> listOfNotNull(dependency.group, dependency.name, dependency.version)
                        .joinToString(":")
                }
            }
    }
}
