package io.jankhunter.gradle

import org.gradle.api.GradleException
import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterAutomaticDependenciesTest {
    @Test
    fun usesExactBuildTypeNameForImplementationConfiguration() {
        assertEquals(
            "debugImplementation",
            JankHunterAutomaticDependencies.implementationConfigurationName("debug"),
        )
        assertEquals(
            "qaImplementation",
            JankHunterAutomaticDependencies.implementationConfigurationName("qa"),
        )
        assertEquals(
            "ReleaseImplementation",
            JankHunterAutomaticDependencies.implementationConfigurationName("Release"),
        )
    }

    @Test
    fun alwaysAddsAnnotationsToCompileOnly() {
        val project = ProjectBuilder.builder().build()
        val compileOnly = project.configurations.create("compileOnly")

        JankHunterAutomaticDependencies.addAnnotations(project)
        JankHunterAutomaticDependencies.addAnnotations(project)

        assertEquals(1, compileOnly.dependencies.count { it.name == "jankhunter-annotations" })
    }

    @Test
    fun addsOnlyRuntimeToTheRequestedVariant() {
        val project = ProjectBuilder.builder().build()
        val debugImplementation = project.configurations.create("debugImplementation")
        val releaseImplementation = project.configurations.create("releaseImplementation")

        JankHunterAutomaticDependencies.addRuntime(project, "debug")

        assertTrue(debugImplementation.dependencies.any { it.name == "jankhunter-runtime" })
        assertFalse(debugImplementation.dependencies.any { it.name == "jankhunter-okhttp3" })
        assertTrue(releaseImplementation.dependencies.isEmpty())
    }

    @Test
    fun doesNotAddDuplicateRuntimeWhenPublicSdkIsDeclared() {
        val project = ProjectBuilder.builder().build()
        val implementation = project.configurations.create("implementation")
        val debugImplementation = project.configurations.create("debugImplementation")
        project.dependencies.add(
            implementation.name,
            "io.jankhunter:jankhunter-android-sdk:1.0.0",
        )

        JankHunterAutomaticDependencies.addRuntime(project, "debug")

        assertTrue(debugImplementation.dependencies.isEmpty())
    }

    @Test
    fun failsFastWhenRequiredConfigurationIsMissing() {
        val project = ProjectBuilder.builder().build()

        assertThrows(GradleException::class.java) {
            JankHunterAutomaticDependencies.addAnnotations(project)
        }
    }
}
