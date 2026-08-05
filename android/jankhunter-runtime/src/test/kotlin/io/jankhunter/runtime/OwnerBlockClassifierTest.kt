package io.jankhunter.runtime

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class OwnerBlockClassifierTest {
    @Test
    fun backgroundOwnerBlockIsNotMainThreadStall() {
        assertFalse(
            isMainThreadOwnerBlock(
                durationMs = 1_200,
                thresholdMs = 250,
                isMainThread = false,
                monitorActive = false,
            ),
        )
    }

    @Test
    fun mainThreadOwnerBlockAboveThresholdIsStall() {
        assertTrue(
            isMainThreadOwnerBlock(
                durationMs = 250,
                thresholdMs = 250,
                isMainThread = true,
                monitorActive = false,
            ),
        )
    }

    @Test
    fun shortMainThreadOwnerBlockIsNotStall() {
        assertFalse(
            isMainThreadOwnerBlock(
                durationMs = 249,
                thresholdMs = 250,
                isMainThread = true,
                monitorActive = false,
            ),
        )
    }

    @Test
    fun activeMainThreadMonitorOwnsStallEvent() {
        assertFalse(
            isMainThreadOwnerBlock(
                durationMs = 1_200,
                thresholdMs = 250,
                isMainThread = true,
                monitorActive = true,
            ),
        )
    }
}
