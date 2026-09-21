package io.jankhunter.runtime.internal.system

import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import io.jankhunter.runtime.JankHunterContextSnapshot
import io.jankhunter.runtime.MainThreadStallCallbacks
import io.jankhunter.runtime.MainThreadStallState
import io.jankhunter.runtime.RuntimeHookFailureTracker
import io.jankhunter.runtime.RuntimeHookFailureReason
import io.jankhunter.runtime.RuntimeHookGuard
import java.util.concurrent.atomic.AtomicLong
import kotlin.math.max

internal class MainThreadWatchdog(
    thresholdMs: Long,
    private val callbacks: MainThreadStallCallbacks,
    private val mainThread: WatchdogMainThread = AndroidWatchdogMainThread(),
    private val maxStackBytes: Int = 1024,
    private val watchdogThreadFactory: (Runnable, String) -> Thread = ::Thread,
) {
    private val thresholdMs = max(MIN_THRESHOLD_MS, thresholdMs)
    private val pollIntervalMs = max(MIN_POLL_INTERVAL_MS, this.thresholdMs / 2L)
    private val runState = CollectorRunState()
    private val lifecycleLock = Any()

    @Volatile
    private var thread: Thread? = null

    fun start() {
        synchronized(lifecycleLock) {
            val expectedGeneration = runState.start() ?: return
            val initialBeatMs = try {
                mainThread.uptimeMillis()
            } catch (throwable: Throwable) {
                runState.stop()
                RuntimeHookGuard.rethrowFatal(throwable)
                RuntimeHookFailureTracker.record(RuntimeHookFailureReason.COLLECTOR)
                return
            }
            val run = WatchdogRun(expectedGeneration, initialBeatMs)
            val heartbeat = Heartbeat(run)
            this.heartbeat = heartbeat
            if (!postHeartbeat(heartbeat)) {
                this.heartbeat = null
                runState.stop()
                return
            }
            try {
                val monitor = watchdogThreadFactory(
                    Runnable {
                        try {
                            monitorMainThread(run)
                        } finally {
                            finishMonitorGeneration(expectedGeneration)
                        }
                    },
                    "JankHunterMainWatchdog",
                ).apply {
                    isDaemon = true
                    priority = Thread.MIN_PRIORITY
                }
                thread = monitor
                monitor.start()
            } catch (throwable: Throwable) {
                thread = null
                runState.stop()
                removeHeartbeatLocked()
                throw throwable
            }
        }
    }

    fun stop(timeoutMs: Long = 0L): Boolean {
        val current = synchronized(lifecycleLock) {
            if (!runState.stop()) return thread?.isAlive != true
            removeHeartbeatLocked()
            thread.also { thread = null }
        }
        current?.interrupt()
        if (current == null || current === Thread.currentThread()) return true
        if (timeoutMs > 0L) {
            try {
                current.join(timeoutMs)
            } catch (_: InterruptedException) {
                Thread.currentThread().interrupt()
                return false
            }
        }
        return !current.isAlive
    }

    private fun finishMonitorGeneration(expectedGeneration: Long) {
        synchronized(lifecycleLock) {
            if (thread !== Thread.currentThread() || !isCurrent(expectedGeneration)) return
            runState.stop()
            removeHeartbeatLocked()
            thread = null
        }
    }

    private var heartbeat: Heartbeat? = null

    private fun postHeartbeat(task: Runnable): Boolean {
        return postHeartbeatSafely { mainThread.post(task) }
    }

    private fun postDelayedHeartbeat(task: Runnable): Boolean {
        return postHeartbeatSafely { mainThread.postDelayed(task, pollIntervalMs) }
    }

    private inline fun postHeartbeatSafely(post: () -> Boolean): Boolean {
        val accepted = try {
            post()
        } catch (throwable: Throwable) {
            RuntimeHookGuard.rethrowFatal(throwable)
            false
        }
        if (!accepted) RuntimeHookFailureTracker.record(RuntimeHookFailureReason.COLLECTOR)
        return accepted
    }

    private fun removeHeartbeatLocked() {
        val activeHeartbeat = heartbeat ?: return
        heartbeat = null
        RuntimeHookGuard.run(RuntimeHookFailureReason.COLLECTOR) {
            mainThread.removeCallbacks(activeHeartbeat)
        }
    }

    private inner class Heartbeat(
        private val run: WatchdogRun,
    ) : Runnable {
        private val expectedGeneration: Long get() = run.generation
        override fun run() {
            if (!isCurrent(expectedGeneration)) return
            val completed = RuntimeHookGuard.value(false, RuntimeHookFailureReason.COLLECTOR) {
                val now = mainThread.uptimeMillis()
                val stalledSince = run.activeStallBeatMs.get()
                if (stalledSince == NO_ACTIVE_STALL) {
                    run.lastBeatMs.set(now)
                } else {
                    // Preserve the first recovery heartbeat until the watchdog consumes it.
                    run.lastBeatMs.compareAndSet(stalledSince, now)
                }
                true
            }
            if (!completed) {
                stop()
                return
            }
            val accepted = synchronized(lifecycleLock) {
                if (!isCurrent(expectedGeneration)) return
                postDelayedHeartbeat(this)
            }
            if (!accepted) stop()
        }
    }

    private fun monitorMainThread(run: WatchdogRun) {
        val episodeTracker = StallEpisodeTracker(thresholdMs)
        var pendingStall: StallCapture? = null
        var lastObservationMs = run.lastBeatMs.get()
        try {
            while (isCurrent(run.generation)) {
                RuntimeHookGuard.run {
                    val now = mainThread.uptimeMillis()
                    lastObservationMs = now
                    val observedBeatMs = run.lastBeatMs.get()
                    when (episodeTracker.update(observedBeatMs, now)) {
                        StallEpisodeChange.STARTED -> {
                            run.activeStallBeatMs.compareAndSet(NO_ACTIVE_STALL, observedBeatMs)
                            val captured = captureStall(observedBeatMs)
                            pendingStall = captured
                            recordStall(captured, now - observedBeatMs, MainThreadStallState.ONGOING)
                        }
                        StallEpisodeChange.RECOVERED -> {
                            val captured = pendingStall
                            pendingStall = null
                            run.activeStallBeatMs.set(NO_ACTIVE_STALL)
                            if (captured != null) {
                                recordStall(captured, episodeTracker.completedDurationMs, MainThreadStallState.RECOVERED)
                            }
                        }
                        StallEpisodeChange.NONE -> pendingStall?.let(::sampleActiveStall)
                    }
                }
                try {
                    Thread.sleep(pollIntervalMs)
                } catch (_: InterruptedException) {
                    return
                }
            }
        } finally {
            // The monitor owns this episode through termination, including stop/restart races.
            pendingStall?.let { captured ->
                RuntimeHookGuard.run(RuntimeHookFailureReason.COLLECTOR) {
                    val now = RuntimeHookGuard.value(lastObservationMs, RuntimeHookFailureReason.COLLECTOR) {
                        mainThread.uptimeMillis()
                    }
                    recordStall(captured, now - captured.startedAtMs, MainThreadStallState.INTERRUPTED)
                }
            }
        }
    }

    private fun recordStall(stall: StallCapture, durationMs: Long, state: MainThreadStallState) {
        callbacks.recordMainThreadStall(
            stall.context,
            stall.evidence.stackHint,
            durationMs.coerceAtLeast(0L),
            stall.incidentId,
            state,
        )
    }

    private fun captureStall(startedAtMs: Long): StallCapture {
        val evidence = MainThreadStallEvidence(MAX_STALL_STACK_SAMPLES, maxStackBytes)
        evidence.addSample(mainThread.stackTrace())
        return StallCapture(
            incidentId = callbacks.nextMainThreadStallId(),
            startedAtMs = startedAtMs,
            context = callbacks.captureMainThreadStallContext(null),
            evidence = evidence,
        )
    }

    private fun sampleActiveStall(stall: StallCapture) {
        if (stall.evidence.canSample) {
            stall.evidence.addSample(mainThread.stackTrace())
        }
    }

    private fun isCurrent(expectedGeneration: Long): Boolean {
        return runState.isCurrent(expectedGeneration)
    }

    private class WatchdogRun(val generation: Long, initialBeatMs: Long) {
        val lastBeatMs = AtomicLong(initialBeatMs)
        val activeStallBeatMs = AtomicLong(NO_ACTIVE_STALL)
    }

    private data class StallCapture(
        val incidentId: Long,
        val startedAtMs: Long,
        val context: JankHunterContextSnapshot,
        val evidence: MainThreadStallEvidence,
    )

    private companion object {
        private const val MIN_THRESHOLD_MS = 100L
        private const val MIN_POLL_INTERVAL_MS = 50L
        private const val NO_ACTIVE_STALL = Long.MIN_VALUE
        private const val MAX_STALL_STACK_SAMPLES = 8
    }
}

