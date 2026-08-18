package io.jankhunter.runtime.internal.system

import io.jankhunter.runtime.internal.io.BinaryLogWriter
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class CollectorStateTest {
    @Test
    fun stallEpisodeCompletesOnceWithDurationFromLastBeatToRecovery() {
        val tracker = StallEpisodeTracker(thresholdMs = 700L)

        assertEquals(StallEpisodeChange.NONE, tracker.update(lastBeatMs = 1_000L, nowMs = 1_699L))
        assertEquals(StallEpisodeChange.STARTED, tracker.update(lastBeatMs = 1_000L, nowMs = 1_700L))
        assertEquals(StallEpisodeChange.NONE, tracker.update(lastBeatMs = 1_000L, nowMs = 2_400L))
        assertEquals(StallEpisodeChange.RECOVERED, tracker.update(lastBeatMs = 2_500L, nowMs = 2_550L))
        assertEquals(1_500L, tracker.completedDurationMs)
        assertEquals(StallEpisodeChange.NONE, tracker.update(lastBeatMs = 2_500L, nowMs = 2_600L))
    }

    @Test
    fun stopInvalidatesInterruptedRunAndRestartGetsNewGeneration() {
        val state = CollectorRunState()
        val first = requireNotNull(state.start())

        assertTrue(state.isCurrent(first))
        assertTrue(state.stop())
        assertFalse(state.isCurrent(first))

        val restarted = requireNotNull(state.start())
        assertNotEquals(first, restarted)
        assertFalse(state.isCurrent(first))
        assertTrue(state.isCurrent(restarted))
    }

    @Test
    fun jankStatsAndChoreographerFallbackAreMutuallyExclusive() {
        val selector = FrameSourceSelector(fallbackEnabled = true)

        assertTrue(selector.useFallback())
        assertFalse(selector.useJankStats())

        assertTrue(selector.updateJankStats(true))
        assertTrue(selector.useJankStats())
        assertFalse(selector.useFallback())

        assertTrue(selector.updateJankStats(false))
        assertFalse(selector.useJankStats())
        assertTrue(selector.useFallback())
    }

    @Test
    fun latestResumedWindowWinsAndPauseRestoresPreviousWindow() {
        val registry = LastResumedRegistry<String>()

        registry.onResumed("first")
        registry.onResumed("second")
        assertEquals("second", registry.latestMatching { true })

        registry.onNotResumed("second")
        assertEquals("first", registry.latestMatching { true })

        registry.onResumed("second")
        assertEquals("second", registry.latestMatching { it != "missing" })
    }

    @Test
    fun durationHistogramCalculatesCommonFramePercentilesExactly() {
        val histogram = FrameDurationHistogram(maxExactDurationMs = 100)
        repeat(100) { histogram.add(10L) }
        repeat(10) { histogram.add(1_000L) }

        histogram.calculatePercentiles()

        assertEquals(1_000L, histogram.p95Ms)
    }

    @Test
    fun durationHistogramKeepsDistinctSevereFrameDurations() {
        val histogram = FrameDurationHistogram(maxExactDurationMs = 100)
        repeat(6) { histogram.add(600L) }
        repeat(4) { histogram.add(1_000L) }

        histogram.calculatePercentiles()

        assertEquals(1_000L, histogram.p95Ms)
    }

    @Test
    fun exactFrameWindowsSealPartialEvidenceAtScreenAndShutdownBoundaries() {
        val windows = FrameWindowAccumulator(
            windowNanos = 1_000_000_000L,
            preservePartialWindows = true,
        )

        assertEquals(null, windows.add("Feed", 100_000_000L, 10L, isJank = false))
        val feed = requireNotNull(windows.add("Chat", 200_000_000L, 20L, isJank = true))
        val chat = requireNotNull(windows.finish(300_000_000L))

        assertEquals("Feed", feed.screen)
        assertEquals(1L, feed.frameCount)
        assertEquals(10L, feed.p95Ms)
        assertEquals("Chat", chat.screen)
        assertEquals(1L, chat.frameCount)
        assertEquals(1L, chat.jankCount)
        assertEquals(20L, chat.p95Ms)
    }

    @Test
    fun exactFrameWindowsDoNotReattributeUnknownScreenFrames() {
        val windows = FrameWindowAccumulator(
            windowNanos = 1_000_000_000L,
            preservePartialWindows = true,
        )

        assertEquals(null, windows.add(null, 100_000_000L, 10L, isJank = false))
        val unknown = requireNotNull(windows.add("Feed", 200_000_000L, 12L, isJank = false))

        assertEquals(null, unknown.screen)
        assertEquals(1L, unknown.frameCount)
    }

    @Test
    fun exactFrameWindowsRemainBoundedAcrossHighScreenCardinality() {
        val windows = FrameWindowAccumulator(
            windowNanos = 1_000_000_000L,
            preservePartialWindows = true,
        )
        var emitted = 0

        repeat(HIGH_CARDINALITY_SCREEN_COUNT) { index ->
            windows.add("Screen$index", index.toLong() + 1L, 1L, isJank = false)?.let { snapshot ->
                assertEquals(1L, snapshot.frameCount)
                emitted++
            }
        }
        windows.finish(HIGH_CARDINALITY_SCREEN_COUNT.toLong() + 1L)?.let { emitted++ }

        assertEquals(HIGH_CARDINALITY_SCREEN_COUNT, emitted)
    }

    @Test
    fun uiWindowAlwaysCarriesClassificationAndUsesConfiguredThreshold() {
        val belowCustomThreshold = UiWindowClassifier.flags(
            jankCount = 0L,
            p95Ms = 40L,
            problemP95ThresholdMs = 50L,
        )
        val aboveCustomThreshold = UiWindowClassifier.flags(
            jankCount = 0L,
            p95Ms = 50L,
            problemP95ThresholdMs = 50L,
        )

        assertTrue((belowCustomThreshold and BinaryLogWriter.FLAG_UI_CLASSIFIED) != 0L)
        assertFalse((belowCustomThreshold and BinaryLogWriter.FLAG_UI_PROBLEM) != 0L)
        assertTrue((aboveCustomThreshold and BinaryLogWriter.FLAG_UI_CLASSIFIED) != 0L)
        assertTrue((aboveCustomThreshold and BinaryLogWriter.FLAG_UI_PROBLEM) != 0L)
    }

    private companion object {
        const val HIGH_CARDINALITY_SCREEN_COUNT = 10_000
    }
}
