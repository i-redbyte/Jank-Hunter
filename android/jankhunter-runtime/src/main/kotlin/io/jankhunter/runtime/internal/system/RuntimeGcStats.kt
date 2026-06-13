package io.jankhunter.runtime.internal.system

internal class RuntimeGcStats(
    private val readStat: (String) -> String?,
    private val clockMs: () -> Long,
) {
    private var last: Snapshot? = null

    fun sample(): Delta {
        val nowMs = clockMs()
        val current = Snapshot(
            atMs = nowMs,
            gcCount = statLong("art.gc.gc-count"),
            gcTimeMs = statLong("art.gc.gc-time"),
            blockingGcCount = statLong("art.gc.blocking-gc-count"),
            blockingGcTimeMs = statLong("art.gc.blocking-gc-time"),
            bytesAllocated = statLong("art.gc.bytes-allocated"),
            bytesFreed = statLong("art.gc.bytes-freed"),
        )
        val previous = last
        last = current
        if (previous == null) {
            return Delta()
        }
        val elapsedMs = (current.atMs - previous.atMs).coerceAtLeast(1L)
        val allocatedDelta = positiveDelta(current.bytesAllocated, previous.bytesAllocated)
        return Delta(
            gcCountDelta = positiveDelta(current.gcCount, previous.gcCount),
            gcTimeMsDelta = positiveDelta(current.gcTimeMs, previous.gcTimeMs),
            blockingGcCountDelta = positiveDelta(current.blockingGcCount, previous.blockingGcCount),
            blockingGcTimeMsDelta = positiveDelta(current.blockingGcTimeMs, previous.blockingGcTimeMs),
            bytesAllocatedDelta = allocatedDelta,
            bytesFreedDelta = positiveDelta(current.bytesFreed, previous.bytesFreed),
            allocationRateBytesPerSec = allocatedDelta * 1000L / elapsedMs,
        )
    }

    private fun statLong(key: String): Long {
        return readStat(key)?.toLongOrNull()?.coerceAtLeast(0L) ?: 0L
    }

    private fun positiveDelta(current: Long, previous: Long): Long {
        return if (current >= previous) current - previous else 0L
    }

    private data class Snapshot(
        val atMs: Long,
        val gcCount: Long,
        val gcTimeMs: Long,
        val blockingGcCount: Long,
        val blockingGcTimeMs: Long,
        val bytesAllocated: Long,
        val bytesFreed: Long,
    )

    data class Delta(
        val gcCountDelta: Long = 0L,
        val gcTimeMsDelta: Long = 0L,
        val blockingGcCountDelta: Long = 0L,
        val blockingGcTimeMsDelta: Long = 0L,
        val bytesAllocatedDelta: Long = 0L,
        val bytesFreedDelta: Long = 0L,
        val allocationRateBytesPerSec: Long = 0L,
    )
}
