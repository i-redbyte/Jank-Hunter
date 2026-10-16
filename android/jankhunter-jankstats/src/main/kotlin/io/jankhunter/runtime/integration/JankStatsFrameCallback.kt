package io.jankhunter.runtime.integration

import androidx.metrics.performance.FrameData
import androidx.metrics.performance.JankStats
import io.jankhunter.runtime.JankHunterFrameListenerV1


/** Loaded only after the optional AndroidX dependency has been resolved. */
class JankStatsFrameCallback(
    listener: JankHunterFrameListenerV1,
) : JankStats.OnFrameListener, AutoCloseable {
    @Volatile private var listener: JankHunterFrameListenerV1? = listener

    override fun onFrame(volatileFrameData: FrameData) {
        val target = listener ?: return
        try {
            // FrameData is reused by AndroidX. Copy only its primitive values during this callback.
            target.onFrame(volatileFrameData.isJank, volatileFrameData.frameDurationUiNanos)
        } catch (failure: Throwable) {
            if (failure is VirtualMachineError || failure is ThreadDeath) throw failure
            target.onFailure()
        }
    }

    override fun close() {
        listener = null
    }
}
