package io.jankhunter.runtime.internal.system

import android.os.Looper
import android.os.SystemClock
import android.util.Printer
import io.jankhunter.runtime.JankHunter
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.math.max

class MainLooperDispatchMonitor(
    thresholdMs: Long,
) {
    private val running = AtomicBoolean(false)
    private val thresholdMs = max(1L, thresholdMs)
    private val tracker = MainThreadDispatchTracker { SystemClock.elapsedRealtime() }
    private val printer = Printer { line ->
        tracker.onMessage(line)?.let { sample ->
            JankHunter.recordMainThreadDispatch(sample.durationMs, thresholdMs, sample.source)
        }
    }

    fun start() {
        if (!running.compareAndSet(false, true)) return
        Looper.getMainLooper().setMessageLogging(printer)
    }

    fun stop() {
        running.set(false)
        Looper.getMainLooper().setMessageLogging(null)
    }
}
