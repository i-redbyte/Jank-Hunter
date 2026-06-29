package io.jankhunter.gradle

import org.gradle.api.GradleException
import org.gradle.api.Project
import org.gradle.api.artifacts.FileCollectionDependency
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
        val configurationNames = candidateConfigurationNames(variantName, project.configurations.names)
        val displayNames = linkedSetOf<String>()
        configurationNames.forEach { configurationName ->
            val configuration = project.configurations.findByName(configurationName) ?: return@forEach
            configuration.dependencies.forEach { dependency ->
                when (dependency) {
                    is ProjectDependency -> displayNames.add("project ${dependency.path}")
                    else -> {
                        val fileNames = dependencyFileNames(dependency)
                        if (fileNames.isNotEmpty()) {
                            displayNames.addAll(fileNames)
                        } else {
                            displayNames.add(
                                listOfNotNull(dependency.group, dependency.name, dependency.version)
                                    .joinToString(":"),
                            )
                        }
                    }
                }
            }
        }
        return displayNames
    }

    fun candidateConfigurationNames(variantName: String, availableConfigurationNames: Iterable<String>): Set<String> {
        val variantPrefix = variantName.replaceFirstChar { it.titlecase() }
        val configurationNames = linkedSetOf(
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
        val normalizedVariant = variantName.lowercase()
        val dependencyBuckets = listOf("api", "implementation", "compileonly", "runtimeonly")
        availableConfigurationNames.forEach { configurationName ->
            val normalizedName = configurationName.lowercase()
            dependencyBuckets.forEach { bucket ->
                if (normalizedName.endsWith(bucket)) {
                    val prefix = normalizedName.removeSuffix(bucket)
                    if (prefix.isNotEmpty() && normalizedVariant.endsWith(prefix)) {
                        configurationNames.add(configurationName)
                    }
                }
            }
        }
        return configurationNames
    }

    private fun dependencyFileNames(dependency: Any): List<String> {
        if (dependency is FileCollectionDependency) {
            return dependency.files.files.map { it.name }
        }

        val resolveMethod = dependency.javaClass.methods.firstOrNull { method ->
            method.name == "resolve" && method.parameterCount == 0
        } ?: return emptyList()
        val files = runCatching { resolveMethod.invoke(dependency) as? Iterable<*> }.getOrNull()
            ?: return emptyList()
        return files.mapNotNull { file -> (file as? java.io.File)?.name }
    }
}
