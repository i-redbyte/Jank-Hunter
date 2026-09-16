package io.jankhunter.runtime.internal

import org.junit.Assert.assertEquals
import org.junit.Test

class SaturatingMathTest {
    @Test
    fun monotonicDeadlineKeepsHugeTimeoutWithinComparableRange() {
        val startedAtNs = Long.MAX_VALUE - 7L

        val deadlineNs = monotonicDeadlineAfterMillis(Long.MAX_VALUE, startedAtNs)

        assertEquals(Long.MAX_VALUE / 2L, deadlineNs - startedAtNs)
    }

    @Test
    fun monotonicDeadlineTreatsNegativeTimeoutAsZero() {
        val startedAtNs = 42L

        assertEquals(startedAtNs, monotonicDeadlineAfterMillis(-1L, startedAtNs))
    }
}
