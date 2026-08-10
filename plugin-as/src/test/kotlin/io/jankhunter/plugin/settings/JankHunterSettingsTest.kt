package io.jankhunter.plugin.settings

import com.intellij.util.xmlb.XmlSerializer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterSettingsTest {
    @Test
    fun migratesLegacyStateWithoutDroppingSavedValues() {
        val recentRun = JankHunterRecentRun().apply {
            timestamp = "2026-08-10T12:00:00"
            output = "/tmp/old-report.html"
        }
        val legacy = JankHunterSettings.State().apply {
            schemaVersion = 0
            outputDirectory = "/tmp/custom-reports"
            openReportInIde = true
            openReportExternally = false
            packageName = "com.example.app"
            lastRun = recentRun
            recentRuns = mutableListOf(recentRun)
        }

        val restored = XmlSerializer.deserialize(
            XmlSerializer.serialize(legacy),
            JankHunterSettings.State::class.java,
        )
        val settings = JankHunterSettings()
        settings.loadState(restored)
        val migrated = settings.state

        assertEquals(JankHunterSettings.CURRENT_SCHEMA_VERSION, migrated.schemaVersion)
        assertEquals("/tmp/custom-reports", migrated.outputDirectory)
        assertTrue(migrated.openReportExternally)
        assertEquals("com.example.app", migrated.packageName)
        assertEquals("/tmp/old-report.html", migrated.lastRun?.output)
        assertEquals("2026-08-10T12:00:00", migrated.recentRuns.single().timestamp)
    }

    @Test
    fun suppliesDefaultOutputDirectoryForBlankLegacyValue() {
        val legacy = JankHunterSettings.State().apply {
            schemaVersion = 0
            outputDirectory = ""
            openReportInIde = false
        }

        val settings = JankHunterSettings()
        settings.loadState(legacy)

        assertEquals(JankHunterSettings.defaultOutputDirectory(), settings.state.outputDirectory)
    }
}
