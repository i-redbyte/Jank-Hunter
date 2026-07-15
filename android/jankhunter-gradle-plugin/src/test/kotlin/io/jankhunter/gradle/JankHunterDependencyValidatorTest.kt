package io.jankhunter.gradle

import org.gradle.api.GradleException
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterDependencyValidatorTest {
    @Test
    fun detectsDirectOkHttpAndExplicitHelperArtifacts() {
        val components = listOf(
            "project :app",
            "com.squareup.okhttp3:okhttp:4.12.0",
            "io.jankhunter:jankhunter-okhttp3:1.0.0",
        )

        assertTrue(JankHunterDependencyValidator.hasOkHttp(components))
        assertTrue(JankHunterDependencyValidator.hasJankHunterOkHttp3(components))
        JankHunterDependencyValidator.validateOkHttpHelper(
            variantName = "debug",
            hooksEnabled = true,
            displayNames = components,
        )
    }

    @Test
    fun singleAndroidSdkDependencyProvidesRuntimeAndOkHttpSupport() {
        val components = listOf(
            "com.squareup.okhttp3:okhttp:4.12.0",
            "io.jankhunter:jankhunter-android-sdk:1.0.0",
        )

        assertTrue(JankHunterDependencyValidator.hasJankHunterAndroidSdk(components))
        assertTrue(JankHunterDependencyValidator.hasJankHunterRuntime(components))
        JankHunterDependencyValidator.validateOkHttpHelper(
            variantName = "debug",
            hooksEnabled = true,
            displayNames = components,
        )
        JankHunterDependencyValidator.validateRuntime(
            variantName = "debug",
            hooksEnabled = true,
            displayNames = components,
        )
    }

    @Test
    fun unrelatedBundleIsNotAcceptedAsRuntimeOrOkHttpHelper() {
        val components = listOf(
            "project :app",
            "io.jankhunter:legacy-bundle:1.0.0",
        )

        assertFalse(JankHunterDependencyValidator.hasJankHunterOkHttp3(components))
        assertFalse(JankHunterDependencyValidator.hasJankHunterRuntime(components))
    }

    @Test
    fun hooksDoNotRequireHelperWhenOkHttpIsAbsent() {
        val components = listOf("project :app", "io.jankhunter:jankhunter-runtime:1.0.0")

        assertFalse(JankHunterDependencyValidator.hasOkHttp(components))
        JankHunterDependencyValidator.validateOkHttpHelper(
            variantName = "debug",
            hooksEnabled = true,
            displayNames = components,
        )
    }

    @Test
    fun hooksRequireHelperWhenOkHttpIsPresent() {
        assertThrows(GradleException::class.java) {
            JankHunterDependencyValidator.validateOkHttpHelper(
                variantName = "debug",
                hooksEnabled = true,
                displayNames = listOf("com.squareup.okhttp3:okhttp:3.12.13"),
            )
        }

        JankHunterDependencyValidator.validateOkHttpHelper(
            variantName = "debug",
            hooksEnabled = false,
            displayNames = emptyList(),
        )
    }

    @Test
    fun detectsProjectHelperDependency() {
        val components = listOf(
            "project :sample-app",
            "project :okhttp",
            "project :jankhunter-okhttp3",
        )

        assertTrue(JankHunterDependencyValidator.hasOkHttp(components))
        assertTrue(JankHunterDependencyValidator.hasJankHunterOkHttp3(components))
    }

    @Test
    fun detectsFileHelperDependency() {
        val components = listOf(
            "/tmp/dependencies/okhttp-3.12.13.jar",
            "jankhunter-okhttp3-1.0.0.aar",
        )

        assertTrue(JankHunterDependencyValidator.hasOkHttp(components))
        assertTrue(JankHunterDependencyValidator.hasJankHunterOkHttp3(components))
    }

    @Test
    fun doesNotMistakeRelatedOkHttpArtifactsForTheCoreLibrary() {
        val components = listOf(
            "com.squareup.okhttp3:logging-interceptor:4.12.0",
            "okhttp-urlconnection-4.12.0.jar",
            "project :okhttp-logging",
            "project :fakeokhttp",
            "okhttp-4.12.0-sources.jar",
            "io.jankhunter:jankhunter-okhttp3:1.0.0",
        )

        assertFalse(JankHunterDependencyValidator.hasOkHttp(components))
    }

    @Test
    fun doesNotAcceptRelatedArtifactsAsJankHunterRuntimeOrHelper() {
        val components = listOf(
            "io.jankhunter:jankhunter-okhttp3-testing:1.0.0",
            "project :fake-jankhunter-okhttp3",
            "jankhunter-okhttp3-1.0.0-sources.jar",
            "io.jankhunter:jankhunter-runtime-tools:1.0.0",
        )

        assertFalse(JankHunterDependencyValidator.hasJankHunterOkHttp3(components))
        assertFalse(JankHunterDependencyValidator.hasJankHunterRuntime(components))
    }

    @Test
    fun inheritedFlavorDependenciesAreValidatedWithoutResolvingAConfiguration() {
        val project = org.gradle.testfixtures.ProjectBuilder.builder().build()
        val flavor = project.configurations.create("freeImplementation")
        val variantClasspath = project.configurations.create("freeDebugCompileClasspath")
        variantClasspath.extendsFrom(flavor)
        project.dependencies.add(flavor.name, "com.squareup.okhttp3:okhttp:4.12.0")

        assertThrows(GradleException::class.java) {
            JankHunterDependencyValidator.validateDeclaredOkHttpHelper(
                project = project,
                variantName = "freeDebug",
                hooksEnabled = true,
            )
        }

        assertFalse(variantClasspath.state == org.gradle.api.artifacts.Configuration.State.RESOLVED)
    }

    @Test
    fun compileOnlyHelperDoesNotSatisfyRuntimeSafetyCheck() {
        val project = org.gradle.testfixtures.ProjectBuilder.builder().build()
        val compileOnly = project.configurations.create("debugCompileOnly")
        val compileClasspath = project.configurations.create("debugCompileClasspath")
        val runtimeClasspath = project.configurations.create("debugRuntimeClasspath")
        compileClasspath.extendsFrom(compileOnly)
        project.dependencies.add(compileOnly.name, "com.squareup.okhttp3:okhttp:4.12.0")
        project.dependencies.add(compileOnly.name, "io.jankhunter:jankhunter-okhttp3:1.0.0")

        assertThrows(GradleException::class.java) {
            JankHunterDependencyValidator.validateDeclaredOkHttpHelper(
                project = project,
                variantName = "debug",
                hooksEnabled = true,
            )
        }

        assertFalse(runtimeClasspath.state == org.gradle.api.artifacts.Configuration.State.RESOLVED)
    }

    @Test
    fun reportsExplicitRuntimeHelperWithoutResolvingClasspathOrRequiringOkHttp() {
        val project = org.gradle.testfixtures.ProjectBuilder.builder().build()
        val implementation = project.configurations.create("debugImplementation")
        val compileClasspath = project.configurations.create("debugCompileClasspath")
        val runtimeClasspath = project.configurations.create("debugRuntimeClasspath")
        compileClasspath.extendsFrom(implementation)
        runtimeClasspath.extendsFrom(implementation)
        project.dependencies.add(implementation.name, "io.jankhunter:jankhunter-okhttp3:1.0.0")

        assertTrue(
            JankHunterDependencyValidator.validateDeclaredOkHttpHelper(
                project = project,
                variantName = "debug",
                hooksEnabled = true,
            ),
        )
        assertFalse(compileClasspath.state == org.gradle.api.artifacts.Configuration.State.RESOLVED)
        assertFalse(runtimeClasspath.state == org.gradle.api.artifacts.Configuration.State.RESOLVED)
    }

    @Test
    fun reportsSingleAndroidSdkWithoutResolvingClasspath() {
        val project = org.gradle.testfixtures.ProjectBuilder.builder().build()
        val implementation = project.configurations.create("debugImplementation")
        val compileClasspath = project.configurations.create("debugCompileClasspath")
        val runtimeClasspath = project.configurations.create("debugRuntimeClasspath")
        compileClasspath.extendsFrom(implementation)
        runtimeClasspath.extendsFrom(implementation)
        project.dependencies.add(implementation.name, "com.squareup.okhttp3:okhttp:4.12.0")
        project.dependencies.add(implementation.name, "io.jankhunter:jankhunter-android-sdk:1.0.0")

        assertTrue(
            JankHunterDependencyValidator.validateDeclaredOkHttpHelper(
                project = project,
                variantName = "debug",
                hooksEnabled = true,
            ),
        )
        assertFalse(compileClasspath.state == org.gradle.api.artifacts.Configuration.State.RESOLVED)
        assertFalse(runtimeClasspath.state == org.gradle.api.artifacts.Configuration.State.RESOLVED)
    }

    @Test
    fun includesBuildTypeDependencyConfigurationsForFlavorVariant() {
        val configurationNames = JankHunterDependencyValidator.candidateConfigurationNames(
            variantName = "vkteamsStoreDebug",
            availableConfigurationNames = listOf(
                "debugImplementation",
                "debugRuntimeOnly",
                "releaseImplementation",
            ),
        )

        assertTrue(configurationNames.contains("debugImplementation"))
        assertTrue(configurationNames.contains("debugRuntimeOnly"))
        assertFalse(configurationNames.contains("releaseImplementation"))
    }

    @Test
    fun runtimeHooksRequireRuntimeArtifact() {
        assertTrue(
            JankHunterDependencyValidator.hasJankHunterRuntime(
                listOf("io.jankhunter:jankhunter-runtime:1.0.0"),
            ),
        )
        assertThrows(GradleException::class.java) {
            JankHunterDependencyValidator.validateRuntime(
                variantName = "debug",
                hooksEnabled = true,
                displayNames = listOf("project :app"),
            )
        }
        JankHunterDependencyValidator.validateRuntime(
            variantName = "debug",
            hooksEnabled = false,
            displayNames = emptyList(),
        )
    }
}
