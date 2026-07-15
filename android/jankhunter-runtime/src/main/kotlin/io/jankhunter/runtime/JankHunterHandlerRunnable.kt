package io.jankhunter.runtime

import android.os.SystemClock

internal class JankHunterHandlerRunnable internal constructor(
    private val delegate: Runnable,
    private val ownerName: String?,
) : Runnable {
    private val capturedContext = JankHunter.captureContext(ownerOverride = ownerName)

    override fun run() {
        try {
            if (!JankHunter.isRuntimeActiveForCallbacks()) {
                delegate.run()
                return
            }
            runWithTelemetry()
        } finally {
            JankHunter.unregisterHandlerRunnable(delegate, this)
        }
    }

    private fun runWithTelemetry() {
        val start = RuntimeHookGuard.value(0L) { SystemClock.elapsedRealtime() }
        var failed = false
        try {
            JankHunter.callWithContext(capturedContext, ownerName) {
                delegate.run()
            }
        } catch (throwable: Throwable) {
            failed = true
            throw throwable
        } finally {
            RuntimeHookGuard.run {
                val durationMs = if (start > 0L) {
                    (SystemClock.elapsedRealtime() - start).coerceAtLeast(0L)
                } else {
                    0L
                }
                JankHunter.recordWrappedWork(ownerName, "handler_runnable", durationMs, failed)
            }
        }
    }
}
