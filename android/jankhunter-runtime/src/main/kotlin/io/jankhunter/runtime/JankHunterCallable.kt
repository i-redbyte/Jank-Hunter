package io.jankhunter.runtime

import android.os.SystemClock
import java.util.concurrent.Callable

class JankHunterCallable<T> internal constructor(
    private val delegate: Callable<T>,
    private val ownerName: String?,
) : Callable<T> {
    override fun call(): T {
        val start = SystemClock.elapsedRealtime()
        var failed = false
        try {
            return JankHunter.callWithOwner(ownerName) {
                delegate.call()
            }
        } catch (throwable: Throwable) {
            failed = true
            throw throwable
        } finally {
            JankHunter.recordWrappedWork(
                ownerName,
                "callable",
                SystemClock.elapsedRealtime() - start,
                failed,
            )
        }
    }
}
