package io.jankhunter.runtime.integration

import android.view.Window
import io.jankhunter.runtime.RuntimeHookFailureReason
import io.jankhunter.runtime.RuntimeHookFailureTracker
import io.jankhunter.runtime.RuntimeHookGuard

/** Checks the optional dependency before loading the typed AndroidX integration. */
internal object JankHunterJankStats {
    private val dependencyAvailable by lazy(LazyThreadSafetyMode.PUBLICATION, ::isDependencyAvailable)

    fun install(window: Window?, onFrame: FrameListener): Handle? {
        if (window == null || !dependencyAvailable) return null
        var callback: JankStatsFrameCallback? = null
        return try {
            val frameCallback = JankStatsFrameCallback(onFrame)
            callback = frameCallback
            Handle(JankStatsTrackingControl(window, frameCallback))
        } catch (throwable: Throwable) {
            callback?.close()
            RuntimeHookGuard.rethrowFatal(throwable)
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.JANKSTATS_INSTALL)
            null
        }
    }

    internal fun createFrameListener(onFrame: FrameListener): Any? {
        if (!dependencyAvailable) return null
        return JankStatsFrameCallback(onFrame)
    }

    internal class Handle(control: TrackingControl) {
        private var control: TrackingControl? = control

        @Synchronized
        fun setTrackingEnabled(enabled: Boolean) {
            val active = control ?: return
            RuntimeHookGuard.run(RuntimeHookFailureReason.JANKSTATS_CONTROL) { active.setTrackingEnabled(enabled) }
        }

        @Synchronized
        fun uninstall() {
            val active = control ?: return
            control = null
            try {
                RuntimeHookGuard.run(RuntimeHookFailureReason.JANKSTATS_CONTROL) { active.setTrackingEnabled(false) }
            } finally {
                active.close()
            }
        }
    }

    internal interface TrackingControl : AutoCloseable {
        fun setTrackingEnabled(enabled: Boolean)
    }

    internal fun interface FrameListener {
        fun onFrame(isJank: Boolean, durationNanos: Long)
    }

    private fun isDependencyAvailable(): Boolean {
        return try {
            Class.forName("androidx.metrics.performance.JankStats")
            true
        } catch (_: ClassNotFoundException) {
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.JANKSTATS_DEPENDENCY_MISSING)
            false
        } catch (_: NoClassDefFoundError) {
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.JANKSTATS_DEPENDENCY_MISSING)
            false
        } catch (throwable: Throwable) {
            RuntimeHookGuard.rethrowFatal(throwable)
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.JANKSTATS_INSTALL)
            false
        }
    }
}
