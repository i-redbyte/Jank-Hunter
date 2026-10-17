package io.jankhunter.workmanager

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class WorkerProgressLimiterTest {
    @Test fun recordsAtMostOnePerSecondAndThirtyTwoPerAttempt() {
        val limiter = WorkerProgressLimiter()
        assertTrue(limiter.accept(0))
        assertFalse(limiter.accept(999_999_999))
        for (index in 1L..31L) assertTrue(limiter.accept(index * 1_000_000_000))
        assertFalse(limiter.accept(32_000_000_000))
        assertEquals(2, limiter.finish())
        assertFalse(limiter.accept(33_000_000_000))
    }
}
