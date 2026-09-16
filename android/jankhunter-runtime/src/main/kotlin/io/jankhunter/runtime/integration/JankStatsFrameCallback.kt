package io.jankhunter.runtime.integration

import androidx.metrics.performance.FrameData
import androidx.metrics.performance.JankStats
import io.jankhunter.runtime.RuntimeHookFailureReason
import io.jankhunter.runtime.RuntimeHookGuard

/** Loaded only after the optional AndroidX dependency has been resolved. */
internal class JankStatsFrameCallback(
    listener: JankHunterJankStats.FrameListener,
) : JankStats.OnFrameListener, AutoCloseable {
    @Volatile private var listener: JankHunterJankStats.FrameListener? = listener

    override fun onFrame(volatileFrameData: FrameData) {
        val target = listener ?: return
        RuntimeHookGuard.run(RuntimeHookFailureReason.JANKSTATS_FRAME) {
            // FrameData is reused by AndroidX. Copy only its primitive values during this callback.
            target.onFrame(volatileFrameData.isJank, volatileFrameData.frameDurationUiNanos)
        }
    }

    override fun close() {
        listener = null
    }
}
