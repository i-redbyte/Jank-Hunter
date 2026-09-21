package io.jankhunter.runtime.internal.system

import android.app.ActivityManager
import android.content.Context
import android.os.Build
import io.jankhunter.runtime.RuntimeCollectorCallbacks

internal class ProcessExitReporter(
    private val callbacks: RuntimeCollectorCallbacks,
) {
    fun report(context: Context) {
        if (Build.VERSION.SDK_INT < 30) return

        try {
            val activityManager = context.getSystemService(Context.ACTIVITY_SERVICE) as? ActivityManager ?: return
            val exits = activityManager.getHistoricalProcessExitReasons(null, 0, 8)
            if (exits.isNullOrEmpty()) return

            for (exit in exits) {
                callbacks.recordProcessExit(
                    reason = exit.reason.toLong(),
                    timestampUnixMs = exit.timestamp,
                    importance = exit.importance.toLong(),
                    pssKb = exit.pss,
                    rssKb = exit.rss,
                    processName = exit.processName,
                )
            }
        } catch (_: Exception) {
            callbacks.recordCounter("process.exit.read_failed.count", 1)
        }
    }
}
