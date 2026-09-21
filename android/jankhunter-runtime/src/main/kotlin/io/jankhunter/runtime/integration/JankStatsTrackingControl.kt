package io.jankhunter.runtime.integration

import android.view.Window
import androidx.metrics.performance.JankStats

/** Direct calls keep AndroidX entry points reachable under R8 without broad keep rules. */
internal class JankStatsTrackingControl(
    window: Window,
    private val callback: JankStatsFrameCallback,
) : JankHunterJankStats.TrackingControl {
    private var instance: JankStats? = JankStats.createAndTrack(window, callback)

    override fun setTrackingEnabled(enabled: Boolean) {
        instance?.isTrackingEnabled = enabled
    }

    override fun close() {
        instance = null
        callback.close()
    }
}
