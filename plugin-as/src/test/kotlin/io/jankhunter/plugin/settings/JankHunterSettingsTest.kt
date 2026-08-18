package io.jankhunter.plugin.settings

import com.intellij.util.xmlb.XmlSerializer
import org.junit.Assert.assertEquals
import org.junit.Test

class JankHunterSettingsTest {
    @Test
    fun roundTripsCurrentStateDirectly() {
        val recentRun = JankHunterRecentRun().apply {
            timestamp = "2026-08-10T12:00:00"
            output = "/tmp/report.html"
        }
        val current = JankHunterSettings.State().apply {
            outputDirectory = "/tmp/custom-reports"
            openReportExternally = false
            lastRun = recentRun
            recentRuns = mutableListOf(recentRun)
        }

        val restored = XmlSerializer.deserialize(
            XmlSerializer.serialize(current),
            JankHunterSettings.State::class.java,
        )
        val settings = JankHunterSettings()
        settings.loadState(restored)
        val loaded = settings.state

        assertEquals("/tmp/custom-reports", loaded.outputDirectory)
        assertEquals(false, loaded.openReportExternally)
        assertEquals("/tmp/report.html", loaded.lastRun?.output)
        assertEquals("2026-08-10T12:00:00", loaded.recentRuns.single().timestamp)
    }

    @Test
    fun newStateUsesCurrentDefaultOutputDirectory() {
        assertEquals(JankHunterSettings.defaultOutputDirectory(), JankHunterSettings.State().outputDirectory)
    }
}
