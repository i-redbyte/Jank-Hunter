package io.jankhunter.runtime.internal.system

import org.junit.Assert.assertEquals
import org.junit.Test

class RuntimeGcStatsTest {
    @Test
    fun sampleReportsPositiveDeltasAndAllocationRate() {
        var now = 1_000L
        val stats = mutableMapOf(
            "art.gc.gc-count" to "10",
            "art.gc.gc-time" to "40",
            "art.gc.blocking-gc-count" to "2",
            "art.gc.blocking-gc-time" to "11",
            "art.gc.bytes-allocated" to "1000",
            "art.gc.bytes-freed" to "500",
        )
        val runtimeStats = RuntimeGcStats(
            readStat = { stats[it] },
            clockMs = { now },
        )

        assertEquals(RuntimeGcStats.Delta(), runtimeStats.sample())

        now = 2_000L
        stats["art.gc.gc-count"] = "13"
        stats["art.gc.gc-time"] = "55"
        stats["art.gc.blocking-gc-count"] = "3"
        stats["art.gc.blocking-gc-time"] = "20"
        stats["art.gc.bytes-allocated"] = "2200"
        stats["art.gc.bytes-freed"] = "900"

        assertEquals(
            RuntimeGcStats.Delta(
                gcCountDelta = 3,
                gcTimeMsDelta = 15,
                blockingGcCountDelta = 1,
                blockingGcTimeMsDelta = 9,
                bytesAllocatedDelta = 1200,
                bytesFreedDelta = 400,
                allocationRateBytesPerSec = 1200,
            ),
            runtimeStats.sample(),
        )
    }

    @Test
    fun sampleClampsCounterResetToZero() {
        var now = 1_000L
        val stats = mutableMapOf(
            "art.gc.gc-count" to "10",
            "art.gc.bytes-allocated" to "5000",
        )
        val runtimeStats = RuntimeGcStats(
            readStat = { stats[it] },
            clockMs = { now },
        )

        runtimeStats.sample()
        now = 1_500L
        stats["art.gc.gc-count"] = "1"
        stats["art.gc.bytes-allocated"] = "10"

        assertEquals(RuntimeGcStats.Delta(), runtimeStats.sample())
    }
}
