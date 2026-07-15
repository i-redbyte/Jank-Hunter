package io.jankhunter.runtime

import android.os.SystemClock
import java.util.concurrent.Callable

internal class JankHunterCallable<T> internal constructor(
    private val delegate: Callable<T>,
    private val ownerName: String?,
) : Callable<T> {
    private val capturedContext = JankHunter.captureContext(ownerOverride = ownerName)

    override fun call(): T {
        if (!JankHunter.isRuntimeActiveForCallbacks()) return delegate.call()
        val start = RuntimeHookGuard.value(0L) { SystemClock.elapsedRealtime() }
        var failed = false
        try {
            return JankHunter.callWithContext(capturedContext, ownerName) {
                delegate.call()
            }
        } catch (throwable: Throwable) {
            failed = true
            throw throwable
        } finally {
            RuntimeHookGuard.run {
                JankHunter.recordWrappedWork(
                    ownerName,
                    "callable",
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
