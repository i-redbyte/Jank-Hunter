package io.jankhunter.runtime

import android.os.SystemClock

internal fun interface HandlerRunnableOwner {
    fun unregister(delegate: Runnable, wrapper: Runnable)
}

internal class JankHunterHandlerRunnable internal constructor(
    private val delegate: Runnable,
    private val ownerName: String?,
    private val callbacks: RuntimeAsyncCallbacks,
    private val owner: HandlerRunnableOwner,
) : Runnable {
    private val capturedContext = callbacks.captureContext(ownerName)

    override fun run() {
        try {
            if (!callbacks.isActive()) {
                delegate.run()
                return
            }
            runWithTelemetry()
        } finally {
            owner.unregister(delegate, this)
        }
    }

    private fun runWithTelemetry() {
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
                val durationMs = if (start > 0L) {
                    (SystemClock.elapsedRealtime() - start).coerceAtLeast(0L)
                } else {
                    0L
                }
                callbacks.recordWrappedWork(ownerName, "handler_runnable", durationMs, failed)
            }
        }
    }
}
