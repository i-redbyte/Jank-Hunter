package io.jankhunter.buildlogic

import org.gradle.testfixtures.ProjectBuilder
import org.jetbrains.kotlin.buildtools.api.ExperimentalBuildToolsApi
import org.jetbrains.kotlin.gradle.ExperimentalKotlinGradlePluginApi
import org.jetbrains.kotlin.gradle.dsl.KotlinBaseExtension
import org.jetbrains.kotlin.gradle.dsl.KotlinVersion
import org.jetbrains.kotlin.gradle.tasks.KotlinCompile
import org.junit.Assert.assertEquals
import org.junit.Test

class JankHunterDeviceKotlinBoundaryTest {
    @OptIn(ExperimentalBuildToolsApi::class, ExperimentalKotlinGradlePluginApi::class)
    @Test
    fun deviceBoundaryConstrainsLanguageApiAndPublishedStdlibTogether() {
        val project = ProjectBuilder.builder().build()
        project.pluginManager.apply("org.jetbrains.kotlin.jvm")
        project.configureDeviceKotlinBoundary()
        project.tasks.withType(KotlinCompile::class.java).forEach { task ->
            assertEquals(KotlinVersion.KOTLIN_1_9, task.compilerOptions.languageVersion.get())
            assertEquals(KotlinVersion.KOTLIN_1_9, task.compilerOptions.apiVersion.get())
        }
        val stdlib = project.configurations.getByName("api").dependencies.single {
            it.group == "org.jetbrains.kotlin" && it.name == "kotlin-stdlib"
        }
        assertEquals("1.9.25", stdlib.version)
        val kotlin = project.extensions.getByType(KotlinBaseExtension::class.java)
        assertEquals("2.0.21", kotlin.compilerVersion.get())
        assertEquals("1.9.25", kotlin.coreLibrariesVersion)
    }
}
