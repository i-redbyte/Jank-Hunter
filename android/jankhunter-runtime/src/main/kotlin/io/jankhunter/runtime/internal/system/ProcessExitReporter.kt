package io.jankhunter.runtime.internal.system

import android.app.ActivityManager
import android.content.Context
import android.os.Build
import io.jankhunter.runtime.JankHunter
import kotlin.math.max

internal object ProcessExitReporter {
    fun report(context: Context) {
        if (Build.VERSION.SDK_INT < 30) return

        try {
            val activityManager = context.getSystemService(Context.ACTIVITY_SERVICE) as? ActivityManager ?: return
            val exits = activityManager.getHistoricalProcessExitReasons(null, 0, 8)
            if (exits.isNullOrEmpty()) return

            val latest = exits.first()
            JankHunter.recordGauge("process.exit.last.reason", latest.reason.toLong())
            JankHunter.recordGauge("process.exit.last.importance", latest.importance.toLong())
            JankHunter.recordGauge("process.exit.last.pss_kb", latest.pss)
            JankHunter.recordGauge("process.exit.last.rss_kb", latest.rss)
            JankHunter.recordGauge(
                "process.exit.last.age_ms",
                max(0L, System.currentTimeMillis() - latest.timestamp),
            )

            val counts = LinkedHashMap<Int, Long>()
            for (exit in exits) {
                counts[exit.reason] = (counts[exit.reason] ?: 0L) + 1L
            }
            for ((reason, count) in counts) {
                JankHunter.recordGauge("process.exit.last.reason_$reason.count", count)
            }
        } catch (_: Exception) {
            JankHunter.recordCounter("process.exit.read_failed.count", 1)
        }
    }
}
