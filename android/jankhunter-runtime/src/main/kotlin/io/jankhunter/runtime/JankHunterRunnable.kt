package io.jankhunter.runtime

import android.os.SystemClock

class JankHunterRunnable internal constructor(
    private val delegate: Runnable,
    private val ownerName: String?,
) : Runnable {
    override fun run() {
        val start = SystemClock.elapsedRealtime()
        var failed = false
        try {
            JankHunter.callWithOwner(ownerName) {
                delegate.run()
            }
        } catch (throwable: Throwable) {
            failed = true
            throw throwable
        } finally {
            JankHunter.recordWrappedWork(
                ownerName,
                "runnable",
                SystemClock.elapsedRealtime() - start,
                failed,
            )
        }
    }
}
