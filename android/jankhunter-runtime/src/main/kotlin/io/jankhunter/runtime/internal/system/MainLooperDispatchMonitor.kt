package io.jankhunter.runtime.internal.system

import android.os.Looper
import android.os.SystemClock
import android.util.Printer
import io.jankhunter.runtime.JankHunter
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.math.max

class MainLooperDispatchMonitor(
    thresholdMs: Long,
    private val getMessageLogging: () -> Printer? = ::readMainLooperPrinter,
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
    @Volatile
    private var previousPrinter: Printer? = null
    private val printer = Printer { line ->
        try {
            previousPrinter?.println(line)
        } catch (_: Throwable) {
        }
        if (!running.get()) return@Printer
        tracker.onMessage(line)?.let { sample ->
            recordDispatch(sample.durationMs, this.thresholdMs, sample.source)
        }
    }

    fun start() {
        if (!running.compareAndSet(false, true)) return
        previousPrinter = safeCurrentPrinter()?.takeUnless { it === printer }
        setMessageLogging(printer)
    }

    fun stop() {
        running.set(false)
        if (safeCurrentPrinter() === printer) {
            setMessageLogging(previousPrinter)
        }
    }

    private fun safeCurrentPrinter(): Printer? {
        return try {
            getMessageLogging()
        } catch (_: Throwable) {
            null
        }
    }
}

private fun readMainLooperPrinter(): Printer? {
    return try {
        val field = Looper::class.java.getDeclaredField("mLogging")
        field.isAccessible = true
        field.get(Looper.getMainLooper()) as? Printer
    } catch (_: Throwable) {
        null
    }
}
