package io.jankhunter.runtime.integration

import android.view.Window
import io.jankhunter.runtime.JankHunterFrameListenerV1
import io.jankhunter.runtime.JankHunterFrameTrackingV1
import io.jankhunter.runtime.JankHunterJankStatsAdapterV1

class AndroidXJankStatsAdapter : JankHunterJankStatsAdapterV1 {
    override fun create(window: Window, listener: JankHunterFrameListenerV1): JankHunterFrameTrackingV1 =
        JankStatsTrackingControl(window, JankStatsFrameCallback(listener))
    override fun createFrameCallback(listener: JankHunterFrameListenerV1): Any = JankStatsFrameCallback(listener)
}
