package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.RuntimeHookFailureReason
import io.jankhunter.runtime.RuntimeHookFailureTracker

/** Converts process-wide monotonic hook failures to per-session quality deltas without snapshots. */
internal class RuntimeHookFailureQualitySynchronizer(
    private val quality: LogQualityCounters,
) {
    private val reported = LongArray(RuntimeHookFailureReason.entries.size).also { baseline ->
        for (reason in RuntimeHookFailureReason.entries) {
            baseline[reason.ordinal] = RuntimeHookFailureTracker.count(reason)
        }
    }

    fun sync() {
        for (reason in RuntimeHookFailureReason.entries) {
            val index = reason.ordinal
            val previous = reported[index]
            val current = RuntimeHookFailureTracker.count(reason)
            if (current <= previous) continue
            val delta = current - previous
            quality.add(QualityCounterId.RUNTIME_HOOK_FAILURE_TOTAL, delta)
            quality.add(reason.qualityCounterId(), delta)
            reported[index] = current
        }
    }

    private fun RuntimeHookFailureReason.qualityCounterId(): Int = when (this) {
        RuntimeHookFailureReason.INSTRUMENTATION_HOOK -> QualityCounterId.RUNTIME_HOOK_INSTRUMENTATION_FAILURE
        RuntimeHookFailureReason.ASYNC_WRAPPER -> QualityCounterId.RUNTIME_HOOK_ASYNC_WRAPPER_FAILURE
        RuntimeHookFailureReason.RUNTIME_LIFECYCLE -> QualityCounterId.RUNTIME_HOOK_LIFECYCLE_FAILURE
        RuntimeHookFailureReason.COLLECTOR -> QualityCounterId.RUNTIME_HOOK_COLLECTOR_FAILURE
        RuntimeHookFailureReason.CONTEXT -> QualityCounterId.RUNTIME_HOOK_CONTEXT_FAILURE
        RuntimeHookFailureReason.SCHEDULER -> QualityCounterId.RUNTIME_HOOK_SCHEDULER_FAILURE
        RuntimeHookFailureReason.JANKSTATS_DEPENDENCY_MISSING -> QualityCounterId.JANKSTATS_DEPENDENCY_MISSING
        RuntimeHookFailureReason.JANKSTATS_INSTALL -> QualityCounterId.JANKSTATS_INSTALL_FAILURE
        RuntimeHookFailureReason.JANKSTATS_FRAME -> QualityCounterId.JANKSTATS_FRAME_FAILURE
        RuntimeHookFailureReason.JANKSTATS_CONTROL -> QualityCounterId.JANKSTATS_CONTROL_FAILURE
        RuntimeHookFailureReason.UNCLASSIFIED -> QualityCounterId.RUNTIME_HOOK_UNCLASSIFIED_FAILURE
    }
}
