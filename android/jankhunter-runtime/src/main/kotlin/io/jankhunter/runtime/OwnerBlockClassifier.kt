package io.jankhunter.runtime

internal fun isMainThreadOwnerBlock(
    durationMs: Long,
    thresholdMs: Long,
    isMainThread: Boolean,
    monitorActive: Boolean,
): Boolean = isMainThread && !monitorActive && durationMs >= thresholdMs
