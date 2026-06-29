package io.jankhunter.gradle

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterDependencyValidatorTest {
    @Test
    fun detectsOkHttpAndHelperArtifacts() {
        val components = listOf(
            "project :app",
            "com.squareup.okhttp3:okhttp:3.12.13",
            "okhttp-3.12.13.jar",
            "io.jankhunter:jankhunter-okhttp3:1.0.0",
        )

        assertTrue(JankHunterDependencyValidator.hasOkHttp(components))
        assertTrue(JankHunterDependencyValidator.hasJankHunterOkHttp3(components))
    }

    @Test
    fun doesNotRequireHelperWhenOkHttpIsAbsent() {
        val components = listOf(
            "project :app",
            "io.jankhunter:jankhunter-runtime:1.0.0",
        )

        assertFalse(JankHunterDependencyValidator.hasOkHttp(components))
        assertFalse(JankHunterDependencyValidator.hasJankHunterOkHttp3(components))
    }

    @Test
    fun detectsProjectHelperDependency() {
        val components = listOf(
            "project :sample-app",
            "com.squareup.okhttp3:okhttp:3.12.13",
            "project :jankhunter-okhttp3",
        )

        assertTrue(JankHunterDependencyValidator.hasOkHttp(components))
        assertTrue(JankHunterDependencyValidator.hasJankHunterOkHttp3(components))
    }

    @Test
    fun detectsFileHelperDependency() {
        val components = listOf(
            "com.squareup.okhttp3:okhttp:3.12.13",
            "jankhunter-okhttp3-1.0.0.aar",
        )

        assertTrue(JankHunterDependencyValidator.hasOkHttp(components))
        assertTrue(JankHunterDependencyValidator.hasJankHunterOkHttp3(components))
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
}
