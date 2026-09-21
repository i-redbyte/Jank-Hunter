package io.jankhunter.runtime

import android.os.SystemClock

internal class JankHunterRunnable internal constructor(
    private val delegate: Runnable,
    private val ownerName: String?,
    private val callbacks: RuntimeAsyncCallbacks,
) : Runnable {
    private val capturedContext = callbacks.captureContext(ownerName)

    override fun run() {
        if (!callbacks.isActive()) {
            delegate.run()
            return
        }
        val start = RuntimeHookGuard.value(0L, RuntimeHookFailureReason.ASYNC_WRAPPER) { SystemClock.elapsedRealtime() }
        var failed = false
        try {
            callbacks.callWithContext(capturedContext, ownerName) {
                delegate.run()
            }
        } catch (throwable: Throwable) {
            failed = true
            throw throwable
        } finally {
            RuntimeHookGuard.run(RuntimeHookFailureReason.ASYNC_WRAPPER) {
                callbacks.recordWrappedWork(
                    ownerName,
                    "runnable",
                    elapsedRealtimeSince(start),
                    failed,
                )
            }
        }
    }
}
