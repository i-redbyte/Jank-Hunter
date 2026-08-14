package io.jankhunter.runtime.internal.system

import android.os.Debug
import io.jankhunter.runtime.JankHunter

internal class MemorySampler(
    private val intervalMs: Long,
    private val foreground: () -> Boolean = { true },
) {
    private val gcStats = RuntimeGcStats(::readRuntimeStat) { android.os.SystemClock.elapsedRealtime() }
    private val schedule = ForegroundSamplingSchedule(intervalMs, foreground, ::sampleOnce)

    fun start(scheduler: RuntimeMaintenanceScheduler) {
        schedule.start(scheduler)
    }

    fun stop() {
        schedule.stop()
    }

    fun onForegroundChanged() {
        schedule.onForegroundChanged()
    }

    private fun sampleOnce() {
        val info = Debug.MemoryInfo()
        Debug.getMemoryInfo(info)
        val runtime = Runtime.getRuntime()
        val javaHeapKb = (runtime.totalMemory() - runtime.freeMemory()) / 1024L
        val nativeHeapKb = Debug.getNativeHeapAllocatedSize() / 1024L
        JankHunter.recordMemory(info.totalPss.toLong(), javaHeapKb, nativeHeapKb)
        recordHeapPressure(runtime, nativeHeapKb)
    }

    private fun recordHeapPressure(runtime: Runtime, nativeHeapKb: Long) {
        val javaUsedKb = (runtime.totalMemory() - runtime.freeMemory()) / 1024L
        JankHunter.recordGauge("memory.java_heap.used_kb", javaUsedKb)
        JankHunter.recordGauge("memory.java_heap.free_kb", runtime.freeMemory() / 1024L)
        JankHunter.recordGauge("memory.java_heap.max_kb", runtime.maxMemory() / 1024L)
        JankHunter.recordGauge("memory.native_heap.allocated_kb", nativeHeapKb)
        JankHunter.recordGauge("memory.native_heap.free_kb", Debug.getNativeHeapFreeSize() / 1024L)
        JankHunter.recordGauge("memory.native_heap.size_kb", Debug.getNativeHeapSize() / 1024L)

        val delta = gcStats.sample()
        recordPositiveCounter("gc.count.delta", delta.gcCountDelta)
        recordPositiveCounter("gc.time_ms.delta", delta.gcTimeMsDelta)
        recordPositiveCounter("gc.blocking_count.delta", delta.blockingGcCountDelta)
        recordPositiveCounter("gc.blocking_time_ms.delta", delta.blockingGcTimeMsDelta)
        recordPositiveCounter("gc.bytes_allocated.delta", delta.bytesAllocatedDelta)
        recordPositiveCounter("gc.bytes_freed.delta", delta.bytesFreedDelta)
        JankHunter.recordGauge("memory.allocation_rate_bytes_per_sec", delta.allocationRateBytesPerSec)
    }

    private fun recordPositiveCounter(name: String, value: Long) {
        if (value > 0) {
            JankHunter.recordCounter(name, value)
        }
    }

    private fun readRuntimeStat(key: String): String? {
        return try {
            Debug.getRuntimeStat(key)
        } catch (_: Exception) {
            null
        }
    }

}
