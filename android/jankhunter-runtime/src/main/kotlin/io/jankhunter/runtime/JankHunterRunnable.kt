package io.jankhunter.runtime

import android.os.SystemClock

internal class JankHunterRunnable internal constructor(
    private val delegate: Runnable,
    private val ownerName: String?,
) : Runnable {
    private val capturedContext = JankHunter.captureContext(ownerOverride = ownerName)

    override fun run() {
        if (!JankHunter.isRuntimeActiveForCallbacks()) {
            delegate.run()
            return
        }
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
                JankHunter.recordWrappedWork(
                    ownerName,
                    "runnable",
                    elapsedSince(start),
                    failed,
                )
            }
        }
    }

    private fun elapsedSince(startMs: Long): Long {
        if (startMs <= 0L) return 0L
        return (SystemClock.elapsedRealtime() - startMs).coerceAtLeast(0L)
    }
}
