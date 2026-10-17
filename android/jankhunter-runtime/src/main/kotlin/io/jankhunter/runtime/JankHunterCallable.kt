package io.jankhunter.runtime

import android.os.SystemClock
import java.util.concurrent.Callable

internal class JankHunterCallable<T> internal constructor(
    private val delegate: Callable<T>,
    private val ownerName: String?,
    private val callbacks: RuntimeAsyncCallbacks,
) : Callable<T> {
    private val capturedContext = callbacks.captureContext(ownerName)
    @Volatile private var causalBinding: RuntimeCausalExecutionBinding? = null
    @Volatile private var causalAmbiguous = false

    internal fun bindCausal(binding: RuntimeCausalExecutionBinding): Boolean {
        synchronized(this) {
            if (causalBinding == null && !causalAmbiguous) {
                causalBinding = binding
                return true
            } else {
                causalBinding = null
                causalAmbiguous = true
                return false
            }
        }
    }

    override fun call(): T {
        if (!callbacks.isActive()) return delegate.call()
        val start = RuntimeHookGuard.value(0L, RuntimeHookFailureReason.ASYNC_WRAPPER) { SystemClock.elapsedRealtime() }
        val binding = causalBinding
        val causalToken = RuntimeHookGuard.value<Any?>(null, RuntimeHookFailureReason.ASYNC_WRAPPER) { binding?.enter() }
        var failed = false
        try {
            return callbacks.callWithContext(capturedContext, ownerName) {
                delegate.call()
            }
        } catch (throwable: Throwable) {
            failed = true
            throw throwable
        } finally {
            RuntimeHookGuard.run(RuntimeHookFailureReason.ASYNC_WRAPPER) { binding?.exit(causalToken, failed) }
            if (binding != null && !binding.recurring) {
                synchronized(this) { if (causalBinding === binding) causalBinding = null }
            }
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
