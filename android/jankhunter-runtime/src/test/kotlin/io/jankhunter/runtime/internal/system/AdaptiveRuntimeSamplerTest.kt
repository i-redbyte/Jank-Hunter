package io.jankhunter.runtime.internal.system

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AdaptiveRuntimeSamplerTest {
    @Test
    fun skipsStableMemoryUntilChangeOrMaxInterval() {
        val sampler = AdaptiveRuntimeSampler(
            memoryStableIntervalMs = 60_000L,
            contextStableIntervalMs = 60_000L,
        )

        assertTrue(sampler.shouldRecordMemory(0L, 100_000L, 40_000L, 20_000L))
        assertFalse(sampler.shouldRecordMemory(10_000L, 101_000L, 40_500L, 20_500L))
        assertTrue(sampler.shouldRecordMemory(20_000L, 105_000L, 40_500L, 20_500L))
        assertFalse(sampler.shouldRecordMemory(30_000L, 105_100L, 40_700L, 20_700L))
        assertTrue(sampler.shouldRecordMemory(90_000L, 105_100L, 40_700L, 20_700L))
    }

    @Test
    fun recordsContextWhenUserVisibleStateChanges() {
        val sampler = AdaptiveRuntimeSampler(
            memoryStableIntervalMs = 60_000L,
            contextStableIntervalMs = 60_000L,
        )

        assertTrue(sampler.shouldRecordContext(0L, 2, 80, 500_000L, false, false, true, 100L, 100L, false))
        assertFalse(sampler.shouldRecordContext(10_000L, 2, 80, 495_000L, false, false, true, 100L, 100L, false))
        assertTrue(sampler.shouldRecordContext(20_000L, 3, 80, 495_000L, false, false, true, 100L, 100L, false))
        assertTrue(sampler.shouldRecordContext(30_000L, 3, 80, 495_000L, true, false, true, 100L, 100L, false))
    }
}
