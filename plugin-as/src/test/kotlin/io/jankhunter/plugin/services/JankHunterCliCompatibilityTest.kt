package io.jankhunter.plugin.services

import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterCliCompatibilityTest {
    @Test
    fun checksOnlyFlagsThatRequireNewCli() {
        assertNull(
            JankHunterCliCompatibility.compatibilityError(
                listOf("inspect", "--presentation"),
                "not a version",
            ),
        )
    }

    @Test
    fun rejectsOldCliForAppearanceFlags() {
        val error = JankHunterCliCompatibility.compatibilityError(
            listOf("inspect", "--report-style", "legacy"),
            "Jank Hunter CLI 1.0.0\n.jhlog format 1\n",
        )

        assertNotNull(error)
        assertTrue(error.orEmpty().contains("1.0.1"))
        assertNull(
            JankHunterCliCompatibility.compatibilityError(
                listOf("inspect", "--report-style", "legacy"),
                "Jank Hunter CLI 1.0.1\n.jhlog format 1\n",
            ),
        )
    }

    @Test
    fun reportsUnknownVersionInsteadOfLaunchingUnsupportedCommand() {
        val error = JankHunterCliCompatibility.compatibilityError(
            listOf("inspect", "--animated-background"),
            "unknown build",
        )

        assertNotNull(error)
        assertTrue(error.orEmpty().contains("jankhunter version"))
    }
}
