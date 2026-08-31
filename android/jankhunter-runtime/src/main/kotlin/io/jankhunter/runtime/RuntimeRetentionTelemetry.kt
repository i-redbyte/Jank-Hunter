package io.jankhunter.runtime

import android.app.Activity
import io.jankhunter.runtime.internal.system.RetentionEvidence
import io.jankhunter.runtime.internal.system.RetainedHeapDumper
import io.jankhunter.runtime.internal.system.RetainedLifecycleClassifier

internal class RuntimeRetentionTelemetry(
    private val state: RuntimeState,
    private val access: RuntimeTelemetryAccess,
    private val metrics: RuntimeMetricsService,
    private val elapsedRealtimeMs: RuntimeLongSource,
) {
    fun recordRetained(className: String?, holder: String?, ageMs: Long, count: Long) {
        recordRetained(className, holder, ageMs, count, RetentionEvidence.TIME_ONLY)
    }

    fun recordWatchedRetained(
        className: String?,
        holder: String?,
        context: JankHunterContext?,
        ageMs: Long,
        count: Long,
        evidence: RetentionEvidence,
    ) {
        if (context == null) {
            recordRetained(className, holder, ageMs, count, evidence, attemptHeapDump = false)
            return
        }
        val explicitOrContextHolder = firstContextValue(holder, context.owner)
        val retainedHolder = effectiveHolder(className, explicitOrContextHolder)
        access.callWithContext(context, retainedHolder) {
            recordRetained(className, retainedHolder, ageMs, count, evidence, attemptHeapDump = false)
        }
    }

    fun dumpWatchedRetainedHeap(
        className: String?,
        holder: String?,
        context: JankHunterContext?,
        ageMs: Long,
        count: Long,
    ) {
        val retainedHolder = effectiveHolder(className, firstContextValue(holder, context?.owner))
        if (context == null) {
            maybeDumpHeap(className, retainedHolder, ageMs, count)
            return
        }
        access.callWithContext(context, retainedHolder) {
            maybeDumpHeap(className, retainedHolder, ageMs, count)
        }
    }

    fun watchObject(instance: Any?, description: String?, ownerHint: String?) {
        if (instance == null) return
        val watcher = state.objectRetentionWatcher ?: return
        val retainedBy = firstContextValue(ownerHint, access.currentOwnerOrNull())
        val context = access.captureContext(ownerOverride = retainedBy)
        recordCounter("jankhunter.object_watcher.watch.count", 1)
        if (retainedBy != null) {
            recordCounter("owner.${metricOwner(retainedBy)}.object_watcher.watch.count", 1)
        }
        watcher.watch(instance, description, retainedBy, context)
    }

    fun watchActivity(activity: Activity?, ownerHint: String?) {
        watchObject(activity, activity?.javaClass?.name, firstContextValue(ownerHint, activity?.javaClass?.name))
    }

    fun watchLifecycleObject(instance: Any?, lifecycleEvent: String?, ownerHint: String?) {
        RuntimeHookGuard.run {
            val targets = RetainedLifecycleClassifier.targets(instance, lifecycleEvent, ownerHint)
            for (target in targets) {
                watchObject(target.instance, target.description, target.ownerHint)
            }
        }
    }

    fun effectiveHolder(className: String?, holder: String?): String? {
        return firstContextValue(holder, className)
    }

    private fun recordRetained(
        className: String?,
        holder: String?,
        ageMs: Long,
        count: Long,
        evidence: RetentionEvidence,
        attemptHeapDump: Boolean = true,
    ) {
        val retainedHolder = effectiveHolder(className, holder)
        val context = access.captureContext(ownerOverride = retainedHolder)
        access.ensureContextRecorded(screenOverride = context.screen, ownerOverride = context.owner)
        access.writer?.retained(
            context.screen,
            context.owner,
            className,
            retainedHolder,
            ageMs,
            count,
            foreground = access.isUiVisible(),
            evidence = evidence,
        )
        if (attemptHeapDump) maybeDumpHeap(className, retainedHolder, ageMs, count)
    }

    private fun maybeDumpHeap(className: String?, holder: String?, ageMs: Long, count: Long) {
        val writer = access.writer ?: return
        val heapDumper = state.retainedHeapDumper ?: return
        state.heapDumpInProgress.set(true)
        val result = try {
            heapDumper.maybeDump(className, holder, ageMs, count)
        } finally {
            val thresholdMs = access.config?.mainThreadStallThresholdMs() ?: HEAP_DUMP_ATTRIBUTION_MIN_MS
            val graceMs = maxOf(HEAP_DUMP_ATTRIBUTION_MIN_MS, thresholdMs * 2L)
            state.heapDumpAttributionUntilMs.set(elapsedRealtimeMs.getAsLong() + graceMs)
            state.heapDumpInProgress.set(false)
        }
        when (result) {
            is RetainedHeapDumper.Result.Dumped -> {
                writer.counter("jankhunter.heap_dump.created.count", 1)
                writer.gauge("jankhunter.heap_dump.retained_age_ms", result.ageMs)
                writer.counter("jankhunter.heap_dump.retained_objects.count", result.count)
                writer.gauge("jankhunter.heap_dump.file_size_kb", result.file.length() / BYTES_PER_KIBIBYTE)
            }
            is RetainedHeapDumper.Result.Skipped -> {
                writer.counter("jankhunter.heap_dump.skipped.${metricOwner(result.reason)}.count", 1)
            }
            is RetainedHeapDumper.Result.Failed -> {
                writer.counter("jankhunter.heap_dump.failed.${metricOwner(result.reason)}.count", 1)
            }
        }
    }

    private fun recordCounter(name: String, value: Long) {
        RuntimeHookGuard.run { metrics.recordCounter(name, value) }
    }

    private companion object {
        const val HEAP_DUMP_ATTRIBUTION_MIN_MS = 500L
        const val BYTES_PER_KIBIBYTE = 1_024L
    }
}
