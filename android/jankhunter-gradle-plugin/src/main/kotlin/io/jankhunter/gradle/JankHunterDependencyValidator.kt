package io.jankhunter.gradle

import org.gradle.api.GradleException
import org.gradle.api.Project
import org.gradle.api.artifacts.Dependency
import org.gradle.api.artifacts.FileCollectionDependency
import org.gradle.api.artifacts.ProjectDependency

internal object JankHunterDependencyValidator {
    fun validateDeclaredRuntime(project: Project, variantName: String, hooksEnabled: Boolean) {
        if (!hooksEnabled) return
        validateRuntime(
            variantName = variantName,
            hooksEnabled = true,
            displayNames = declaredDependencyDisplayNames(project, variantName, DependencyUsage.RUNTIME),
        )
    }

    fun validateRuntime(variantName: String, hooksEnabled: Boolean, displayNames: Iterable<String>) {
        if (!hooksEnabled || hasJankHunterRuntime(displayNames)) return
        throw GradleException(
            "Jank Hunter runtime hooks are enabled for variant '$variantName', but the runtime is missing. " +
                "The Gradle plugin normally adds it automatically; if dependency resolution was customized, add " +
                "implementation(\"$jankHunterGroup:$JANK_HUNTER_ANDROID_SDK_ARTIFACT:<version>\"). " +
                "Instrumentation was stopped to avoid emitting bytecode that could crash the host app.",
        )
    }

    fun validateDeclaredOkHttpHelper(project: Project, variantName: String, hooksEnabled: Boolean): Boolean {
        if (!hooksEnabled) return false
        val compileDependencies = declaredDependencyDisplayNames(project, variantName, DependencyUsage.COMPILE)
        val runtimeDependencies = declaredDependencyDisplayNames(project, variantName, DependencyUsage.RUNTIME)
        val helperAvailable = hasJankHunterOkHttpSupport(runtimeDependencies)
        if (hasOkHttp(compileDependencies) && !helperAvailable) {
            throwMissingOkHttpHelper(variantName)
        }
        return helperAvailable
    }

    fun validateOkHttpHelper(variantName: String, hooksEnabled: Boolean, displayNames: Iterable<String>) {
        if (!hooksEnabled || !hasOkHttp(displayNames) || hasJankHunterOkHttpSupport(displayNames)) return
        throwMissingOkHttpHelper(variantName)
    }

    private fun throwMissingOkHttpHelper(variantName: String): Nothing {
        throw GradleException(
            "Jank Hunter okhttp/webSockets ASM hooks are enabled for variant '$variantName', " +
                "and OkHttp is present, but Jank Hunter Android SDK is missing. " +
                "Add implementation(\"$jankHunterGroup:$JANK_HUNTER_ANDROID_SDK_ARTIFACT:<version>\") " +
                "or disable jankHunter.instrument.okhttp/webSockets.",
        )
    }

    fun hasOkHttp(displayNames: Iterable<String>): Boolean {
        return displayNames.any { name -> matchesArtifact(name, OKHTTP_GROUP, OKHTTP_ARTIFACT) }
    }

    fun hasJankHunterOkHttp3(displayNames: Iterable<String>): Boolean {
        return displayNames.any { name -> matchesArtifact(name, jankHunterGroup, JANK_HUNTER_OKHTTP_ARTIFACT) }
    }

    fun hasJankHunterAndroidSdk(displayNames: Iterable<String>): Boolean {
        return displayNames.any { name -> matchesArtifact(name, jankHunterGroup, JANK_HUNTER_ANDROID_SDK_ARTIFACT) }
    }

    fun hasDeclaredAndroidSdk(project: Project, variantName: String): Boolean {
        return hasJankHunterAndroidSdk(
            declaredDependencyDisplayNames(project, variantName, DependencyUsage.RUNTIME),
        )
    }

    private fun hasJankHunterOkHttpSupport(displayNames: Iterable<String>): Boolean {
        return hasJankHunterAndroidSdk(displayNames) || hasJankHunterOkHttp3(displayNames)
    }

