package io.jankhunter.runtime.internal.system

import io.jankhunter.runtime.internal.io.Jhlog
import java.util.TreeMap
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
    private val overflowStartMs = maxExactDurationMs.coerceIn(1, DEFAULT_MAX_EXACT_DURATION_MS) + 1
    private val bins = LongArray(overflowStartMs)
    private val overflowBins = TreeMap<Long, Long>()
    private val mergeableBuckets = LongArray(Jhlog.UI_FRAME_HISTOGRAM_BUCKET_COUNT)
    private var count = 0L

    var p95Ms: Long = 0L
        private set

    fun add(durationMs: Long) {
        val safeDuration = durationMs.coerceAtLeast(0L)
        if (safeDuration < overflowStartMs) {
            val index = safeDuration.toInt()
            if (bins[index] < Long.MAX_VALUE) bins[index]++
        } else {
            val current = overflowBins[safeDuration] ?: 0L
            if (current < Long.MAX_VALUE) overflowBins[safeDuration] = current + 1L
        }
        if (count < Long.MAX_VALUE) count++
        val durationUs = if (safeDuration > Long.MAX_VALUE / 1_000L) Long.MAX_VALUE else safeDuration * 1_000L
        val bucket = Jhlog.UI_FRAME_HISTOGRAM_UPPER_US.indexOfFirst { durationUs <= it }
            .let { if (it >= 0) it else mergeableBuckets.lastIndex }
        if (mergeableBuckets[bucket] < Long.MAX_VALUE) mergeableBuckets[bucket]++
    }

    fun calculatePercentiles() {
        if (count == 0L) {
            p95Ms = 0L
            return
        }
        val p95Target = percentileIndex(95)
        var seen = 0L
        for (index in bins.indices) {
            seen += bins[index]
            if (seen > p95Target) {
                p95Ms = index.toLong()
                return
            }
        }
        for ((duration, durationCount) in overflowBins) {
            seen += durationCount
            if (seen > p95Target) {
                p95Ms = duration
                return
            }
        }
    }

    fun clear() {
        bins.fill(0L)
        overflowBins.clear()
        mergeableBuckets.fill(0L)
        count = 0L
        p95Ms = 0L
    }

    fun mergeableBuckets(): LongArray = mergeableBuckets.copyOf()

    private fun percentileIndex(percentile: Long): Long {
        val lastIndex = count - 1L
        return (lastIndex / 100L) * percentile + ((lastIndex % 100L) * percentile) / 100L
    }

    private companion object {
        private const val DEFAULT_MAX_EXACT_DURATION_MS = 510
    }
}

internal data class FrameWindowSnapshot(
    val screen: String?,
    val elapsedMs: Long,
    val frameCount: Long,
    val jankCount: Long,
    val p95Ms: Long,
    val frameDurationBuckets: LongArray,
)

internal class FrameWindowAccumulator(
    private val windowNanos: Long,
    private val preservePartialWindows: Boolean,
) {
    private var screen: String? = null
    private var startNanos = 0L
    private var frameCount = 0L
    private var jankCount = 0L
    private val durationHistogram = FrameDurationHistogram()

    fun add(
        nextScreen: String?,
        frameTimeNanos: Long,
        durationMs: Long,
        isJank: Boolean,
    ): FrameWindowSnapshot? {
        var completed: FrameWindowSnapshot? = null
        if (frameCount > 0L && screen != nextScreen) {
            completed = if (preservePartialWindows) finish(frameTimeNanos) else null
            if (!preservePartialWindows) reset(frameTimeNanos)
        }
        if (screen == null) screen = nextScreen
        if (startNanos == 0L) {
            val durationNanos = millisecondsToNanos(durationMs)
            startNanos = if (durationNanos >= frameTimeNanos) 1L else frameTimeNanos - durationNanos
        }

        frameCount++
        if (isJank) {
            jankCount++
        }
        durationHistogram.add(durationMs)

        val elapsedNanos = (frameTimeNanos - startNanos).coerceAtLeast(0L)
        if (elapsedNanos >= windowNanos) {
            check(completed == null)
            completed = finish(frameTimeNanos)
            screen = nextScreen
        }
        return completed
    }

    fun finish(endNanos: Long): FrameWindowSnapshot? {
        if (frameCount == 0L) {
            reset()
            return null
        }
        durationHistogram.calculatePercentiles()
        val snapshot = FrameWindowSnapshot(
            screen = screen,
            elapsedMs = maxOf(1L, (endNanos - startNanos).coerceAtLeast(0L) / NANOS_PER_MS),
            frameCount = frameCount,
            jankCount = jankCount,
            p95Ms = durationHistogram.p95Ms,
            frameDurationBuckets = durationHistogram.mergeableBuckets(),
        )
        reset(endNanos)
        return snapshot
    }

    fun reset(nextStartNanos: Long = 0L) {
        startNanos = nextStartNanos
        screen = null
        frameCount = 0L
        jankCount = 0L
        durationHistogram.clear()
    }

    private fun millisecondsToNanos(milliseconds: Long): Long {
        val positive = milliseconds.coerceAtLeast(0L)
        return if (positive > Long.MAX_VALUE / NANOS_PER_MS) Long.MAX_VALUE else positive * NANOS_PER_MS
    }

    private companion object {
        private const val NANOS_PER_MS = 1_000_000L
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
    var windowActive: Boolean = false
        private set

    var jankStatsActive: Boolean = false
        private set

    fun updateWindowActive(active: Boolean): Boolean {
        if (windowActive == active) return false
        windowActive = active
        return true
    }

    fun updateJankStats(active: Boolean): Boolean {
        if (jankStatsActive == active) return false
        jankStatsActive = active
        return true
    }

    fun useJankStats(): Boolean = windowActive && jankStatsActive

    fun useFallback(): Boolean = fallbackEnabled && windowActive && !jankStatsActive
}
