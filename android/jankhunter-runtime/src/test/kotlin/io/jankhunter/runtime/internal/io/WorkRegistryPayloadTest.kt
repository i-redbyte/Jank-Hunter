package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterWorkInfo
import io.jankhunter.runtime.JankHunterWorkState
import io.jankhunter.runtime.JankHunterWorkRegistryResult
import io.jankhunter.runtime.JankHunterWorkRegistryUnavailable
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class WorkRegistryPayloadTest {
    @Test fun unavailableSnapshotHasOneVersionedBoundedPacket() {
        val packets = encodeWorkRegistryPackets(1, 10, 20, 30000,
            JankHunterWorkRegistryResult.Unavailable(JankHunterWorkRegistryUnavailable.NO_SOURCE))
        assertEquals(1, packets.size)
        assertEquals(1, packets.single()[0].toInt())
        assertTrue(packets.single().size < 64)
    }

    @Test fun invalidSourceCannotEmitAnApparentlyEmptyCompleteRegistry() {
        val packets = encodeWorkRegistryPackets(1, 10, 20, 30000,
            JankHunterWorkRegistryResult.Snapshot(emptyList(), -1))
        val expected = encodeWorkRegistryPackets(1, 10, 20, 30000,
            JankHunterWorkRegistryResult.Unavailable(JankHunterWorkRegistryUnavailable.INVALID_SOURCE))
        org.junit.Assert.assertArrayEquals(expected.single(), packets.single())
    }
    @Test fun eligibilityForRunningWorkIsRejectedAsInvalidSource() {
        val row = JankHunterWorkInfo(
            uuid = "00000000-0000-0000-0000-000000000001", instanceId = 1,
            state = JankHunterWorkState.RUNNING, runAttempt = 0, generation = 0,
            tags = emptyList(), omittedTags = 0, initialDelayMs = 0,
            repeatIntervalMs = null, flexIntervalMs = null, earliestEligibleUnixMs = 100,
            stopReason = null, networkType = "NOT_REQUIRED", constraintsMask = 0,
            contentTriggers = emptyList(), omittedContentTriggers = 0,
            progress = emptyList(), output = emptyList(), omittedProgressKeys = 0,
            omittedOutputKeys = 0, truncatedFields = 0,
        )
        val actual = encodeWorkRegistryPackets(1, 10, 20, 30000,
            JankHunterWorkRegistryResult.Snapshot(listOf(row), 1))
        val expected = encodeWorkRegistryPackets(1, 10, 20, 30000,
            JankHunterWorkRegistryResult.Unavailable(JankHunterWorkRegistryUnavailable.INVALID_SOURCE))
        org.junit.Assert.assertArrayEquals(expected.single(), actual.single())
    }

}
