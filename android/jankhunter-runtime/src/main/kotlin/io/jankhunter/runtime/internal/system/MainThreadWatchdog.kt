package io.jankhunter.runtime.internal.system

import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterContextSnapshot
import java.util.concurrent.atomic.AtomicLong
import kotlin.math.max

internal class MainThreadWatchdog(
    thresholdMs: Long,
) {
    private val thresholdMs = max(MIN_THRESHOLD_MS, thresholdMs)
    private val pollIntervalMs = max(MIN_POLL_INTERVAL_MS, this.thresholdMs / 2L)
    private val mainLooper = Looper.getMainLooper()
    private val mainHandler = Handler(mainLooper)
    private val runState = CollectorRunState()
    private val lastBeatMs = AtomicLong()
    private val activeStallBeatMs = AtomicLong(NO_ACTIVE_STALL)

    @Volatile
    private var thread: Thread? = null

    fun start() {
        val expectedGeneration = runState.start() ?: return
        activeStallBeatMs.set(NO_ACTIVE_STALL)
        lastBeatMs.set(SystemClock.elapsedRealtime())
        postBeat(expectedGeneration)
        thread = Thread({ monitorMainThread(expectedGeneration) }, "JankHunterMainWatchdog").apply {
            isDaemon = true
            priority = Thread.MIN_PRIORITY
            start()
        }
    }

    fun stop() {
        if (!runState.stop()) return
        val current = thread
        thread = null
        current?.interrupt()
    }

    private fun postBeat(expectedGeneration: Long) {
        mainHandler.post(
            object : Runnable {
                override fun run() {
                    if (!isCurrent(expectedGeneration)) return
                    val now = SystemClock.elapsedRealtime()
                    val stalledSince = activeStallBeatMs.get()
                    if (stalledSince == NO_ACTIVE_STALL) {
                        lastBeatMs.set(now)
                    } else {
                        // Preserve the first recovery heartbeat until the watchdog consumes it.
                        lastBeatMs.compareAndSet(stalledSince, now)
                    }
                    mainHandler.postDelayed(this, pollIntervalMs)
                }
            },
        )
    }

    private fun monitorMainThread(expectedGeneration: Long) {
        val episodeTracker = StallEpisodeTracker(thresholdMs)
        var pendingStall: StallCapture? = null
        while (isCurrent(expectedGeneration)) {
            val now = SystemClock.elapsedRealtime()
            val observedBeatMs = lastBeatMs.get()
            when (episodeTracker.update(observedBeatMs, now)) {
                StallEpisodeChange.STARTED -> {
                    activeStallBeatMs.compareAndSet(NO_ACTIVE_STALL, observedBeatMs)
                    pendingStall = captureStall()
                }
                StallEpisodeChange.RECOVERED -> {
                    val captured = pendingStall
                    pendingStall = null
                    activeStallBeatMs.set(NO_ACTIVE_STALL)
                    if (captured != null && isCurrent(expectedGeneration)) {
                        JankHunter.recordMainThreadStall(
                            captured.context,
                            captured.stackHint,
                            episodeTracker.completedDurationMs,
                        )
                    }
                }
                StallEpisodeChange.NONE -> Unit
            }
            try {
                Thread.sleep(pollIntervalMs)
            } catch (_: InterruptedException) {
                return
            }
        }
    }

    private fun captureStall(): StallCapture {
        val stack = mainLooper.thread.stackTrace
        val frame = stack.firstOrNull(::isApplicationFrame) ?: stack.firstOrNull()
        val owner = frame?.className
        val stackHint = frame?.let(::stackHint) ?: "unknown"
        return StallCapture(
            context = JankHunter.captureMainThreadStallContext(owner),
            stackHint = stackHint,
        )
    }

    private fun isCurrent(expectedGeneration: Long): Boolean {
        return runState.isCurrent(expectedGeneration)
    }

    private fun isApplicationFrame(frame: StackTraceElement): Boolean {
        val className = frame.className
        return INFRASTRUCTURE_PREFIXES.none(className::startsWith)
    }

    private fun stackHint(frame: StackTraceElement): String {
        val location = when {
            frame.isNativeMethod -> "Native Method"
            frame.fileName != null && frame.lineNumber >= 0 -> "${frame.fileName}:${frame.lineNumber}"
            frame.fileName != null -> frame.fileName
            else -> "Unknown Source"
        }
        return "${frame.className}.${frame.methodName}($location)"
    }

    private data class StallCapture(
        val context: JankHunterContextSnapshot,
        val stackHint: String,
    )

    private companion object {
        private const val MIN_THRESHOLD_MS = 100L
        private const val MIN_POLL_INTERVAL_MS = 50L
        private const val NO_ACTIVE_STALL = Long.MIN_VALUE

        private val INFRASTRUCTURE_PREFIXES = arrayOf(
            "android.",
            "androidx.",
            "com.android.",
            "dalvik.",
            "io.jankhunter.",
            "java.",
            "jdk.",
            "kotlin.",
            "kotlinx.",
            "sun.",
        )
    }
}
