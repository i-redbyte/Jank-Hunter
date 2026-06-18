package io.jankhunter.gradle

import org.gradle.api.GradleException
import org.gradle.api.Project
import org.gradle.api.artifacts.Configuration

internal object JankHunterDependencyValidator {
    fun validateOkHttpHelper(project: Project, variantName: String, hooksEnabled: Boolean) {
        if (!hooksEnabled) return

        val displayNames = classpathComponents(project, variantName)
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
                normalized.contains("project :okhttp") ||
                normalized.contains("project ':okhttp")
        }
    }

    fun hasJankHunterOkHttp3(displayNames: Iterable<String>): Boolean {
        return displayNames.any { name ->
            name.lowercase().contains("jankhunter-okhttp3")
        }
    }

    private fun classpathComponents(project: Project, variantName: String): Set<String> {
        return listOfNotNull(
            project.configurations.findByName("${variantName}RuntimeClasspath"),
            project.configurations.findByName("${variantName}CompileClasspath"),
        ).flatMapTo(linkedSetOf()) { configuration ->
            configuration.componentDisplayNames()
        }
    }

    private fun Configuration.componentDisplayNames(): Set<String> {
        return incoming.resolutionResult.allComponents
            .mapTo(linkedSetOf()) { it.id.displayName }
    }
}
