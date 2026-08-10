package io.jankhunter.plugin.execution

import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Test

class JankHunterUserPathsTest {
    @Test
    fun expandsHomeDirectory() {
        assertEquals(System.getProperty("user.home"), JankHunterUserPaths.expandHome("~"))
        assertEquals(
            File(System.getProperty("user.home"), "JankHunter/reports").path,
            JankHunterUserPaths.expandHome("~/JankHunter/reports"),
        )
    }

    @Test
    fun keepsRegularPathUnchanged() {
        assertEquals("/tmp/jankhunter", JankHunterUserPaths.expandHome(" /tmp/jankhunter "))
    }

    @Test
    fun fallsBackWhenUserHomeIsUnavailable() {
        assertEquals(
            File("/tmp/jankhunter-user-dir").toPath().toAbsolutePath().normalize().toFile(),
            JankHunterUserPaths.resolveHomeDirectory(null, "/tmp/jankhunter-user-dir"),
        )
        assertEquals(
            File(".").toPath().toAbsolutePath().normalize().toFile(),
            JankHunterUserPaths.resolveHomeDirectory(null, null),
        )
    }
}
