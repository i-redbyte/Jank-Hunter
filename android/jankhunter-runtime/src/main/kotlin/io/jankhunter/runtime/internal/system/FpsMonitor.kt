package io.jankhunter.runtime.internal.system

import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import android.view.Choreographer
import io.jankhunter.runtime.RuntimeCollectorCallbacks
import io.jankhunter.runtime.RuntimeHookGuard
import io.jankhunter.runtime.RuntimeHookFailureTracker
import io.jankhunter.runtime.RuntimeHookFailureReason
import io.jankhunter.runtime.internal.io.Jhlog
import java.util.concurrent.CountDownLatch
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
    private val callbacks: RuntimeCollectorCallbacks,
    choreographerFallbackEnabled: Boolean = true,
    private val exactAdmission: Boolean = false,
) : Choreographer.FrameCallback {
    private val mainHandler = Handler(Looper.getMainLooper())
    private val runState = CollectorRunState()
    private val sourceSelector = FrameSourceSelector(choreographerFallbackEnabled)
    private val windowNanos = millisecondsToNanos(max(250L, windowMs))
    private val jankFrameThresholdMs = max(1L, jankFrameThresholdMs)
    private val window = FrameWindowAccumulator(windowNanos, exactAdmission)

    private var choreographer: Choreographer? = null
    private var callbackPosted = false
    private var lastFallbackFrameNanos = 0L

    fun start() {
        val expectedGeneration = runState.start() ?: return
        runOnMain {
            if (!isCurrent(expectedGeneration)) return@runOnMain
            choreographer = Choreographer.getInstance()
            resetWindow()
            updateFallbackRegistration()
        }
    }

    fun stop() {
        if (!runState.stop()) return
        runOnMain(waitForCompletion = exactAdmission) {
            if (exactAdmission) finishWindow(SystemClock.elapsedRealtimeNanos())
            removeFallbackCallback()
            sourceSelector.updateJankStats(false)
            resetWindow()
            choreographer = null
        }
    }

    fun setJankStatsActive(active: Boolean, sourceChanged: Boolean = false) {
        runOnMain {
            if (!runState.isRunning()) return@runOnMain
            val activeChanged = sourceSelector.jankStatsActive != active
            if (!activeChanged && !sourceChanged) return@runOnMain
            if (exactAdmission) finishWindow(SystemClock.elapsedRealtimeNanos())
            sourceSelector.updateJankStats(active)
            resetWindow()
            if (activeChanged) {
                updateFallbackRegistration()
            }
        }
    }

    fun setWindowActive(active: Boolean) {
        runOnMain {
            if (!runState.isRunning() || !sourceSelector.updateWindowActive(active)) return@runOnMain
            if (exactAdmission) finishWindow(SystemClock.elapsedRealtimeNanos())
            resetWindow()
            updateFallbackRegistration()
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

        RuntimeHookGuard.run {
            val previousFrameNanos = lastFallbackFrameNanos
            lastFallbackFrameNanos = frameTimeNanos
            if (previousFrameNanos != 0L) {
                val durationMs = ((frameTimeNanos - previousFrameNanos).coerceAtLeast(0L)) / NANOS_PER_MS
                recordFrame(
                    screen = callbacks.currentScreen(),
                    frameTimeNanos = frameTimeNanos,
                    durationMs = durationMs,
                    isJank = durationMs >= jankFrameThresholdMs,
                )
            } else {
                window.reset(frameTimeNanos)
            }
        }
        RuntimeHookGuard.run(::postFallbackCallback)
    }

    private fun recordFrame(
        screen: String?,
        frameTimeNanos: Long,
        durationMs: Long,
        isJank: Boolean,
    ) {
        window.add(screen, frameTimeNanos, durationMs, isJank)?.let(::emitWindow)
    }

    private fun emitWindow(snapshot: FrameWindowSnapshot) {
        val source = if (sourceSelector.useJankStats()) {
            Jhlog.UI_SOURCE_JANKSTATS
        } else {
            Jhlog.UI_SOURCE_CHOREOGRAPHER
        }
        callbacks.recordUiWindow(
            snapshot.screen,
            snapshot.elapsedMs,
            snapshot.frameCount,
            snapshot.jankCount,
            snapshot.p95Ms,
            source,
            jankFrameThresholdMs * NANOS_PER_MS / 1_000L,
            snapshot.frameDurationBuckets,
        )
    }

    private fun finishWindow(endNanos: Long) {
        window.finish(endNanos)?.let(::emitWindow)
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
            local.postFrameCallback(this)
            callbackPosted = true
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
        window.reset(frameTimeNanos)
        lastFallbackFrameNanos = 0L
    }

    private fun runOnMain(waitForCompletion: Boolean = false, block: () -> Unit) {
        if (Looper.myLooper() == Looper.getMainLooper()) {
            RuntimeHookGuard.run(block)
        } else {
            val completed = if (waitForCompletion) CountDownLatch(1) else null
            val accepted = mainHandler.post {
                try {
                    RuntimeHookGuard.run(block)
                } finally {
                    completed?.countDown()
                }
            }
            if (!accepted) {
                RuntimeHookFailureTracker.record(RuntimeHookFailureReason.COLLECTOR)
                return
            }
            if (completed != null) awaitUninterruptibly(completed)
        }
    }

    private fun awaitUninterruptibly(completed: CountDownLatch) {
        var interrupted = false
        while (true) {
            try {
                completed.await()
                break
            } catch (_: InterruptedException) {
                interrupted = true
            }
        }
        if (interrupted) Thread.currentThread().interrupt()
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
    }
}
