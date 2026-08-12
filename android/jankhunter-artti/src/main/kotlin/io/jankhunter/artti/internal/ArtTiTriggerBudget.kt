package io.jankhunter.artti.internal

internal class ArtTiTriggerBudget(
    private val minIntervalMs: Long,
    private val maxPerMinute: Int,
) {
    private var windowStartMs = Long.MIN_VALUE
    private var lastTriggerMs = Long.MIN_VALUE
    private var count = 0

    @Synchronized
    fun tryAcquire(nowMs: Long): Boolean {
        if (nowMs < 0L) return false
        if (windowStartMs == Long.MIN_VALUE || nowMs < windowStartMs || nowMs - windowStartMs >= MINUTE_MS) {
            windowStartMs = nowMs
            count = 0
        }
        if (lastTriggerMs != Long.MIN_VALUE && nowMs >= lastTriggerMs && nowMs - lastTriggerMs < minIntervalMs) {
            return false
        }
        if (count >= maxPerMinute) return false
        lastTriggerMs = nowMs
        count++
        return true
    }

    private companion object {
        const val MINUTE_MS = 60_000L
    }
}
