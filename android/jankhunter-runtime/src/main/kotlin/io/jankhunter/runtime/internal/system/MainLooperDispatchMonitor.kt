package io.jankhunter.runtime.internal.system

import android.os.Looper
import android.os.SystemClock
import android.util.Printer
import io.jankhunter.runtime.RuntimeHookFailureTracker
import io.jankhunter.runtime.RuntimeHookFailureReason
import io.jankhunter.runtime.RuntimeHookGuard
import io.jankhunter.runtime.RuntimeLongSource
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.math.max

internal class MainLooperDispatchMonitor(
    thresholdMs: Long,
    private val recordDispatch: DispatchSampleRecorder,
    private val getMessageLogging: () -> Printer? = ::readMainLooperPrinter,
    private val setMessageLogging: (Printer?) -> Unit = { printer ->
        Looper.getMainLooper().setMessageLogging(printer)
    },
    clockMs: RuntimeLongSource = RuntimeLongSource { SystemClock.elapsedRealtime() },
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
        RuntimeHookGuard.run {
            tracker.onMessage(line)?.let { sample ->
                recordDispatch.record(sample.durationMs, this.thresholdMs, sample.source)
            }
        }
    }

    fun start() {
        if (!running.compareAndSet(false, true)) return
        previousPrinter = safeCurrentPrinter()?.takeUnless { it === printer }
        try {
            setMessageLogging(printer)
        } catch (throwable: Throwable) {
            running.set(false)
            previousPrinter = null
            throwable.recordOrRethrow()
        }
    }

    fun stop() {
        if (!running.getAndSet(false)) return
        if (safeCurrentPrinter() === printer) {
            RuntimeHookGuard.run { setMessageLogging(previousPrinter) }
        }
        // If a later profiler replaced the global printer, replacing it here would break that profiler.
        // Leave its chain in place; this wrapper is inactive and only forwards to the printer it captured.
    }

    private fun safeCurrentPrinter(): Printer? {
        return try {
            getMessageLogging()
        } catch (_: Throwable) {
            null
        }
    }
}

private fun Throwable.recordOrRethrow() {
    if (this is VirtualMachineError || this is ThreadDeath) throw this
    RuntimeHookFailureTracker.record(RuntimeHookFailureReason.COLLECTOR)
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
