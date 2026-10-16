package io.jankhunter.workmanager

import androidx.work.Constraints
import androidx.work.Data
import androidx.work.WorkInfo
import io.jankhunter.runtime.JankHunterWorkConstraint
import io.jankhunter.runtime.JankHunterWorkState
import java.util.UUID
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class WorkInfoSnapshotTest {
    @Test
    fun retainedTagAndProgressMetadataAreStableAcrossInputOrder() {
        val tags = linkedSetOf("z", "y", "x", "w", "v", "u", "t", "s", "r")
        val progress = Data.Builder().apply {
            listOf("z", "y", "x", "w", "v", "u", "t", "s", "r").forEach { putInt(it, 1) }
        }.build()
        val info = WorkInfo(UUID.randomUUID(), WorkInfo.State.RUNNING, tags, progress = progress)
        val row = workInfoSnapshot(info, observedStopReason = null, instanceId = 1)
        assertEquals(listOf("r", "s", "t", "u", "v", "w", "x", "y"), row.tags)
        assertEquals(listOf("r", "s", "t", "u", "v", "w", "x", "y"), row.progress.map { it.key })
        assertEquals(1, row.omittedTags)
        assertEquals(1, row.omittedProgressKeys)
    }

    @Test
    fun blockedWorkDoesNotInventStartOrScheduleAndConstraintsStayDeclarations() {
        val info = WorkInfo(UUID.randomUUID(), WorkInfo.State.BLOCKED, setOf("tag"),
            constraints = Constraints.Builder().setRequiresCharging(true).setRequiresBatteryNotLow(true).build())
        val row = workInfoSnapshot(info, observedStopReason = null, instanceId = 1)
        assertEquals(info.id.toString(), row.uuid)
        assertEquals(JankHunterWorkState.BLOCKED, row.state)
        assertNull(row.earliestEligibleUnixMs)
        assertNull(row.repeatIntervalMs)
        assertNull(row.stopReason)
        assertEquals(JankHunterWorkConstraint.CHARGING.mask or JankHunterWorkConstraint.BATTERY_NOT_LOW.mask, row.constraintsMask)
    }

    @Test
    fun periodicScheduleAndGenerationAreObservedWithoutInventingAnExecution() {
        val info = WorkInfo(UUID.randomUUID(), WorkInfo.State.ENQUEUED, emptySet(), runAttemptCount = 2,
            generation = 3, initialDelayMillis = 1000, periodicityInfo = WorkInfo.PeriodicityInfo(900000, 300000),
            nextScheduleTimeMillis = 12345, stopReason = WorkInfo.STOP_REASON_CONSTRAINT_CONNECTIVITY)
        val row = workInfoSnapshot(info, observedStopReason = WorkInfo.STOP_REASON_CONSTRAINT_CONNECTIVITY, instanceId = 1)
        assertEquals(2, row.runAttempt)
        assertEquals(3, row.generation)
        assertEquals(900000L, row.repeatIntervalMs)
        assertEquals(300000L, row.flexIntervalMs)
        assertEquals(12345L, row.earliestEligibleUnixMs)
        assertEquals(WorkInfo.STOP_REASON_CONSTRAINT_CONNECTIVITY, row.stopReason)
    }

    @Test
    fun dataStoresOnlyBoundedMetadataNeverValues() {
        val data = Data.Builder().putString("token", "secret-value-123").putLong("count", 123).build()
        val info = WorkInfo(UUID.randomUUID(), WorkInfo.State.RUNNING, (0..20).map { "tag$it" }.toSet(), progress = data)
        val row = workInfoSnapshot(info, observedStopReason = null, instanceId = 1)
        assertEquals(8, row.tags.size)
        assertEquals(13, row.omittedTags)
        assertEquals(2, row.progress.size)
        assertFalse(row.toString().contains("secret-value"))
        assertTrue(row.progress.any { it.key == "token" && it.type == "string" && it.payloadBytes == 16L })
        assertTrue(row.progress.any { it.key == "count" && it.type == "long" && it.payloadBytes == 8L })
    }
}
