package io.jankhunter.runtime

import android.os.SystemClock

internal class JankHunterRunnable internal constructor(
    private val delegate: Runnable,
    private val ownerName: String?,
    private val callbacks: RuntimeAsyncCallbacks,
) : Runnable {
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

    override fun run() {
        if (!callbacks.isActive()) {
            delegate.run()
            return
        }
        val start = RuntimeHookGuard.value(0L, RuntimeHookFailureReason.ASYNC_WRAPPER) { SystemClock.elapsedRealtime() }
        val binding = causalBinding
        val causalToken = RuntimeHookGuard.value<Any?>(null, RuntimeHookFailureReason.ASYNC_WRAPPER) { binding?.enter() }
        var failed = false
        try {
            callbacks.callWithContext(capturedContext, ownerName) {
                delegate.run()
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
                    "runnable",
                    elapsedRealtimeSince(start),
                    failed,
                )
            }
        }
    }
}