internal interface WatchdogMainThread {
    // Handler delays use uptime: suspended device time is not main-thread execution delay.
    fun uptimeMillis(): Long

    fun post(task: Runnable): Boolean

    fun postDelayed(task: Runnable, delayMs: Long): Boolean

    fun removeCallbacks(task: Runnable)

    fun stackTrace(): Array<StackTraceElement>
}

private class AndroidWatchdogMainThread : WatchdogMainThread {
    private val looper = Looper.getMainLooper()
    private val handler = Handler(looper)

    override fun uptimeMillis(): Long = SystemClock.uptimeMillis()

    override fun post(task: Runnable): Boolean = handler.post(task)

    override fun postDelayed(task: Runnable, delayMs: Long): Boolean = handler.postDelayed(task, delayMs)

    override fun removeCallbacks(task: Runnable) = handler.removeCallbacks(task)

    override fun stackTrace(): Array<StackTraceElement> = looper.thread.stackTrace
}

/**
 * Keeps a bounded vote over stack samples collected only while the main thread is already stalled.
 * Samples are grouped by the observed top frame and retain the bounded caller chain.
 * Observed frames never become owners: obfuscation erases package-based provenance.
 */
internal class MainThreadStallEvidence(
    maxSamples: Int,
    maxStackBytes: Int = 4096,
) {
    private val maxSamples = maxSamples.also {
        require(it in 1..8) { "maxSamples must be between 1 and 8" }
    }
    private val maxStackBytes = maxStackBytes.coerceIn(1, MAX_STACK_CHARS)
    private val classNames = arrayOfNulls<String>(maxSamples)
    private val methodNames = arrayOfNulls<String>(maxSamples)
    private val stackHints = arrayOfNulls<String>(maxSamples)
    private val votes = IntArray(maxSamples)
    private var distinctFrames = 0
    private var strongestFrame = -1

    var sampleCount: Int = 0
        private set

    var stackHint: String = UNKNOWN_STACK
        private set

    val canSample: Boolean
        get() = sampleCount < maxSamples

    fun addSample(stack: Array<StackTraceElement>): Boolean {
        if (!canSample) return false
        sampleCount++
        val frame = stack.firstOrNull() ?: return true
        val index = frameIndex(frame, stack)
        votes[index]++
        if (strongestFrame < 0 || votes[index] > votes[strongestFrame]) {
            strongestFrame = index
            stackHint = stackHints[index] ?: UNKNOWN_STACK
        }
        return true
    }

    private fun frameIndex(frame: StackTraceElement, stack: Array<StackTraceElement>): Int {
        for (index in 0 until distinctFrames) {
            if (classNames[index] == frame.className && methodNames[index] == frame.methodName) {
                return index
            }
        }
        val index = distinctFrames
        distinctFrames++
        classNames[index] = frame.className.take(MAX_FRAME_CHARS)
        methodNames[index] = frame.methodName.take(MAX_FRAME_CHARS)
        stackHints[index] = formatBoundedStack(stack)
        return index
    }

    private fun formatBoundedStack(stack: Array<StackTraceElement>): String {
        val marker = if (maxStackBytes >= TRUNCATED.length) TRUNCATED else "!"
        val output = StringBuilder(maxStackBytes)
        var written = 0
        var bytes = 0
        for (index in 0 until minOf(stack.size, MAX_STACK_FRAMES)) {
            val frame = stack[index]
            val location = when {
                frame.isNativeMethod -> "Native Method"
                frame.fileName != null -> frame.fileName
                else -> "Unknown Source"
            }
            // Check before interpolation: hostile/oversized class metadata cannot grow this payload.
            val chars = frame.className.length.toLong() + frame.methodName.length + location.length + 24L
            if (chars > MAX_FRAME_CHARS) break
            val lineBytes = if (!frame.isNativeMethod && frame.fileName != null && frame.lineNumber >= 0) {
                1 + decimalLength(frame.lineNumber)
            } else 0
            val frameBytes = utf8Size(frame.className) + utf8Size(frame.methodName) + utf8Size(location) +
                7 + lineBytes + if (written > 0) 1 else 0
            if (bytes + frameBytes > maxStackBytes - marker.length) break
            bytes += frameBytes
            if (written > 0) output.append('\n')
            output.append("\tat ").append(frame.className).append('.').append(frame.methodName).append('(')
            output.append(location)
            if (!frame.isNativeMethod && frame.fileName != null && frame.lineNumber >= 0) {
                output.append(':').append(frame.lineNumber)
            }
            output.append(')')
            written++
        }
        if (written < stack.size) output.append(marker)
        return output.toString()
    }

    // Surrogate code units are conservatively charged three bytes each (a pair needs four).
    private fun utf8Size(value: String): Int {
        var bytes = 0
        for (character in value) bytes += when {
            character.code < 0x80 -> 1
            character.code < 0x800 -> 2
            else -> 3
        }
        return bytes
    }

    private fun decimalLength(value: Int): Int {
        var remaining = value
        var digits = 1
        while (remaining >= 10) {
            remaining /= 10
            digits++
        }
        return digits
    }

    private companion object {
        private const val UNKNOWN_STACK = "unknown"
        private const val MAX_STACK_FRAMES = 32
        private const val MAX_STACK_CHARS = 4096
        private const val MAX_FRAME_CHARS = 512
        private const val TRUNCATED = "\n[Jank Hunter: stack truncated]"
    }
}
