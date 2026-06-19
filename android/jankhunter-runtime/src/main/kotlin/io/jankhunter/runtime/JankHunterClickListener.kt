package io.jankhunter.runtime

import android.os.SystemClock
import android.view.View

internal class JankHunterClickListener internal constructor(
    private val delegate: View.OnClickListener,
    private val ownerName: String?,
) : View.OnClickListener {
    private val capturedContext = JankHunter.captureContext(ownerOverride = ownerName)

    override fun onClick(view: View) {
        val start = SystemClock.elapsedRealtime()
        JankHunter.callWithContext(capturedContext, ownerName) {
            val flowToken = if (JankHunter.currentFlow() == "unknown") {
                JankHunter.startFlow("click.${ownerName ?: "unknown"}")
            } else {
                null
            }
            var failed = false
            try {
                JankHunter.markFlowStep("click")
                delegate.onClick(view)
            } catch (throwable: Throwable) {
                failed = true
                throw throwable
            } finally {
                JankHunter.recordClick(ownerName, SystemClock.elapsedRealtime() - start, failed)
                JankHunter.endFlow(flowToken)
            }
        }
    }
}
