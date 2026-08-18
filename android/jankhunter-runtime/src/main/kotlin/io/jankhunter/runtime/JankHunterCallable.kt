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
        val start = RuntimeHookGuard.value(0L, RuntimeHookFailureReason.ASYNC_WRAPPER) { SystemClock.elapsedRealtime() }
        var failed = false
        try {
            return JankHunter.callWithContext(capturedContext, ownerName) {
                delegate.call()
            }
        } catch (throwable: Throwable) {
            failed = true
            throw throwable
        } finally {
            RuntimeHookGuard.run(RuntimeHookFailureReason.ASYNC_WRAPPER) {
                JankHunter.recordWrappedWork(
                    ownerName,
                    "callable",
                    elapsedRealtimeSince(start),
                    failed,
                )
            }
        }
    }
}
