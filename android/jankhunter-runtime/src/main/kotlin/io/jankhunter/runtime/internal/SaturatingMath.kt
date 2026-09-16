package io.jankhunter.runtime.internal

import java.util.concurrent.TimeUnit

internal fun saturatingAdd(left: Long, right: Long): Long {
    if (right <= 0L) return left
    return if (left > Long.MAX_VALUE - right) Long.MAX_VALUE else left + right
}

/**
 * Creates a deadline that remains safe for the usual `deadline - now` monotonic-clock comparison.
 * `TimeUnit` saturates the unit conversion; the half-range cap keeps subtraction unambiguous even
 * when [System.nanoTime] wraps around.
 */
internal fun monotonicDeadlineAfterMillis(timeoutMs: Long, nowNs: Long = System.nanoTime()): Long {
    val timeoutNs = TimeUnit.MILLISECONDS.toNanos(timeoutMs.coerceAtLeast(0L))
    return nowNs + timeoutNs.coerceAtMost(Long.MAX_VALUE / 2L)
}
