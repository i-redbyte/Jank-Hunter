package io.jankhunter.runtime

import android.os.SystemClock
import android.view.View

internal class JankHunterClickListener internal constructor(
    private val delegate: View.OnClickListener,
    private val ownerName: String?,
    private val callbacks: RuntimeAsyncCallbacks,
) : View.OnClickListener {
    private val capturedContext = callbacks.captureContext(ownerName)

    override fun onClick(view: View) {
        if (!callbacks.isActive()) {
            delegate.onClick(view)
            return
        }
        val start = RuntimeHookGuard.value(0L, RuntimeHookFailureReason.ASYNC_WRAPPER) { SystemClock.elapsedRealtime() }
        callbacks.callWithContext(capturedContext, ownerName) {
            val operation = RuntimeHookGuard.value<JankHunterOperation?>(null, RuntimeHookFailureReason.ASYNC_WRAPPER) {
                callbacks.startOperation(
                    name = "click.${ownerName ?: "unknown"}",
                    kind = JankHunterOperationKind.USER,
                )
            }
            var failed = false
            try {
                delegate.onClick(view)
            } catch (throwable: Throwable) {
                failed = true
                RuntimeHookGuard.run(RuntimeHookFailureReason.ASYNC_WRAPPER) { operation?.failure() }
                throw throwable
            } finally {
                if (!failed) {
                    RuntimeHookGuard.run(RuntimeHookFailureReason.ASYNC_WRAPPER) { operation?.success() }
                }
                RuntimeHookGuard.run(RuntimeHookFailureReason.ASYNC_WRAPPER) {
                    val durationMs = if (start > 0L) {
                        (SystemClock.elapsedRealtime() - start).coerceAtLeast(0L)
                    } else {
                        0L
                    }
                    callbacks.recordClick(ownerName, durationMs, failed)
                }
            }
        }
    }
}
