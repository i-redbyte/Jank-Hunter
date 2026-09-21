package io.jankhunter.runtime

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeSamplingServiceTest {
    @Test
    fun exactAdmissionRecordsEveryCollectorSnapshot() {
        val service = RuntimeSamplingService { 1_000L }
        service.configure(
            JankHunterConfig.builder()
                .exactEventCollectionEnabled(true)
                .adaptiveSamplingEnabled(true)
                .adaptiveMemoryStableIntervalMs(60_000L)
                .build(),
        )

        assertTrue(service.shouldRecordMemory(100L, 50L, 25L))
        assertTrue(service.shouldRecordMemory(100L, 50L, 25L))
    }

    @Test
    fun bestEffortCanStillSuppressUnchangedCollectorSnapshots() {
        val service = RuntimeSamplingService { 1_000L }
        service.configure(
            JankHunterConfig.builder()
                .exactEventCollectionEnabled(false)
                .adaptiveSamplingEnabled(true)
                .adaptiveMemoryStableIntervalMs(60_000L)
                .build(),
        )

        assertTrue(service.shouldRecordMemory(100L, 50L, 25L))
        assertFalse(service.shouldRecordMemory(100L, 50L, 25L))
    }
}