    fun hasJankHunterRuntime(displayNames: Iterable<String>): Boolean {
        return hasJankHunterAndroidSdk(displayNames) ||
            displayNames.any { name -> matchesArtifact(name, jankHunterGroup, JANK_HUNTER_RUNTIME_ARTIFACT) }
    }

    private fun declaredDependencyDisplayNames(
        project: Project,
        variantName: String,
        usage: DependencyUsage,
    ): Set<String> {
        val availableNames = project.configurations.names
        val classpathName = availableNames.firstOrNull { name ->
            name.equals("$variantName${usage.classpathSuffix}", ignoreCase = true)
        }
        val configurationNames = classpathName?.let(::setOf) ?: candidateConfigurationNames(
            variantName,
            availableNames,
        ).filterTo(linkedSetOf(), usage::acceptsFallbackConfiguration)
        val displayNames = linkedSetOf<String>()
        configurationNames.forEach { configurationName ->
            val configuration = project.configurations.findByName(configurationName) ?: return@forEach
            configuration.allDependencies.forEach { dependency ->
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
            "${variantName}CompileClasspath",
            "${variantName}RuntimeClasspath",
            "${variantPrefix}CompileClasspath",
            "${variantPrefix}RuntimeClasspath",
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

    private fun dependencyFileNames(dependency: Dependency): List<String> {
        if (dependency is FileCollectionDependency) {
            return dependency.files.files.map { it.name }
        }
        return emptyList()
    }

    private fun matchesArtifact(displayName: String, group: String, artifact: String): Boolean {
        val normalized = displayName.trim().lowercase()
        if (normalized.isEmpty()) return false

        val fileName = normalized.substringAfterLast('/').substringAfterLast('\\')
        if (matchesArtifactFile(fileName, artifact)) return true

        val projectPath = normalized.removePrefix("project").trim()
            .removeSurrounding("'")
            .removeSurrounding("\"")
        if (normalized.startsWith("project ") && projectPath.startsWith(':')) {
            return projectPath.substringAfterLast(':') == artifact
        }

        val coordinates = normalized.split(':')
        return coordinates.size >= 2 && coordinates[0] == group && coordinates[1] == artifact
    }

    private fun matchesArtifactFile(fileName: String, artifact: String): Boolean {
        val extension = fileName.substringAfterLast('.', missingDelimiterValue = "")
        if (extension != "jar" && extension != "aar") return false
        val stem = fileName.removeSuffix(".$extension")
        if (stem == artifact) return true
        if (!stem.startsWith("$artifact-")) return false
        val versionAndClassifier = stem.removePrefix("$artifact-")
        if (versionAndClassifier.firstOrNull()?.isDigit() != true) return false
        return CLASSIFIER_ONLY_SUFFIXES.none { suffix -> versionAndClassifier.endsWith("-$suffix") }
    }

    private enum class DependencyUsage(
        val classpathSuffix: String,
        private val fallbackBuckets: Set<String>,
    ) {
        COMPILE(
            classpathSuffix = "CompileClasspath",
            fallbackBuckets = setOf("api", "implementation", "compileonly", "compileclasspath"),
        ),
        RUNTIME(
            classpathSuffix = "RuntimeClasspath",
            fallbackBuckets = setOf("api", "implementation", "runtimeonly", "runtimeclasspath"),
        ),
        ;

        fun acceptsFallbackConfiguration(name: String): Boolean {
            val normalized = name.lowercase()
            return fallbackBuckets.any(normalized::endsWith)
        }
    }

    private val jankHunterGroup by lazy { JankHunterDependencyCoordinates.load().group.lowercase() }

    private const val OKHTTP_GROUP = "com.squareup.okhttp3"
    private const val OKHTTP_ARTIFACT = "okhttp"
    private const val JANK_HUNTER_ANDROID_SDK_ARTIFACT = "jankhunter-android-sdk"
    private const val JANK_HUNTER_OKHTTP_ARTIFACT = "jankhunter-okhttp3"
    private const val JANK_HUNTER_RUNTIME_ARTIFACT = "jankhunter-runtime"
    private val CLASSIFIER_ONLY_SUFFIXES = setOf("sources", "javadoc")
}
