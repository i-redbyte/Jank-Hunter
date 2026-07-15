package io.jankhunter.runtime.internal.system

import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import android.view.Choreographer
import io.jankhunter.runtime.JankHunter
import kotlin.math.max

/**
 * Canonical UI frame pipeline.
 *
 * JankStats supplies real frame durations whenever it is tracking a resumed window. Choreographer
 * is enabled only as a fallback, so a frame can never be admitted by both sources.
 */
internal class FpsMonitor(
    windowMs: Long,
    jankFrameThresholdMs: Long,
    choreographerFallbackEnabled: Boolean = true,
) : Choreographer.FrameCallback {
    private val mainHandler = Handler(Looper.getMainLooper())
    private val runState = CollectorRunState()
    private val sourceSelector = FrameSourceSelector(choreographerFallbackEnabled)
    private val windowNanos = millisecondsToNanos(max(250L, windowMs))
    private val jankFrameThresholdMs = max(1L, jankFrameThresholdMs)

    private var choreographer: Choreographer? = null
    private var callbackPosted = false
    private var windowScreen: String? = null
    private var windowStartNanos = 0L
    private var lastFallbackFrameNanos = 0L
    private var frameCount = 0L
    private var jankCount = 0L
    private var deadlineMissCount = 0L
    private var maxFrameOverrunMs = 0L
    private var maxFrameDurationMs = 0L
    private val durationHistogram = FrameDurationHistogram()

    fun start() {
        val expectedGeneration = runState.start() ?: return
        mainHandler.post {
            if (!isCurrent(expectedGeneration)) return@post
            choreographer = Choreographer.getInstance()
            resetWindow()
            updateFallbackRegistration()
        }
    }

    fun stop() {
        if (!runState.stop()) return
        mainHandler.post {
            removeFallbackCallback()
            sourceSelector.updateJankStats(false)
            resetWindow()
            choreographer = null
        }
    }

    fun setJankStatsActive(active: Boolean, sourceChanged: Boolean = false) {
        runOnMain {
            if (!runState.isRunning()) return@runOnMain
            val activeChanged = sourceSelector.updateJankStats(active)
            if (!activeChanged && !sourceChanged) return@runOnMain
            resetWindow()
            if (activeChanged) {
                updateFallbackRegistration()
                JankHunter.recordGauge("ui.frame.source.jankstats", if (active) 1L else 0L)
            }
        }
    }

    fun onJankStatsFrame(screen: String?, durationNanos: Long, isJank: Boolean) {
        runOnMain {
            if (!runState.isRunning() || !sourceSelector.useJankStats()) return@runOnMain
            recordFrame(
                screen = screen,
                frameTimeNanos = SystemClock.elapsedRealtimeNanos(),
                durationMs = durationNanos.coerceAtLeast(0L) / NANOS_PER_MS,
                isJank = isJank,
            )
        }
    }

    override fun doFrame(frameTimeNanos: Long) {
        callbackPosted = false
        if (!shouldUseFallback()) return

        val previousFrameNanos = lastFallbackFrameNanos
        lastFallbackFrameNanos = frameTimeNanos
        if (previousFrameNanos != 0L) {
            val durationMs = ((frameTimeNanos - previousFrameNanos).coerceAtLeast(0L)) / NANOS_PER_MS
            recordFrame(
                screen = JankHunter.currentScreen(),
                frameTimeNanos = frameTimeNanos,
                durationMs = durationMs,
                isJank = durationMs >= jankFrameThresholdMs,
            )
        } else {
            windowStartNanos = frameTimeNanos
        }
        postFallbackCallback()
    }

    private fun recordFrame(
        screen: String?,
        frameTimeNanos: Long,
        durationMs: Long,
        isJank: Boolean,
    ) {
        if (windowScreen != null && screen != null && windowScreen != screen) {
            resetWindow(frameTimeNanos)
        }
        if (windowScreen == null) {
            windowScreen = screen
        }
        if (windowStartNanos == 0L) {
            val durationNanos = millisecondsToNanos(durationMs)
            windowStartNanos = if (durationNanos >= frameTimeNanos) 1L else frameTimeNanos - durationNanos
        }
        val safeDurationMs = durationMs.coerceAtLeast(0L)
        frameCount++
        if (isJank) {
            jankCount++
            deadlineMissCount++
            maxFrameOverrunMs = max(maxFrameOverrunMs, safeDurationMs - jankFrameThresholdMs)
        }
        maxFrameDurationMs = max(maxFrameDurationMs, safeDurationMs)
        durationHistogram.add(safeDurationMs)

        val elapsedNanos = (frameTimeNanos - windowStartNanos).coerceAtLeast(0L)
        if (elapsedNanos < windowNanos) return

        val elapsedMs = max(1L, elapsedNanos / NANOS_PER_MS)
        durationHistogram.calculatePercentiles()
        JankHunter.recordUiWindow(
            windowScreen,
            elapsedMs,
            frameCount,
            jankCount,
            durationHistogram.p50Ms,
            durationHistogram.p95Ms,
            durationHistogram.p99Ms,
        )
        JankHunter.recordGauge("ui.fps_x100", saturatedFpsX100(frameCount, elapsedMs))
        JankHunter.recordGauge("ui.frame_deadline_miss.count", deadlineMissCount)
        JankHunter.recordGauge("ui.frame_overrun.max_ms", maxFrameOverrunMs)
        JankHunter.recordGauge("ui.frame_duration.max_ms", maxFrameDurationMs)
        resetWindow(frameTimeNanos)
        windowScreen = screen
    }

    private fun updateFallbackRegistration() {
        if (shouldUseFallback()) {
            postFallbackCallback()
        } else {
            removeFallbackCallback()
        }
    }

    private fun shouldUseFallback(): Boolean {
        return runState.isRunning() && sourceSelector.useFallback()
    }

    private fun postFallbackCallback() {
        val local = choreographer ?: return
        if (!callbackPosted && shouldUseFallback()) {
            callbackPosted = true
            local.postFrameCallback(this)
        }
    }

    private fun removeFallbackCallback() {
        if (callbackPosted) {
            choreographer?.removeFrameCallback(this)
            callbackPosted = false
        }
        lastFallbackFrameNanos = 0L
    }

    private fun resetWindow(frameTimeNanos: Long = 0L) {
        windowStartNanos = frameTimeNanos
        windowScreen = null
        lastFallbackFrameNanos = 0L
        frameCount = 0L
        jankCount = 0L
        deadlineMissCount = 0L
        maxFrameOverrunMs = 0L
        maxFrameDurationMs = 0L
        durationHistogram.clear()
    }

    private fun saturatedFpsX100(frames: Long, elapsedMs: Long): Long {
        return if (frames > Long.MAX_VALUE / FPS_SCALE) {
            Long.MAX_VALUE / elapsedMs
        } else {
            frames * FPS_SCALE / elapsedMs
        }
    }

    private fun runOnMain(block: () -> Unit) {
        if (Looper.myLooper() == Looper.getMainLooper()) {
            block()
        } else {
            mainHandler.post(block)
        }
    }

    private fun isCurrent(expectedGeneration: Long): Boolean {
        return runState.isCurrent(expectedGeneration)
    }

    private fun millisecondsToNanos(milliseconds: Long): Long {
        val positive = milliseconds.coerceAtLeast(0L)
        return if (positive > Long.MAX_VALUE / NANOS_PER_MS) Long.MAX_VALUE else positive * NANOS_PER_MS
    }

    private companion object {
        private const val NANOS_PER_MS = 1_000_000L
        private const val FPS_SCALE = 100_000L
    }
}
