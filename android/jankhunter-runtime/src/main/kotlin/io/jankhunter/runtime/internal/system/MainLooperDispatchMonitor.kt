package io.jankhunter.runtime.internal.system

import android.os.Looper
import android.os.SystemClock
import android.util.Printer
import io.jankhunter.runtime.JankHunter
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.math.max

class MainLooperDispatchMonitor(
    thresholdMs: Long,
    private val setMessageLogging: (Printer?) -> Unit = { printer ->
        Looper.getMainLooper().setMessageLogging(printer)
    },
    clockMs: () -> Long = { SystemClock.elapsedRealtime() },
    private val recordDispatch: (Long, Long, String?) -> Unit = { durationMs, thresholdMs, source ->
        JankHunter.recordMainThreadDispatch(durationMs, thresholdMs, source)
    },
) {
    private val running = AtomicBoolean(false)
    private val thresholdMs = max(1L, thresholdMs)
    private val tracker = MainThreadDispatchTracker(
        clockMs = clockMs,
        minDurationMs = this.thresholdMs,
    )
    private val printer = Printer { line ->
        if (!running.get()) return@Printer
        tracker.onMessage(line)?.let { sample ->
            recordDispatch(sample.durationMs, this.thresholdMs, sample.source)
        }
    }

    fun start() {
        if (!running.compareAndSet(false, true)) return
        setMessageLogging(printer)
    }

    fun stop() {
        running.set(false)
        // Looper has no public previous-Printer getter; clearing here can remove another profiler's logger.
    }
}
