package io.jankhunter.runtime.internal.system

import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong

internal class CollectorRunState {
    private val running = AtomicBoolean(false)
    private val generation = AtomicLong()

    fun start(): Long? {
        if (!running.compareAndSet(false, true)) return null
        return generation.incrementAndGet()
    }

    fun stop(): Boolean {
        if (!running.getAndSet(false)) return false
        generation.incrementAndGet()
        return true
    }

    fun isRunning(): Boolean = running.get()

    fun isCurrent(expectedGeneration: Long): Boolean {
        return running.get() && generation.get() == expectedGeneration
    }
}

internal class StallEpisodeTracker(
    private val thresholdMs: Long,
) {
    private var stalledSinceMs = NO_EPISODE

    var completedDurationMs: Long = 0L
        private set

    fun update(lastBeatMs: Long, nowMs: Long): StallEpisodeChange {
        val activeStart = stalledSinceMs
        if (activeStart != NO_EPISODE) {
            if (lastBeatMs == activeStart) return StallEpisodeChange.NONE
            completedDurationMs = (lastBeatMs - activeStart).coerceAtLeast(0L)
            stalledSinceMs = NO_EPISODE
            return StallEpisodeChange.RECOVERED
        }

        val delayMs = (nowMs - lastBeatMs).coerceAtLeast(0L)
        if (delayMs < thresholdMs) return StallEpisodeChange.NONE
        stalledSinceMs = lastBeatMs
        return StallEpisodeChange.STARTED
    }

    private companion object {
        private const val NO_EPISODE = Long.MIN_VALUE
    }
}

internal enum class StallEpisodeChange {
    NONE,
    STARTED,
    RECOVERED,
}

internal class FrameDurationHistogram(
    maxExactDurationMs: Int = DEFAULT_MAX_EXACT_DURATION_MS,
) {
    private val overflowBin = maxExactDurationMs.coerceIn(1, DEFAULT_MAX_EXACT_DURATION_MS) + 1
    private val bins = LongArray(overflowBin + 1)
    private var count = 0L
    private var maxObservedMs = 0L

    var p50Ms: Long = 0L
        private set
    var p95Ms: Long = 0L
        private set
    var p99Ms: Long = 0L
        private set

    fun add(durationMs: Long) {
        val safeDuration = durationMs.coerceAtLeast(0L)
        val index = if (safeDuration >= overflowBin) overflowBin else safeDuration.toInt()
        if (bins[index] < Long.MAX_VALUE) bins[index]++
        if (count < Long.MAX_VALUE) count++
        if (safeDuration > maxObservedMs) maxObservedMs = safeDuration
    }

    fun calculatePercentiles() {
        if (count == 0L) {
            p50Ms = 0L
            p95Ms = 0L
            p99Ms = 0L
            return
        }
        val p50Target = percentileIndex(50)
        val p95Target = percentileIndex(95)
        val p99Target = percentileIndex(99)
        var seen = 0L
        var found50 = false
        var found95 = false
        for (index in bins.indices) {
            seen += bins[index]
            val duration = if (index == overflowBin) maxObservedMs else index.toLong()
            if (!found50 && seen > p50Target) {
                p50Ms = duration
                found50 = true
            }
            if (!found95 && seen > p95Target) {
                p95Ms = duration
                found95 = true
            }
            if (seen > p99Target) {
                p99Ms = duration
                return
            }
        }
        p99Ms = maxObservedMs
    }

    fun clear() {
        bins.fill(0L)
        count = 0L
        maxObservedMs = 0L
        p50Ms = 0L
        p95Ms = 0L
        p99Ms = 0L
    }

    private fun percentileIndex(percentile: Long): Long {
        val lastIndex = count - 1L
        return (lastIndex / 100L) * percentile + ((lastIndex % 100L) * percentile) / 100L
    }

    private companion object {
        private const val DEFAULT_MAX_EXACT_DURATION_MS = 510
    }
}

internal class LastResumedRegistry<T : Any> {
    private val values = linkedSetOf<T>()

    fun onResumed(value: T) {
        values.remove(value)
        values.add(value)
    }

    fun onNotResumed(value: T) {
        values.remove(value)
    }

    fun latestMatching(predicate: (T) -> Boolean): T? {
        var latest: T? = null
        for (value in values) {
            if (predicate(value)) latest = value
        }
        return latest
    }

    fun clear() = values.clear()
}

internal class FrameSourceSelector(
    private val fallbackEnabled: Boolean,
) {
    var jankStatsActive: Boolean = false
        private set

    fun updateJankStats(active: Boolean): Boolean {
        if (jankStatsActive == active) return false
        jankStatsActive = active
        return true
    }

    fun useJankStats(): Boolean = jankStatsActive

    fun useFallback(): Boolean = fallbackEnabled && !jankStatsActive
}
