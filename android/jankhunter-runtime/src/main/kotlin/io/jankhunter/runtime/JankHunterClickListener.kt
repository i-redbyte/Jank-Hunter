package io.jankhunter.runtime

import android.os.SystemClock
import android.view.View

internal class JankHunterClickListener internal constructor(
    private val delegate: View.OnClickListener,
    private val ownerName: String?,
) : View.OnClickListener {
    private val capturedContext = JankHunter.captureContext(ownerOverride = ownerName)

    override fun onClick(view: View) {
        if (!JankHunter.isRuntimeActiveForCallbacks()) {
            delegate.onClick(view)
            return
        }
        val start = RuntimeHookGuard.value(0L) { SystemClock.elapsedRealtime() }
        JankHunter.callWithContext(capturedContext, ownerName) {
            val currentFlow = RuntimeHookGuard.value("unknown") { JankHunter.currentFlow() }
            val flowToken = if (currentFlow == "unknown") {
                RuntimeHookGuard.value<JankHunterFlow?>(null) {
                    JankHunter.startFlow("click.${ownerName ?: "unknown"}")
                }
            } else {
                null
            }
            var failed = false
            try {
                RuntimeHookGuard.run { JankHunter.markFlowStep("click") }
                delegate.onClick(view)
            } catch (throwable: Throwable) {
                failed = true
                throw throwable
            } finally {
                RuntimeHookGuard.run {
                    val durationMs = if (start > 0L) {
                        (SystemClock.elapsedRealtime() - start).coerceAtLeast(0L)
                    } else {
                        0L
                    }
                    JankHunter.recordClick(ownerName, durationMs, failed)
                }
                RuntimeHookGuard.run { JankHunter.endFlow(flowToken) }
            }
        }
    }
}
