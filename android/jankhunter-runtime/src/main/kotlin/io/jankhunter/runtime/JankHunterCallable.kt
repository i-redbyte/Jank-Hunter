package io.jankhunter.runtime

import android.os.SystemClock
import java.util.concurrent.Callable

internal class JankHunterCallable<T> internal constructor(
    private val delegate: Callable<T>,
    private val ownerName: String?,
    private val callbacks: RuntimeAsyncCallbacks,
) : Callable<T> {
    private val capturedContext = callbacks.captureContext(ownerName)

    override fun call(): T {
        if (!callbacks.isActive()) return delegate.call()
        val start = RuntimeHookGuard.value(0L, RuntimeHookFailureReason.ASYNC_WRAPPER) { SystemClock.elapsedRealtime() }
        var failed = false
        try {
            return callbacks.callWithContext(capturedContext, ownerName) {
                delegate.call()
            }
        } catch (throwable: Throwable) {
            failed = true
            throw throwable
        } finally {
            RuntimeHookGuard.run(RuntimeHookFailureReason.ASYNC_WRAPPER) {
                callbacks.recordWrappedWork(
                    ownerName,
                    "callable",
                    elapsedRealtimeSince(start),
                    failed,
                )
            }
        }
    }
}
