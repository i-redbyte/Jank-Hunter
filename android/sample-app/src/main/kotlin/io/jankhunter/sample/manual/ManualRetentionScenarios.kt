package io.jankhunter.sample.manual

import io.jankhunter.runtime.JankHunterTelemetry

import io.jankhunter.sample.LeakCanaryBridge
import io.jankhunter.sample.R
import io.jankhunter.sample.ReleasedCheckoutProbe
import io.jankhunter.sample.RetainedCheckoutCache
import io.jankhunter.sample.RetainedCheckoutScreen
import io.jankhunter.sample.SampleApplication
import io.jankhunter.runtime.JankHunter

internal class ManualRetentionScenarios(
    private val application: SampleApplication,
    private val text: SampleText,
    private val stateSink: ManualStateSink,
) {
    val handlers: Map<ManualAction, ManualActionHandler> = mapOf(
        ManualAction.CLEAN_OBJECT to { recordCleanObject() },
        ManualAction.ACTIVITY_REFERENCE to { recordActivityReference(requireNotNull(it)) },
        ManualAction.VIEW_BINDING to { recordViewBinding(requireNotNull(it)) },
        ManualAction.LISTENER_CALLBACK to { recordListenerCallback() },
        ManualAction.CACHE_ENTRIES to { recordCacheEntries() },
        ManualAction.CLEAR_RETAINED to { clearRetainedObjects() },
        ManualAction.CANDIDATE_LEAK_BURST to { recordLeakRegressionBurst(requireNotNull(it)) },
    )

    fun recordCleanObject() {
        JankHunterTelemetry.traceOperation("sample.memory_leak.clean_object") {
            val probe = ReleasedCheckoutProbe()
            JankHunterTelemetry.watch(
                probe,
                ReleasedCheckoutProbe::class.java.name,
                "sample.memory_leak.cleaned_scope",
            )
            LeakCanaryBridge.watch(probe, text(R.string.leakcanary_desc_clean_object))
        }
        JankHunterTelemetry.counter("sample.memory_leak.clean.watch.count", 1)
        status(text(R.string.status_clean_object_watched))
    }

    fun recordActivityReference(activityReference: Any) {
        recordRetained(
            step = "activity_reference",
            displayName = text(R.string.leak_display_activity_reference),
            owner = "sample.memory_leak.activity_registry",
            sample = RetainedCheckoutScreen(activityReference, ByteArray(128 * 1024)),
        )
    }

    fun recordViewBinding(activityReference: Any) {
        recordRetained(
            step = "view_binding",
            displayName = text(R.string.leak_display_view_binding),
            owner = "sample.memory_leak.binding_cache",
            sample = RetainedCheckoutScreen(activityReference, ByteArray(256 * 1024)),
        )
    }

    fun recordListenerCallback() {
        recordRetained(
            step = "listener_callback",
            displayName = text(R.string.leak_display_listener_callback),
            owner = "sample.memory_leak.listener_registry",
            sample = RetainedManualListener { status(text(R.string.status_listener_retained)) },
        )
    }

    fun recordLeakCanaryObject(activityReference: Any) {
        recordRetained(
            step = "leakcanary_retained_object",
            displayName = text(R.string.leak_display_leakcanary_retained),
            owner = "sample.memory_leak.leakcanary_benchmark",
            sample = RetainedCheckoutScreen(activityReference, ByteArray(128 * 1024)),
        )
    }

    fun recordCacheEntries() {
        JankHunterTelemetry.traceOperation("sample.memory_leak.cache_entries") {
            repeat(3) { index ->
                val entry = RetainedCheckoutCache(index, ByteArray(128 * 1024))
                application.retainedObjects += entry
                JankHunterTelemetry.watch(entry, entry.javaClass.name, "sample.memory_leak.checkout_cache")
                LeakCanaryBridge.watch(entry, text(R.string.leakcanary_desc_cache_entry, index))
            }
        }
        JankHunterTelemetry.counter("sample.memory_leak.cache.watch.count", 3)
        JankHunter.flush()
        status(text(R.string.status_cache_entries_watched))
    }

    fun clearRetainedObjects() {
        application.retainedObjects.clear()
        JankHunterTelemetry.counter("sample.memory_leak.retained_list.clear.count", 1)
        status(text(R.string.status_retained_list_cleared))
    }

    fun recordLeakRegressionBurst(activityReference: Any) {
        JankHunterTelemetry.traceOperation("sample.memory_leak.compare_candidate") {
            repeat(2) { index ->
                JankHunterTelemetry.traceOperation("sample.memory_leak.activity_and_binding_burst_$index") {
                    val activity = RetainedCheckoutScreen(activityReference, ByteArray(160 * 1024))
                    val cache = RetainedCheckoutCache(index, ByteArray(160 * 1024))
                    application.retainedObjects += activity
                    application.retainedObjects += cache
                    JankHunterTelemetry.watch(activity, activity.javaClass.name, "sample.memory_leak.regression_burst")
                    JankHunterTelemetry.watch(cache, cache.javaClass.name, "sample.memory_leak.regression_burst")
                    LeakCanaryBridge.watch(activity, text(R.string.leakcanary_desc_regression_activity, index))
                    LeakCanaryBridge.watch(cache, text(R.string.leakcanary_desc_regression_binding, index))
                }
            }
        }
        JankHunterTelemetry.counter("sample.memory_leak.regression_burst.watch.count", 4)
        JankHunter.flush()
        status(text(R.string.status_candidate_leak_burst_watched))
    }

    private fun recordRetained(step: String, displayName: String, owner: String, sample: Any) {
        JankHunterTelemetry.traceOperation("sample.memory_leak.$step") {
            JankHunterTelemetry.withOwner(owner) {
                application.retainedObjects += sample
                JankHunterTelemetry.watch(sample, sample.javaClass.name, owner)
                LeakCanaryBridge.watch(
                    sample,
                    text(R.string.leakcanary_desc_retained_by_owner, displayName, owner),
                )
            }
        }
        JankHunterTelemetry.counter("sample.memory_leak.watch.count", 1)
        JankHunter.flush()
        status(text(R.string.status_leak_scenario_recorded, displayName))
    }

    private fun status(value: String) {
        stateSink.emit(ManualStateUpdate.Status(value))
    }

    private class RetainedManualListener(val action: () -> Unit)
}
