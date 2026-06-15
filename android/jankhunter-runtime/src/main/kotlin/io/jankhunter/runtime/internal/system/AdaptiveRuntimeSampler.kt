package io.jankhunter.runtime.internal.system

import kotlin.math.abs

class AdaptiveRuntimeSampler(
    private val memoryStableIntervalMs: Long,
    private val contextStableIntervalMs: Long,
) {
    private var lastMemory: MemorySnapshot? = null
    private var lastContext: ContextSnapshot? = null

    @Synchronized
    fun shouldRecordMemory(nowMs: Long, pssKb: Long, javaHeapKb: Long, nativeHeapKb: Long): Boolean {
        val next = MemorySnapshot(nowMs, pssKb, javaHeapKb, nativeHeapKb)
        val previous = lastMemory
        if (previous == null || nowMs - previous.timeMs >= memoryStableIntervalMs || previous.changedEnough(next)) {
            lastMemory = next
            return true
        }
        return false
    }

    @Synchronized
    fun shouldRecordContext(
        nowMs: Long,
        networkKind: Int,
        batteryPct: Int,
        availMemoryKb: Long,
        lowMemory: Boolean,
        networkMetered: Boolean,
        networkValidated: Boolean,
        rxBytes: Long,
        txBytes: Long,
        networkVpn: Boolean,
    ): Boolean {
        val next = ContextSnapshot(
            timeMs = nowMs,
            networkKind = networkKind,
            batteryPct = batteryPct,
            availMemoryKb = availMemoryKb,
            lowMemory = lowMemory,
            networkMetered = networkMetered,
            networkValidated = networkValidated,
            rxBytes = rxBytes,
            txBytes = txBytes,
            networkVpn = networkVpn,
        )
        val previous = lastContext
        if (previous == null || nowMs - previous.timeMs >= contextStableIntervalMs || previous.changedEnough(next)) {
            lastContext = next
            return true
        }
        return false
    }

    private data class MemorySnapshot(
        val timeMs: Long,
        val pssKb: Long,
        val javaHeapKb: Long,
        val nativeHeapKb: Long,
    ) {
        fun changedEnough(next: MemorySnapshot): Boolean {
            return abs(next.pssKb - pssKb) >= MEMORY_PSS_DELTA_KB ||
                abs(next.javaHeapKb - javaHeapKb) >= MEMORY_HEAP_DELTA_KB ||
                abs(next.nativeHeapKb - nativeHeapKb) >= MEMORY_HEAP_DELTA_KB
        }
    }

    private data class ContextSnapshot(
        val timeMs: Long,
        val networkKind: Int,
        val batteryPct: Int,
        val availMemoryKb: Long,
        val lowMemory: Boolean,
        val networkMetered: Boolean,
        val networkValidated: Boolean,
        val rxBytes: Long,
        val txBytes: Long,
        val networkVpn: Boolean,
    ) {
        fun changedEnough(next: ContextSnapshot): Boolean {
            return next.lowMemory ||
                next.networkKind != networkKind ||
                next.networkMetered != networkMetered ||
                next.networkValidated != networkValidated ||
                next.networkVpn != networkVpn ||
                abs(next.batteryPct - batteryPct) >= BATTERY_DELTA_PCT ||
                abs(next.availMemoryKb - availMemoryKb) >= AVAILABLE_MEMORY_DELTA_KB ||
                abs(next.rxBytes - rxBytes) >= TRAFFIC_DELTA_BYTES ||
                abs(next.txBytes - txBytes) >= TRAFFIC_DELTA_BYTES
        }
    }

    companion object {
        private const val MEMORY_PSS_DELTA_KB = 4 * 1024L
        private const val MEMORY_HEAP_DELTA_KB = 2 * 1024L
        private const val AVAILABLE_MEMORY_DELTA_KB = 32 * 1024L
        private const val TRAFFIC_DELTA_BYTES = 1024 * 1024L
        private const val BATTERY_DELTA_PCT = 2
    }
}
