package io.jankhunter.runtime.internal.system

import android.os.Handler
import android.os.Looper
import android.view.Choreographer
import io.jankhunter.runtime.RuntimeCollectorCallbacks
import io.jankhunter.runtime.RuntimeHookGuard
import io.jankhunter.runtime.RuntimeHookFailureTracker
import io.jankhunter.runtime.RuntimeHookFailureReason
import io.jankhunter.runtime.RuntimeLongSource
import io.jankhunter.runtime.internal.io.Jhlog
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
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
    private val mainThread: FpsMainThreadDispatcher = AndroidFpsMainThreadDispatcher(),
    // Choreographer frame timestamps use System.nanoTime(), which excludes deep sleep on Android.
    private val nanoTime: RuntimeLongSource = RuntimeLongSource(System::nanoTime),
) : Choreographer.FrameCallback {
    private val runState = CollectorRunState()
    private val sourceSelector = FrameSourceSelector(choreographerFallbackEnabled)
    private val windowNanos = millisecondsToNanos(max(250L, windowMs))
    private val jankFrameThresholdMs = max(1L, jankFrameThresholdMs)
    private val window = FrameWindowAccumulator(windowNanos, exactAdmission)

    private var choreographer: Choreographer? = null
    private var callbackPosted = false
    private var lastFallbackFrameNanos = 0L
    @Volatile private var frameGeneration = 0L

    fun start() {
        val expectedGeneration = runState.start() ?: return
        val accepted = runOnMain(onFailure = { runState.stop() }) {
            if (!isCurrent(expectedGeneration)) return@runOnMain
            choreographer = Choreographer.getInstance()
            frameGeneration = expectedGeneration
            resetWindow()
            updateFallbackRegistration()
        }
        if (!accepted) runState.stop()
    }

    fun stop(timeoutMs: Long = DEFAULT_STOP_TIMEOUT_MS): Boolean {
        val stoppedGeneration = frameGeneration
        if (!runState.stop()) return true
        return runOnMain(waitForCompletionMs = timeoutMs.takeIf { exactAdmission }) {
            if (frameGeneration != stoppedGeneration) return@runOnMain
            if (exactAdmission) finishWindow(nanoTime.getAsLong())
            frameGeneration = 0L
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
            if (exactAdmission) finishWindow(nanoTime.getAsLong())
            sourceSelector.updateJankStats(active)
            resetWindow()
            if (activeChanged) {
                updateFallbackRegistration()
            }
        }
    }

    fun setWindowActive(active: Boolean) {
        runOnMain {
            if (!runState.isRunning() || sourceSelector.windowActive == active) return@runOnMain
            if (exactAdmission) finishWindow(nanoTime.getAsLong())
            sourceSelector.updateWindowActive(active)
            resetWindow()
            updateFallbackRegistration()
        }
    }

    fun onJankStatsFrame(screen: String?, durationNanos: Long, isJank: Boolean) {
        if (!runState.isRunning()) return
        val acceptedGeneration = frameGeneration
        if (acceptedGeneration == 0L) return
        runOnMain {
            // Callbacks already ahead of the stop marker must reach its final partial window.
            if (frameGeneration != acceptedGeneration) {
                RuntimeHookFailureTracker.record(RuntimeHookFailureReason.JANKSTATS_FRAME)
                return@runOnMain
            }
            if (!sourceSelector.useJankStats()) return@runOnMain
            recordFrame(
                screen = screen,
                frameTimeNanos = nanoTime.getAsLong(),
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
            millisecondsToMicroseconds(jankFrameThresholdMs),
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

    private inline fun runOnMain(
        waitForCompletionMs: Long? = null,
        crossinline onFailure: () -> Unit = {},
        crossinline block: () -> Unit,
    ): Boolean {
        val onMain = try {
            mainThread.isMainThread()
        } catch (throwable: Throwable) {
            RuntimeHookGuard.rethrowFatal(throwable)
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.COLLECTOR)
            return false
        }
        if (onMain) {
            return runCollectorBlock(block).also { succeeded ->
                if (!succeeded) onFailure()
            }
        }

        val completed = if (waitForCompletionMs != null) CountDownLatch(1) else null
        val taskSucceeded = if (completed != null) AtomicBoolean() else null
        val accepted = try {
            mainThread.post {
                try {
                    val succeeded = runCollectorBlock(block)
                    taskSucceeded?.set(succeeded)
                    if (!succeeded) onFailure()
                } finally {
                    completed?.countDown()
                }
            }
        } catch (throwable: Throwable) {
            RuntimeHookGuard.rethrowFatal(throwable)
            false
        }
        if (!accepted) {
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.COLLECTOR)
            return false
        }
        if (completed == null) return true
        return awaitCompletion(completed, checkNotNull(waitForCompletionMs)) && taskSucceeded?.get() == true
    }

    private inline fun runCollectorBlock(block: () -> Unit): Boolean {
        return RuntimeHookGuard.value(false, RuntimeHookFailureReason.COLLECTOR) {
            block()
            true
        }
    }

    private fun awaitCompletion(completed: CountDownLatch, timeoutMs: Long): Boolean {
        return try {
            completed.await(timeoutMs.coerceAtLeast(0L), TimeUnit.MILLISECONDS)
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
            false
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
        private const val DEFAULT_STOP_TIMEOUT_MS = 5_000L
        private const val NANOS_PER_MS = 1_000_000L
    }
}

internal fun millisecondsToMicroseconds(milliseconds: Long): Long {
    val positive = milliseconds.coerceAtLeast(0L)
    return if (positive > Long.MAX_VALUE / MICROS_PER_MS) Long.MAX_VALUE else positive * MICROS_PER_MS
}

internal interface FpsMainThreadDispatcher {
    fun isMainThread(): Boolean

    fun post(task: Runnable): Boolean
}

private const val MICROS_PER_MS = 1_000L

private class AndroidFpsMainThreadDispatcher : FpsMainThreadDispatcher {
    private val looper = Looper.getMainLooper()
    private val handler = Handler(looper)

    override fun isMainThread(): Boolean = Looper.myLooper() === looper

    override fun post(task: Runnable): Boolean = handler.post(task)
}
