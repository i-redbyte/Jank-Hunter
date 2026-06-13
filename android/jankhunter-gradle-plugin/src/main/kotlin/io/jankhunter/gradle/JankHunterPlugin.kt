package io.jankhunter.gradle

import org.gradle.api.Plugin
import org.gradle.api.Project

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
        project.logger.lifecycle(
            "Jank Hunter configured for build types {}. ASM instrumentation hooks will be added in the next implementation phase.",
            extension.enabledBuildTypes,
        )
    }
}
