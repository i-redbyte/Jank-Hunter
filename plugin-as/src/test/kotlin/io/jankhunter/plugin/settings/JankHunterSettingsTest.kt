package io.jankhunter.plugin.settings

import com.intellij.util.xmlb.XmlSerializer
import org.junit.Assert.assertEquals
import org.junit.Test

class JankHunterSettingsTest {
    @Test
    fun roundTripsCurrentStateDirectly() {
        val current = JankHunterSettings.State().apply {
            outputDirectory = "/tmp/custom-reports"
            openReportExternally = false
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
    }

    @Test
    fun newStateUsesCurrentDefaultOutputDirectory() {
        assertEquals(JankHunterSettings.defaultOutputDirectory(), JankHunterSettings.State().outputDirectory)
    }
}
