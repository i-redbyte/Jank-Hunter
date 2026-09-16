package io.jankhunter.runtime

import android.app.Activity
import io.jankhunter.runtime.internal.io.AsyncLogWriter
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

    fun bindWatcher(): WatcherSession = WatcherSession(
        access.writer, state.retainedHeapDumper, access.captureContext(),
    )

    /** A late final check owns its original sinks; it cannot resolve a new runtime's resources. */
    inner class WatcherSession(
        private val writer: AsyncLogWriter?,
        private val heapDumper: RetainedHeapDumper?,
        private val fallbackContext: JankHunterContext,
    ) {
        fun record(
            className: String?,
            holder: String?,
            context: JankHunterContext?,
            ageMs: Long,
            count: Long,
            evidence: RetentionEvidence,
        ) {
            val target = writer?.takeIf { it.isAcceptingEvents() } ?: return
            val captured = context ?: fallbackContext
            val retainedHolder = effectiveHolder(className, firstContextValue(holder, captured.owner))
            target.updateProducerContext(captured.screen, retainedHolder, captured.operationId)
            target.retained(
                captured.screen, retainedHolder, className, retainedHolder, ageMs, count,
                foreground = state.writer === target && access.isUiVisible(), evidence = evidence,
            )
        }

        fun dump(className: String?, holder: String?, context: JankHunterContext?, ageMs: Long, count: Long) {
            val captured = context ?: fallbackContext
            val retainedHolder = effectiveHolder(className, firstContextValue(holder, captured.owner))
            writer?.updateProducerContext(captured.screen, retainedHolder, captured.operationId)
            maybeDumpHeap(writer, heapDumper, className, retainedHolder, ageMs, count)
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
        maybeDumpHeap(className, retainedHolder, ageMs, count)
    }

    private fun maybeDumpHeap(className: String?, holder: String?, ageMs: Long, count: Long) {
        maybeDumpHeap(access.writer, state.retainedHeapDumper, className, holder, ageMs, count)
    }

    private fun maybeDumpHeap(
        writer: AsyncLogWriter?,
        heapDumper: RetainedHeapDumper?,
        className: String?,
        holder: String?,
        ageMs: Long,
        count: Long,
    ) {
        if (writer == null || heapDumper == null || !writer.isAcceptingEvents()) return
        if (!state.heapDumpInProgress.compareAndSet(false, true)) {
            writer.counter("jankhunter.heap_dump.skipped.concurrent.count", 1)
            return
        }
        val thresholdMs = access.config?.mainThreadStallThresholdMs() ?: HEAP_DUMP_ATTRIBUTION_MIN_MS
        val result = try {
            heapDumper.maybeDump(className, holder, ageMs, count)
        } finally {
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
