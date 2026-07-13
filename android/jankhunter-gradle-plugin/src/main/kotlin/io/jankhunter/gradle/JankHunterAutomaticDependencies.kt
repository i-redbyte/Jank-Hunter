package io.jankhunter.gradle

import org.gradle.api.Project
import org.gradle.api.artifacts.Dependency
import org.gradle.api.artifacts.ProjectDependency

internal object JankHunterAutomaticDependencies {
    private const val ANDROID_SDK_ARTIFACT = "jankhunter-android-sdk"
    private const val ANNOTATIONS_ARTIFACT = "jankhunter-annotations"

    fun configure(project: Project, extension: JankHunterExtension, addAndroidSdk: Boolean) {
        project.afterEvaluate {
            if (!extension.enabled || !extension.dependencies.enabled) return@afterEvaluate

            val defaultCoordinates = JankHunterDependencyCoordinates.load()
            val group = extension.dependencies.group.normalizedOr(defaultCoordinates.group)
            val version = extension.dependencies.version.normalizedOr(defaultCoordinates.version)

            if (extension.dependencies.addAnnotations) {
                project.addJankHunterDependencyIfMissing(
                    configurationName = "compileOnly",
                    group = group,
                    artifactId = ANNOTATIONS_ARTIFACT,
                    version = version,
                )
            }

            if (addAndroidSdk && extension.dependencies.addAndroidSdk) {
                extension.enabledBuildTypes
                    .map { it.trim() }
                    .filter(String::isNotEmpty)
                    .map(::implementationConfigurationName)
                    .distinct()
                    .forEach { configurationName ->
                        project.addJankHunterDependencyIfMissing(
                            configurationName = configurationName,
                            group = group,
                            artifactId = ANDROID_SDK_ARTIFACT,
                            version = version,
                        )
                    }
            }
        }
    }

    private fun Project.addJankHunterDependencyIfMissing(
        configurationName: String,
        group: String,
        artifactId: String,
        version: String,
    ) {
        val configuration = configurations.findByName(configurationName)
        if (configuration == null) {
            logger.warn(
                "Jank Hunter could not add {}:{}:{} because configuration '{}' was not found.",
                group,
                artifactId,
                version,
                configurationName,
            )
            return
        }
        if (configuration.dependencies.any { it.matchesJankHunterDependency(group, artifactId) }) return

        val notation = localProjectDependency(artifactId) ?: "$group:$artifactId:$version"
        dependencies.add(configurationName, notation)
    }

    private fun Project.localProjectDependency(artifactId: String): Any? {
        val targetProject = rootProject.findProject(":$artifactId") ?: return null
        if (targetProject == this) return null
        return dependencies.project(mapOf("path" to targetProject.path))
    }

    private fun Dependency.matchesJankHunterDependency(group: String, artifactId: String): Boolean {
        if (this is ProjectDependency) {
            return name == artifactId || path == ":$artifactId"
        }
        return this.group == group && name == artifactId
    }

    internal fun implementationConfigurationName(buildType: String): String {
        return "${buildType}Implementation"
    }

    private fun String?.normalizedOr(defaultValue: String): String {
        return this?.trim()?.takeIf(String::isNotEmpty) ?: defaultValue
    }
}
