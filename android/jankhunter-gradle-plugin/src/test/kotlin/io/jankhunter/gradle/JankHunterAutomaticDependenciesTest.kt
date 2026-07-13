package io.jankhunter.gradle

import org.junit.Assert.assertEquals
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
}
