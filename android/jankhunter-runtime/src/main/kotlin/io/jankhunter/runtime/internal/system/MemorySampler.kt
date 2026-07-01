package io.jankhunter.runtime.internal.system

import android.content.Context
import android.os.Debug
import android.os.SystemClock
import io.jankhunter.runtime.JankHunter
import java.util.concurrent.atomic.AtomicBoolean

internal class MemorySampler(
    @Suppress("UNUSED_PARAMETER") context: Context,
    private val intervalMs: Long,
    private val foreground: () -> Boolean = { true },
) {
    private val running = AtomicBoolean(false)
    private val gcStats = RuntimeGcStats(::readRuntimeStat) { SystemClock.elapsedRealtime() }
    private var thread: Thread? = null

    fun start() {
        if (!running.compareAndSet(false, true)) return
        thread = Thread({ loop() }, "JankHunterMemorySampler").apply {
            isDaemon = true
            start()
        }
    }

    fun stop() {
        running.set(false)
        thread?.interrupt()
    }

    private fun loop() {
        val info = Debug.MemoryInfo()
        while (running.get()) {
            Debug.getMemoryInfo(info)
            val runtime = Runtime.getRuntime()
            val javaHeapKb = (runtime.totalMemory() - runtime.freeMemory()) / 1024L
            val nativeHeapKb = Debug.getNativeHeapAllocatedSize() / 1024L
            JankHunter.recordMemory(info.totalPss.toLong(), javaHeapKb, nativeHeapKb)
            recordHeapPressure(runtime, nativeHeapKb)
            sleepUntilNextSample()
        }
    }

    private fun sleepUntilNextSample() {
        val startedAtMs = SystemClock.elapsedRealtime()
        while (running.get()) {
            val remainingMs = currentIntervalMs() - (SystemClock.elapsedRealtime() - startedAtMs)
            if (remainingMs <= 0L) return
            try {
                Thread.sleep(minOf(remainingMs, STATE_POLL_MS))
            } catch (_: InterruptedException) {
                Thread.currentThread().interrupt()
                return
            }
        }
    }

    private fun currentIntervalMs(): Long {
        val foregroundInterval = intervalMs.coerceAtLeast(1_000L)
        if (foreground()) return foregroundInterval
        val backgroundInterval = if (foregroundInterval > Long.MAX_VALUE / BACKGROUND_INTERVAL_MULTIPLIER) {
            Long.MAX_VALUE
        } else {
            foregroundInterval * BACKGROUND_INTERVAL_MULTIPLIER
        }
        return maxOf(MIN_BACKGROUND_INTERVAL_MS, backgroundInterval)
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

    private companion object {
        private const val BACKGROUND_INTERVAL_MULTIPLIER = 12L
        private const val MIN_BACKGROUND_INTERVAL_MS = 2 * 60_000L
        private const val STATE_POLL_MS = 5_000L
    }
}
