package io.jankhunter.runtime.integration

import android.view.Window
import java.lang.reflect.InvocationTargetException
import io.jankhunter.runtime.JankHunterFrameListenerV1
import io.jankhunter.runtime.JankHunterFrameTrackingV1
import io.jankhunter.runtime.JankHunterJankStatsAdapterV1
import io.jankhunter.runtime.RuntimeHookFailureReason
import io.jankhunter.runtime.RuntimeHookFailureTracker
import io.jankhunter.runtime.RuntimeHookGuard

/** Checks the optional dependency before loading the typed AndroidX integration. */
internal object JankHunterJankStats {
    private val adapter by lazy(LazyThreadSafetyMode.PUBLICATION, ::loadAdapter)
    internal fun available(): Boolean = adapter != null

    fun install(window: Window?, onFrame: FrameListener): Handle? {
        if (window == null) return null
        val available = adapter ?: return null
        return try { Handle(available.create(window, onFrame)) }
        catch (failure: Throwable) {
            RuntimeHookGuard.rethrowFatal(failure)
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.JANKSTATS_INSTALL)
            null
        }
    }

    internal fun createFrameListener(onFrame: FrameListener): Any? = adapter?.createFrameCallback(onFrame)

    internal class Handle(control: JankHunterFrameTrackingV1) {
        private var control: JankHunterFrameTrackingV1? = control

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

    internal interface TrackingControl : JankHunterFrameTrackingV1

    internal fun interface FrameListener : JankHunterFrameListenerV1 {
        override fun onFailure() { RuntimeHookFailureTracker.record(RuntimeHookFailureReason.JANKSTATS_FRAME) }
    }

    private fun loadAdapter(): JankHunterJankStatsAdapterV1? {
        return try {
            Class.forName("androidx.metrics.performance.JankStats")
            Class.forName("io.jankhunter.runtime.integration.AndroidXJankStatsAdapter")
                .getDeclaredConstructor().newInstance() as JankHunterJankStatsAdapterV1
        } catch (_: ClassNotFoundException) { null }
        catch (_: NoClassDefFoundError) { null }
        catch (failure: Throwable) {
            val cause = (failure as? InvocationTargetException)?.targetException ?: failure
            RuntimeHookGuard.rethrowFatal(cause)
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.JANKSTATS_INSTALL)
            null
        }
    }
}
