package io.jankhunter.runtime.internal.system

import android.os.Debug
import io.jankhunter.runtime.RuntimeCollectorCallbacks
import io.jankhunter.runtime.RuntimeBooleanSource

internal class MemorySampler(
    private val intervalMs: Long,
    private val callbacks: RuntimeCollectorCallbacks,
    private val userRelevant: RuntimeBooleanSource = RuntimeBooleanSource { true },
) {
    private val gcStats = RuntimeGcStats(::readRuntimeStat) { android.os.SystemClock.elapsedRealtime() }
    private val memoryPressureSampler = MemoryPressureSampler(callbacks::recordGauge)
    private val schedule = UserRelevantSamplingSchedule(intervalMs, userRelevant, ::sampleOnce)

    fun start(scheduler: RuntimeMaintenanceScheduler) {
        schedule.start(scheduler)
    }

    fun stop() {
        schedule.stop()
    }

    fun onUserRelevanceChanged() {
        schedule.onUserRelevanceChanged()
    }

    private fun sampleOnce() {
        val info = Debug.MemoryInfo()
        Debug.getMemoryInfo(info)
        val runtime = Runtime.getRuntime()
        val javaHeapKb = (runtime.totalMemory() - runtime.freeMemory()) / 1024L
        val nativeHeapKb = Debug.getNativeHeapAllocatedSize() / 1024L
        callbacks.recordMemory(info.totalPss.toLong(), javaHeapKb, nativeHeapKb)
        recordHeapPressure(runtime, nativeHeapKb)
        memoryPressureSampler.sample()
    }

    private fun recordHeapPressure(runtime: Runtime, nativeHeapKb: Long) {
        val javaUsedKb = (runtime.totalMemory() - runtime.freeMemory()) / 1024L
        callbacks.recordGauge("memory.java_heap.used_kb", javaUsedKb)
        callbacks.recordGauge("memory.java_heap.free_kb", runtime.freeMemory() / 1024L)
        callbacks.recordGauge("memory.java_heap.max_kb", runtime.maxMemory() / 1024L)
        callbacks.recordGauge("memory.native_heap.allocated_kb", nativeHeapKb)
        callbacks.recordGauge("memory.native_heap.free_kb", Debug.getNativeHeapFreeSize() / 1024L)
        callbacks.recordGauge("memory.native_heap.size_kb", Debug.getNativeHeapSize() / 1024L)

        val delta = gcStats.sample()
        recordPositiveCounter("gc.count.delta", delta.gcCountDelta)
        recordPositiveCounter("gc.time_ms.delta", delta.gcTimeMsDelta)
        recordPositiveCounter("gc.blocking_count.delta", delta.blockingGcCountDelta)
        recordPositiveCounter("gc.blocking_time_ms.delta", delta.blockingGcTimeMsDelta)
        recordPositiveCounter("gc.bytes_allocated.delta", delta.bytesAllocatedDelta)
        recordPositiveCounter("gc.bytes_freed.delta", delta.bytesFreedDelta)
        callbacks.recordGauge("memory.allocation_rate_bytes_per_sec", delta.allocationRateBytesPerSec)
    }

    private fun recordPositiveCounter(name: String, value: Long) {
        if (value > 0) {
            callbacks.recordCounter(name, value)
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
