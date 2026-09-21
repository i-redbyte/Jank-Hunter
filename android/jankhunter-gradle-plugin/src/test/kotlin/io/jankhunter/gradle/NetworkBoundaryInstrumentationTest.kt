package io.jankhunter.gradle

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class NetworkBoundaryInstrumentationTest {
    @Test
    fun networkBoundaryIncludesDependencyClassesOutsideApplicationPackages() {
        assertTrue(networkBoundaryMatches("third.party.network.ClientFactory", emptySet()))
    }

    @Test
    fun networkBoundaryStillHonorsExplicitAndBuiltinExclusions() {
        assertFalse(networkBoundaryMatches("third.party.generated.ClientFactory", setOf("third.party.generated")))
        assertFalse(networkBoundaryMatches("io.jankhunter.runtime.JankHunter", emptySet()))
    }
}
