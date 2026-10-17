package io.jankhunter.runtime

import android.view.Window

/** Optional adapter port. Only Android types and primitive frame observations cross it. */
interface JankHunterJankStatsAdapterV1 {
    fun create(window: Window, listener: JankHunterFrameListenerV1): JankHunterFrameTrackingV1
    fun createFrameCallback(listener: JankHunterFrameListenerV1): Any
}

fun interface JankHunterFrameListenerV1 {
    fun onFrame(isJank: Boolean, durationNanos: Long)
    fun onFailure() = Unit
}

interface JankHunterFrameTrackingV1 : AutoCloseable {
    fun setTrackingEnabled(enabled: Boolean)
}
