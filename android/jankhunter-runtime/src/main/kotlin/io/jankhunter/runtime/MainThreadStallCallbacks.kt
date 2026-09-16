package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.Jhlog

/** One bound writer generation owns both the initial observation and its terminal update. */
internal interface MainThreadStallCallbacks {
    fun nextMainThreadStallId(): Long
    fun captureMainThreadStallContext(owner: String?): JankHunterContextSnapshot
    fun recordMainThreadStall(
        context: JankHunterContextSnapshot,
        stackHint: String?,
        durationMs: Long,
        incidentId: Long = 0L,
        state: MainThreadStallState = MainThreadStallState.RECOVERED,
    )
}

internal enum class MainThreadStallState(val wireValue: Long) {
    UNKNOWN(Jhlog.STALL_STATE_UNKNOWN),
    ONGOING(Jhlog.STALL_STATE_ONGOING),
    RECOVERED(Jhlog.STALL_STATE_RECOVERED),
    INTERRUPTED(Jhlog.STALL_STATE_INTERRUPTED),
}
