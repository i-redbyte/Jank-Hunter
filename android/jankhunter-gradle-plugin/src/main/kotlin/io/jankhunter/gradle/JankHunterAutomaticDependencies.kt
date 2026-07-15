package io.jankhunter.gradle

import org.gradle.api.GradleException
import org.gradle.api.Project
import org.gradle.api.artifacts.Dependency
import org.gradle.api.artifacts.ProjectDependency

internal object JankHunterAutomaticDependencies {
    private const val ANNOTATIONS_ARTIFACT = "jankhunter-annotations"
    private const val RUNTIME_ARTIFACT = "jankhunter-runtime"

    fun addAnnotations(project: Project) {
        project.addJankHunterDependencyIfMissing(
            configurationName = "compileOnly",
            artifactId = ANNOTATIONS_ARTIFACT,
        )
    }

    fun addRuntime(project: Project, variantName: String) {
        if (JankHunterDependencyValidator.hasDeclaredAndroidSdk(project, variantName)) return
        project.addJankHunterDependencyIfMissing(
            configurationName = implementationConfigurationName(variantName),
            artifactId = RUNTIME_ARTIFACT,
        )
    }

    private fun Project.addJankHunterDependencyIfMissing(
        configurationName: String,
        artifactId: String,
    ) {
        val coordinates = JankHunterDependencyCoordinates.load()
        val configuration = configurations.findByName(configurationName)
            ?: throw GradleException(
                "Jank Hunter could not add ${coordinates.group}:$artifactId:${coordinates.version} because " +
                    "required configuration '$configurationName' was not found.",
            )
        if (configuration.dependencies.any { it.matchesJankHunterDependency(coordinates.group, artifactId) }) return

        val notation = localProjectDependency(artifactId) ?:
            "${coordinates.group}:$artifactId:${coordinates.version}"
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

    internal fun implementationConfigurationName(variantName: String): String {
        return "${variantName}Implementation"
    }
}
