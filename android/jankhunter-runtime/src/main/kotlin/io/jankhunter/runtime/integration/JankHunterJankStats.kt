package io.jankhunter.runtime.integration

import android.view.Window
import io.jankhunter.runtime.JankHunter
import java.lang.reflect.Proxy

object JankHunterJankStats {
    @JvmStatic
    fun install(window: Window?): Any? {
        if (window == null) return null

        return try {
            val jankStatsClass = Class.forName("androidx.metrics.performance.JankStats")
            val listenerClass = Class.forName("androidx.metrics.performance.JankStats\$OnFrameListener")
            val proxy = Proxy.newProxyInstance(
                listenerClass.classLoader,
                arrayOf(listenerClass),
            ) { _, method, args ->
                if (method.name == "onFrame" && args?.isNotEmpty() == true) {
                    recordFrameData(args[0])
                }
                null
            }

            jankStatsClass
                .getMethod("createAndTrack", Window::class.java, listenerClass)
                .invoke(null, window, proxy)
        } catch (_: Throwable) {
            null
        }
    }

    private fun recordFrameData(frameData: Any) {
        val type = frameData.javaClass
        val isJank = (type.getMethod("isJank").invoke(frameData) as? Boolean) == true
        val durationNanos = (type.getMethod("getFrameDurationUiNanos").invoke(frameData) as? Long) ?: 0L
        JankHunter.recordCounter("jankstats.frame.count", 1)
        if (isJank) {
            JankHunter.recordCounter("jankstats.frame.jank.count", 1)
        }
        if (durationNanos > 0) {
            JankHunter.recordGauge("jankstats.frame.duration_ms", durationNanos / 1_000_000L)
        }
    }
}
