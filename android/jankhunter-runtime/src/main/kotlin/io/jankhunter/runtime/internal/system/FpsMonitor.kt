package io.jankhunter.runtime.internal.system

import android.os.Handler
import android.os.Looper
import android.view.Choreographer
import io.jankhunter.runtime.JankHunter
import java.util.Arrays
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.math.floor
import kotlin.math.max

class FpsMonitor(
    windowMs: Long,
    jankFrameThresholdMs: Long,
) : Choreographer.FrameCallback {
    private val mainHandler = Handler(Looper.getMainLooper())
    private val running = AtomicBoolean(false)
    private val windowNanos = max(250L, windowMs) * 1_000_000L
    private val jankFrameThresholdMs = max(1L, jankFrameThresholdMs)

    private var choreographer: Choreographer? = null
    private var windowStartNanos = 0L
    private var lastFrameTimeNanos = 0L
    private var frameCount = 0L
    private var jankCount = 0L
    private var frameDurationsMs = LongArray(180)
    private var durationCount = 0

    fun start() {
        if (!running.compareAndSet(false, true)) return
        mainHandler.post {
            choreographer = Choreographer.getInstance()
            reset()
            choreographer?.postFrameCallback(this)
        }
    }

    fun stop() {
        running.set(false)
        mainHandler.post {
            choreographer?.removeFrameCallback(this)
            reset()
        }
    }

    override fun doFrame(frameTimeNanos: Long) {
        if (!running.get()) return

        if (windowStartNanos == 0L) {
            windowStartNanos = frameTimeNanos
            lastFrameTimeNanos = frameTimeNanos
            postNext()
            return
        }

        val frameDurationMs = max(0L, (frameTimeNanos - lastFrameTimeNanos) / 1_000_000L)
        lastFrameTimeNanos = frameTimeNanos
        frameCount++
        if (frameDurationMs >= jankFrameThresholdMs) {
            jankCount++
        }
        recordDuration(frameDurationMs)

        val elapsedNanos = frameTimeNanos - windowStartNanos
        if (elapsedNanos >= windowNanos) {
            val windowMs = max(1L, elapsedNanos / 1_000_000L)
            JankHunter.recordUiWindow(
                JankHunter.currentScreen(),
                windowMs,
                frameCount,
                jankCount,
                percentile(50),
                percentile(95),
                percentile(99),
            )
            JankHunter.recordGauge("ui.fps_x100", (frameCount * 100_000L) / windowMs)
            resetWindow(frameTimeNanos)
        }

        postNext()
    }

    private fun postNext() {
        val local = choreographer
        if (local != null && running.get()) {
            local.postFrameCallback(this)
        }
    }

    private fun reset() {
        windowStartNanos = 0L
        lastFrameTimeNanos = 0L
        frameCount = 0L
        jankCount = 0L
        durationCount = 0
    }

    private fun resetWindow(frameTimeNanos: Long) {
        windowStartNanos = frameTimeNanos
        frameCount = 0L
        jankCount = 0L
        durationCount = 0
    }

    private fun recordDuration(value: Long) {
        if (durationCount == frameDurationsMs.size) {
            frameDurationsMs = Arrays.copyOf(frameDurationsMs, frameDurationsMs.size * 2)
        }
        frameDurationsMs[durationCount++] = value
    }

    private fun percentile(percentile: Int): Long {
        if (durationCount == 0) return 0L
        val copy = Arrays.copyOf(frameDurationsMs, durationCount)
        Arrays.sort(copy)
        var index = floor((copy.size - 1) * (percentile / 100.0)).toInt()
        if (index < 0) index = 0
        if (index >= copy.size) index = copy.size - 1
        return copy[index]
    }
}
