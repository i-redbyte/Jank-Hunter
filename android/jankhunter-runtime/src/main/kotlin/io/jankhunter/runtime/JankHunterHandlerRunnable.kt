package io.jankhunter.runtime

import android.os.SystemClock

internal class JankHunterHandlerRunnable internal constructor(
    private val delegate: Runnable,
    private val ownerName: String?,
) : Runnable {
    private val capturedContext = JankHunter.captureContext(ownerOverride = ownerName)

    override fun run() {
        val start = SystemClock.elapsedRealtime()
        var failed = false
        try {
            JankHunter.callWithContext(capturedContext, ownerName) {
                delegate.run()
            }
        } catch (throwable: Throwable) {
            failed = true
            throw throwable
        } finally {
            JankHunter.unregisterHandlerRunnable(delegate, this)
            JankHunter.recordWrappedWork(
                ownerName,
                "handler_runnable",
                SystemClock.elapsedRealtime() - start,
                failed,
            )
        }
    }
}
