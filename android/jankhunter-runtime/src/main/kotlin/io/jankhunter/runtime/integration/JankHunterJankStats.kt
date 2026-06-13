package io.jankhunter.runtime.integration

import android.view.Window
import io.jankhunter.runtime.JankHunter
import java.lang.reflect.Proxy
import java.util.concurrent.CopyOnWriteArrayList

object JankHunterJankStats {
    private val handles = CopyOnWriteArrayList<Handle>()

    @JvmStatic
    fun install(window: Window?): Handle? = install(window, JankHunter.currentScreen())

    @JvmStatic
    fun install(window: Window?, screenName: String?): Handle? {
        if (window == null) return null

        return try {
            val jankStatsClass = Class.forName("androidx.metrics.performance.JankStats")
            val listenerClass = Class.forName("androidx.metrics.performance.JankStats\$OnFrameListener")
            val proxy = Proxy.newProxyInstance(
                listenerClass.classLoader,
                arrayOf(listenerClass),
            ) { _, method, args ->
                if (method.name == "onFrame" && args?.isNotEmpty() == true) {
                    args[0]?.let { recordFrameData(it, screenName) }
                }
                null
            }

            val instance: Any = jankStatsClass
                .getMethod("createAndTrack", Window::class.java, listenerClass)
                .invoke(null, window, proxy) ?: return null
            Handle(instance).also {
                handles += it
                JankHunter.recordCounter("jankstats.install.count", 1)
            }
        } catch (_: Throwable) {
            null
        }
    }

    @JvmStatic
    fun uninstallAll() {
        for (handle in handles) {
            handle.uninstall()
        }
        handles.clear()
    }

    class Handle internal constructor(
        private val instance: Any,
    ) {
        @Volatile
        private var installed = true

        fun addState(key: String?, value: String?) {
            invokeState("addState", key, value)
        }

        fun removeState(key: String?) {
            invokeState("removeState", key, null)
        }

        fun uninstall() {
            if (!installed) return
            installed = false
            try {
                instance.javaClass
                    .getMethod("setTrackingEnabled", Boolean::class.javaPrimitiveType)
                    .invoke(instance, false)
            } catch (_: Throwable) {
            }
            handles.remove(this)
            JankHunter.recordCounter("jankstats.uninstall.count", 1)
        }

        private fun invokeState(methodName: String, key: String?, value: String?) {
            val safeKey = key?.takeIf { it.isNotBlank() } ?: return
            try {
                if (value == null) {
                    instance.javaClass.getMethod(methodName, String::class.java).invoke(instance, safeKey)
                } else {
                    instance.javaClass
                        .getMethod(methodName, String::class.java, String::class.java)
                        .invoke(instance, safeKey, value)
                }
            } catch (_: Throwable) {
            }
        }
    }

    private fun recordFrameData(frameData: Any, screenName: String?) {
        val type = frameData.javaClass
        val isJank = (type.tryInvoke(frameData, "isJank") as? Boolean) == true
        val durationNanos = (type.tryInvoke(frameData, "getFrameDurationUiNanos") as? Long) ?: 0L
        val screen = metricPart(screenName ?: JankHunter.currentScreen())

        JankHunter.recordCounter("jankstats.frame.count", 1)
        JankHunter.recordCounter("jankstats.screen.$screen.frame.count", 1)
        if (isJank) {
            JankHunter.recordCounter("jankstats.frame.jank.count", 1)
            JankHunter.recordCounter("jankstats.screen.$screen.jank.count", 1)
        }
        if (durationNanos > 0) {
            val durationMs = durationNanos / 1_000_000L
            JankHunter.recordGauge("jankstats.frame.duration_ms", durationMs)
            JankHunter.recordGauge("jankstats.screen.$screen.duration_ms", durationMs)
        }
        recordStates(frameData, isJank)
    }

    private fun recordStates(frameData: Any, isJank: Boolean) {
        val states = frameData.javaClass.tryInvoke(frameData, "getStates") as? Iterable<*> ?: return
        for (state in states) {
            if (state == null) continue
            val type = state.javaClass
            val key = metricPart(type.tryInvoke(state, "getKey") as? String)
            val value = metricPart(type.tryInvoke(state, "getValue") as? String)
            if (key == "unknown" && value == "unknown") continue
            JankHunter.recordCounter("jankstats.state.$key.$value.frame.count", 1)
            if (isJank) {
                JankHunter.recordCounter("jankstats.state.$key.$value.jank.count", 1)
            }
        }
    }

    private fun Class<*>.tryInvoke(target: Any, methodName: String): Any? {
        return try {
            getMethod(methodName).invoke(target)
        } catch (_: Throwable) {
            null
        }
    }

    private fun metricPart(value: String?): String {
        return value
            ?.takeIf { it.isNotBlank() }
            ?.replace(Regex("[^A-Za-z0-9._-]"), "_")
            ?.trim('_', '.', '-')
            ?.take(80)
            ?.takeIf { it.isNotBlank() }
            ?: "unknown"
    }
}
