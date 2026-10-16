package io.jankhunter.runtime.integration

import android.view.Window
import androidx.metrics.performance.JankStats
import io.jankhunter.runtime.JankHunterFrameTrackingV1

/** Direct calls keep AndroidX entry points reachable under R8 without broad keep rules. */
class JankStatsTrackingControl(
    window: Window,
    private val callback: JankStatsFrameCallback,
) : JankHunterFrameTrackingV1 {
    private var instance: JankStats? = JankStats.createAndTrack(window, callback)

    override fun setTrackingEnabled(enabled: Boolean) {
        instance?.isTrackingEnabled = enabled
    }

    override fun close() {
        instance = null
        callback.close()
    }
}
