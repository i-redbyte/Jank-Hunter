package io.jankhunter.runtime

import android.content.Context
import android.content.pm.ApplicationInfo
import android.os.Bundle
import java.io.File
import java.util.Locale

enum class JankHunterLogBucket {
    SESSION,
    DAILY,
    ;

    companion object {
        @JvmStatic
        fun from(value: String?): JankHunterLogBucket? {
            return when (value?.trim()?.lowercase(Locale.US)) {
                "session" -> SESSION
                "daily" -> DAILY
                else -> null
            }
        }
    }
}

class JankHunterConfig private constructor(builder: Builder) {
    private val enabled = builder.enabled
    private val runtimeEnabled = builder.runtimeEnabled
    private val autoStartCollectors = builder.autoStartCollectors
    private val mainThreadStallThresholdMs = builder.mainThreadStallThresholdMs
    private val ownerBlockThresholdMs = builder.ownerBlockThresholdMs
    private val httpSlowThresholdMs = builder.httpSlowThresholdMs
    private val memorySampleIntervalMs = builder.memorySampleIntervalMs
    private val systemSamplerEnabled = builder.systemSamplerEnabled
    private val systemSampleIntervalMs = builder.systemSampleIntervalMs
    private val mainLooperDispatchMonitorEnabled = builder.mainLooperDispatchMonitorEnabled
    private val processExitInfoEnabled = builder.processExitInfoEnabled
    private val objectWatcherEnabled = builder.objectWatcherEnabled
    private val retainedObjectDelayMs = builder.retainedObjectDelayMs
    private val retainedObjectForceGcEnabled = builder.retainedObjectForceGcEnabled
    private val retainedHeapDumpEnabled = builder.retainedHeapDumpEnabled
    private val retainedHeapDumpPrivacyApproved = builder.retainedHeapDumpPrivacyApproved
    private val retainedHeapDumpMinIntervalMs = builder.retainedHeapDumpMinIntervalMs
    private val retainedHeapDumpMaxCount = builder.retainedHeapDumpMaxCount
    private val retainedHeapDumpMinRetainedAgeMs = builder.retainedHeapDumpMinRetainedAgeMs
    private val retainedHeapDumpDirectory = builder.retainedHeapDumpDirectory
    private val fpsMonitorEnabled = builder.fpsMonitorEnabled
    private val jankStatsEnabled = builder.jankStatsEnabled
    private val fpsWindowMs = builder.fpsWindowMs
    private val jankFrameThresholdMs = builder.jankFrameThresholdMs
    private val uiWindowP95ThresholdMs = builder.uiWindowP95ThresholdMs
    private val maxQueueSize = builder.maxQueueSize
    private val maxLogBytes = builder.maxLogBytes
    private val maxLogDirectoryBytes = builder.maxLogDirectoryBytes
    private val logBucket = builder.logBucket
    private val maxDictionaryEntries = builder.maxDictionaryEntries
    private val maxDictionaryValueBytes = builder.maxDictionaryValueBytes
    private val flushIntervalMs = builder.flushIntervalMs
    private val adaptiveSamplingEnabled = builder.adaptiveSamplingEnabled
    private val adaptiveMemoryStableIntervalMs = builder.adaptiveMemoryStableIntervalMs
    private val adaptiveContextStableIntervalMs = builder.adaptiveContextStableIntervalMs
    private val metricAggregationEnabled = builder.metricAggregationEnabled
    private val metricAggregationWindowMs = builder.metricAggregationWindowMs
    private val maxMetricAggregationKeys = builder.maxMetricAggregationKeys
    private val maxLogSpamKeys = builder.maxLogSpamKeys
    private val maxRuntimeCallGraphKeys = builder.maxRuntimeCallGraphKeys
    private val maxHandlerTrackingEntries = builder.maxHandlerTrackingEntries
    private val maxHandlerWrappersPerRunnable = builder.maxHandlerWrappersPerRunnable
    private val routeRedactor = builder.routeRedactor
    private val logDirectory = builder.logDirectory
    private val mainProcessOnly = builder.mainProcessOnly
    private val allowedProcesses = builder.allowedProcesses.toSet()
    private val processNameRedactor = builder.processNameRedactor
    private val deviceInfoEnabled = builder.deviceInfoEnabled
    private val binaryStorage = builder.binaryStorage

    fun enabled(): Boolean = enabled

    fun runtimeEnabled(): Boolean = runtimeEnabled

    fun autoStartCollectors(): Boolean = autoStartCollectors

    fun mainThreadStallThresholdMs(): Long = mainThreadStallThresholdMs.coerceAtLeast(1L)

    fun ownerBlockThresholdMs(): Long = ownerBlockThresholdMs.coerceAtLeast(1L)

    fun httpSlowThresholdMs(): Long = httpSlowThresholdMs.coerceAtLeast(1L)

    fun memorySampleIntervalMs(): Long = memorySampleIntervalMs.coerceAtLeast(1L)

    fun systemSamplerEnabled(): Boolean = systemSamplerEnabled

    fun systemSampleIntervalMs(): Long = systemSampleIntervalMs.coerceAtLeast(1L)

    fun mainLooperDispatchMonitorEnabled(): Boolean = mainLooperDispatchMonitorEnabled

    fun processExitInfoEnabled(): Boolean = processExitInfoEnabled

    fun objectWatcherEnabled(): Boolean = objectWatcherEnabled

    fun retainedObjectDelayMs(): Long = retainedObjectDelayMs.coerceAtLeast(0L)

    fun retainedObjectForceGcEnabled(): Boolean = retainedObjectForceGcEnabled

    fun retainedHeapDumpEnabled(): Boolean = retainedHeapDumpEnabled && retainedHeapDumpPrivacyApproved

    fun retainedHeapDumpPrivacyApproved(): Boolean = retainedHeapDumpPrivacyApproved

    fun retainedHeapDumpMinIntervalMs(): Long = retainedHeapDumpMinIntervalMs.coerceAtLeast(0L)

    fun retainedHeapDumpMaxCount(): Int = retainedHeapDumpMaxCount.coerceAtLeast(0)

    fun retainedHeapDumpMinRetainedAgeMs(): Long = retainedHeapDumpMinRetainedAgeMs.coerceAtLeast(0L)

    fun retainedHeapDumpDirectory(): File? = retainedHeapDumpDirectory

    fun fpsMonitorEnabled(): Boolean = fpsMonitorEnabled

    fun jankStatsEnabled(): Boolean = jankStatsEnabled

    fun fpsWindowMs(): Long = fpsWindowMs.coerceAtLeast(1L)

    fun jankFrameThresholdMs(): Long = jankFrameThresholdMs.coerceAtLeast(1L)

    fun uiWindowP95ThresholdMs(): Long = uiWindowP95ThresholdMs.coerceAtLeast(1L)

    fun maxQueueSize(): Int = maxQueueSize.coerceAtLeast(1)

    fun maxLogBytes(): Long = maxLogBytes.coerceAtLeast(1L)

    fun maxLogDirectoryBytes(): Long = maxLogDirectoryBytes.coerceAtLeast(1L)

    fun logBucket(): JankHunterLogBucket = logBucket

    fun maxDictionaryEntries(): Int = maxDictionaryEntries.coerceAtLeast(0)

    fun maxDictionaryValueBytes(): Int = maxDictionaryValueBytes.coerceAtLeast(1)

    fun flushIntervalMs(): Long = flushIntervalMs.coerceAtLeast(1L)

    fun adaptiveSamplingEnabled(): Boolean = adaptiveSamplingEnabled

    fun adaptiveMemoryStableIntervalMs(): Long = adaptiveMemoryStableIntervalMs.coerceAtLeast(0L)

    fun adaptiveContextStableIntervalMs(): Long = adaptiveContextStableIntervalMs.coerceAtLeast(0L)

    fun metricAggregationEnabled(): Boolean = metricAggregationEnabled

    fun metricAggregationWindowMs(): Long = metricAggregationWindowMs.coerceAtLeast(1L)

    fun maxMetricAggregationKeys(): Int = maxMetricAggregationKeys.coerceAtLeast(0)

    fun maxLogSpamKeys(): Int = maxLogSpamKeys.coerceAtLeast(0)

    fun maxRuntimeCallGraphKeys(): Int = maxRuntimeCallGraphKeys.coerceAtLeast(0)

    fun maxHandlerTrackingEntries(): Int = maxHandlerTrackingEntries.coerceAtLeast(0)

    fun maxHandlerWrappersPerRunnable(): Int = maxHandlerWrappersPerRunnable.coerceAtLeast(0)

    fun redactRoute(route: String?): String? = routeRedactor.redact(route)

    fun logDirectory(): File? = logDirectory

    fun mainProcessOnly(): Boolean = mainProcessOnly

    fun allowedProcesses(): Set<String> = allowedProcesses

    fun redactProcessName(processName: String?): String? = processNameRedactor.redact(processName)

    fun deviceInfoEnabled(): Boolean = deviceInfoEnabled

    fun binaryStorage(): JankHunterBinaryStorage? = binaryStorage

    fun toBuilder(): Builder {
        return Builder()
            .enabled(enabled)
            .runtimeEnabled(runtimeEnabled)
            .autoStartCollectors(autoStartCollectors)
            .mainThreadStallThresholdMs(mainThreadStallThresholdMs)
            .ownerBlockThresholdMs(ownerBlockThresholdMs)
            .httpSlowThresholdMs(httpSlowThresholdMs)
            .memorySampleIntervalMs(memorySampleIntervalMs)
            .systemSamplerEnabled(systemSamplerEnabled)
            .systemSampleIntervalMs(systemSampleIntervalMs)
            .mainLooperDispatchMonitorEnabled(mainLooperDispatchMonitorEnabled)
            .processExitInfoEnabled(processExitInfoEnabled)
            .objectWatcherEnabled(objectWatcherEnabled)
            .retainedObjectDelayMs(retainedObjectDelayMs)
            .retainedObjectForceGcEnabled(retainedObjectForceGcEnabled)
            .retainedHeapDumpEnabled(retainedHeapDumpEnabled)
            .retainedHeapDumpPrivacyApproved(retainedHeapDumpPrivacyApproved)
            .retainedHeapDumpMinIntervalMs(retainedHeapDumpMinIntervalMs)
            .retainedHeapDumpMaxCount(retainedHeapDumpMaxCount)
            .retainedHeapDumpMinRetainedAgeMs(retainedHeapDumpMinRetainedAgeMs)
            .retainedHeapDumpDirectory(retainedHeapDumpDirectory)
            .fpsMonitorEnabled(fpsMonitorEnabled)
            .jankStatsEnabled(jankStatsEnabled)
            .fpsWindowMs(fpsWindowMs)
            .jankFrameThresholdMs(jankFrameThresholdMs)
            .uiWindowP95ThresholdMs(uiWindowP95ThresholdMs)
            .maxQueueSize(maxQueueSize)
            .maxLogBytes(maxLogBytes)
            .maxLogDirectoryBytes(maxLogDirectoryBytes)
            .logBucket(logBucket)
            .maxDictionaryEntries(maxDictionaryEntries)
            .maxDictionaryValueBytes(maxDictionaryValueBytes)
            .flushIntervalMs(flushIntervalMs)
            .adaptiveSamplingEnabled(adaptiveSamplingEnabled)
            .adaptiveMemoryStableIntervalMs(adaptiveMemoryStableIntervalMs)
            .adaptiveContextStableIntervalMs(adaptiveContextStableIntervalMs)
            .metricAggregationEnabled(metricAggregationEnabled)
            .metricAggregationWindowMs(metricAggregationWindowMs)
            .maxMetricAggregationKeys(maxMetricAggregationKeys)
            .maxLogSpamKeys(maxLogSpamKeys)
            .maxRuntimeCallGraphKeys(maxRuntimeCallGraphKeys)
            .maxHandlerTrackingEntries(maxHandlerTrackingEntries)
            .maxHandlerWrappersPerRunnable(maxHandlerWrappersPerRunnable)
            .routeRedactor(routeRedactor)
            .logDirectory(logDirectory)
            .mainProcessOnly(mainProcessOnly)
            .allowedProcesses(allowedProcesses)
            .processNameRedactor(processNameRedactor)
            .deviceInfoEnabled(deviceInfoEnabled)
            .binaryStorage(binaryStorage)
    }

    fun isProcessAllowed(processName: String, mainProcessName: String): Boolean {
        if (allowedProcesses.isNotEmpty()) return processName in allowedProcesses
        if (mainProcessOnly && processName != mainProcessName) return false
        return true
    }

    class Builder {
        internal var enabled = true
        internal var runtimeEnabled = true
        internal var autoStartCollectors = true
        internal var mainThreadStallThresholdMs = 700L
        internal var ownerBlockThresholdMs = 250L
        internal var httpSlowThresholdMs = 1_000L
        internal var memorySampleIntervalMs = 10_000L
        internal var systemSamplerEnabled = true
        internal var systemSampleIntervalMs = 15_000L
        internal var mainLooperDispatchMonitorEnabled = true
        internal var processExitInfoEnabled = true
        internal var objectWatcherEnabled = true
        internal var retainedObjectDelayMs = 5_000L
        internal var retainedObjectForceGcEnabled = false
        internal var retainedHeapDumpEnabled = true
        internal var retainedHeapDumpPrivacyApproved = true
        internal var retainedHeapDumpMinIntervalMs = 10 * 60_000L
        internal var retainedHeapDumpMaxCount = 1
        internal var retainedHeapDumpMinRetainedAgeMs = 30_000L
        internal var retainedHeapDumpDirectory: File? = null
        internal var fpsMonitorEnabled = true
        internal var jankStatsEnabled = true
        internal var fpsWindowMs = 1_000L
        internal var jankFrameThresholdMs = 32L
        internal var uiWindowP95ThresholdMs = 32L
        internal var maxQueueSize = 2048
        internal var maxLogBytes = 5L * 1024L * 1024L
        internal var maxLogDirectoryBytes = 25L * 1024L * 1024L
        internal var logBucket = JankHunterLogBucket.SESSION
        internal var maxDictionaryEntries = 8192
        internal var maxDictionaryValueBytes = 256
        internal var flushIntervalMs = 5_000L
        internal var adaptiveSamplingEnabled = true
        internal var adaptiveMemoryStableIntervalMs = 60_000L
        internal var adaptiveContextStableIntervalMs = 60_000L
        internal var metricAggregationEnabled = true
        internal var metricAggregationWindowMs = 5_000L
        internal var maxMetricAggregationKeys = 2048
        internal var maxLogSpamKeys = 2048
        internal var maxRuntimeCallGraphKeys = 4096
        internal var maxHandlerTrackingEntries = 4096
        internal var maxHandlerWrappersPerRunnable = 32
        internal var routeRedactor: JankHunterRedactor = JankHunterRedactor.default()
        internal var logDirectory: File? = null
        internal var mainProcessOnly = true
        internal var allowedProcesses: List<String> = emptyList()
        internal var processNameRedactor: JankHunterProcessNameRedactor = JankHunterProcessNameRedactor.none()
        internal var deviceInfoEnabled = true
        internal var binaryStorage: JankHunterBinaryStorage? = null

        fun enabled(value: Boolean) = apply { enabled = value }

        fun runtimeEnabled(value: Boolean) = apply { runtimeEnabled = value }

        fun autoStartCollectors(value: Boolean) = apply { autoStartCollectors = value }

        fun mainThreadStallThresholdMs(value: Long) = apply { mainThreadStallThresholdMs = value }

        fun ownerBlockThresholdMs(value: Long) = apply { ownerBlockThresholdMs = value }

        fun httpSlowThresholdMs(value: Long) = apply { httpSlowThresholdMs = value }

        fun memorySampleIntervalMs(value: Long) = apply { memorySampleIntervalMs = value }

        fun systemSamplerEnabled(value: Boolean) = apply { systemSamplerEnabled = value }

        fun systemSampleIntervalMs(value: Long) = apply { systemSampleIntervalMs = value }

        fun mainLooperDispatchMonitorEnabled(value: Boolean) = apply { mainLooperDispatchMonitorEnabled = value }

        fun processExitInfoEnabled(value: Boolean) = apply { processExitInfoEnabled = value }

        fun objectWatcherEnabled(value: Boolean) = apply { objectWatcherEnabled = value }

        fun retainedObjectDelayMs(value: Long) = apply { retainedObjectDelayMs = value }

        fun retainedObjectForceGcEnabled(value: Boolean) = apply { retainedObjectForceGcEnabled = value }

        fun retainedHeapDumpEnabled(value: Boolean) = apply { retainedHeapDumpEnabled = value }

        fun retainedHeapDumpPrivacyApproved(value: Boolean) = apply { retainedHeapDumpPrivacyApproved = value }

        fun retainedHeapDumpMinIntervalMs(value: Long) = apply { retainedHeapDumpMinIntervalMs = value }

        fun retainedHeapDumpMaxCount(value: Int) = apply { retainedHeapDumpMaxCount = value }

        fun retainedHeapDumpMinRetainedAgeMs(value: Long) = apply { retainedHeapDumpMinRetainedAgeMs = value }

        fun retainedHeapDumpDirectory(value: File?) = apply { retainedHeapDumpDirectory = value }

        fun fpsMonitorEnabled(value: Boolean) = apply { fpsMonitorEnabled = value }

        fun jankStatsEnabled(value: Boolean) = apply { jankStatsEnabled = value }

        fun fpsWindowMs(value: Long) = apply { fpsWindowMs = value }

        fun jankFrameThresholdMs(value: Long) = apply { jankFrameThresholdMs = value }

        fun uiWindowP95ThresholdMs(value: Long) = apply { uiWindowP95ThresholdMs = value }

        fun maxQueueSize(value: Int) = apply { maxQueueSize = value }

        fun maxLogBytes(value: Long) = apply { maxLogBytes = value }

        fun maxLogDirectoryBytes(value: Long) = apply { maxLogDirectoryBytes = value }

        fun logBucket(value: JankHunterLogBucket) = apply { logBucket = value }

        fun maxDictionaryEntries(value: Int) = apply { maxDictionaryEntries = value }

        fun maxDictionaryValueBytes(value: Int) = apply { maxDictionaryValueBytes = value }

        fun flushIntervalMs(value: Long) = apply { flushIntervalMs = value }

        fun adaptiveSamplingEnabled(value: Boolean) = apply { adaptiveSamplingEnabled = value }

        fun adaptiveMemoryStableIntervalMs(value: Long) = apply { adaptiveMemoryStableIntervalMs = value }

        fun adaptiveContextStableIntervalMs(value: Long) = apply { adaptiveContextStableIntervalMs = value }

        fun metricAggregationEnabled(value: Boolean) = apply { metricAggregationEnabled = value }

        fun metricAggregationWindowMs(value: Long) = apply { metricAggregationWindowMs = value }

        fun maxMetricAggregationKeys(value: Int) = apply { maxMetricAggregationKeys = value }

        fun maxLogSpamKeys(value: Int) = apply { maxLogSpamKeys = value }

        fun maxRuntimeCallGraphKeys(value: Int) = apply { maxRuntimeCallGraphKeys = value }

        fun maxHandlerTrackingEntries(value: Int) = apply { maxHandlerTrackingEntries = value }

        fun maxHandlerWrappersPerRunnable(value: Int) = apply { maxHandlerWrappersPerRunnable = value }

        fun routeRedactor(value: JankHunterRedactor) = apply { routeRedactor = value }

        fun logDirectory(value: File?) = apply { logDirectory = value }

        fun mainProcessOnly(value: Boolean) = apply { mainProcessOnly = value }

        fun allowedProcesses(values: Collection<String>) = apply {
            allowedProcesses = values.mapNotNull { it.trim().takeIf(String::isNotEmpty) }
        }

        fun processNameRedactor(value: JankHunterProcessNameRedactor) = apply {
            processNameRedactor = value
        }

        fun deviceInfoEnabled(value: Boolean) = apply { deviceInfoEnabled = value }

        fun binaryStorage(value: JankHunterBinaryStorage?) = apply { binaryStorage = value }

        fun build(): JankHunterConfig = JankHunterConfig(this)
    }

    companion object {
        const val META_ENABLED = "io.jankhunter.enabled"
        const val META_RUNTIME_ENABLED = "io.jankhunter.runtime_enabled"
        const val META_AUTO_START_COLLECTORS = "io.jankhunter.auto_start_collectors"
        const val META_MAIN_THREAD_STALL_THRESHOLD_MS = "io.jankhunter.main_thread_stall_threshold_ms"
        const val META_OWNER_BLOCK_THRESHOLD_MS = "io.jankhunter.owner_block_threshold_ms"
        const val META_HTTP_SLOW_THRESHOLD_MS = "io.jankhunter.http_slow_threshold_ms"
        const val META_MEMORY_SAMPLE_INTERVAL_MS = "io.jankhunter.memory_sample_interval_ms"
        const val META_SYSTEM_SAMPLER_ENABLED = "io.jankhunter.system_sampler_enabled"
        const val META_SYSTEM_SAMPLE_INTERVAL_MS = "io.jankhunter.system_sample_interval_ms"
        const val META_MAIN_LOOPER_DISPATCH_MONITOR_ENABLED = "io.jankhunter.main_looper_dispatch_monitor_enabled"
        const val META_PROCESS_EXIT_INFO_ENABLED = "io.jankhunter.process_exit_info_enabled"
        const val META_OBJECT_WATCHER_ENABLED = "io.jankhunter.object_watcher_enabled"
        const val META_RETAINED_OBJECT_DELAY_MS = "io.jankhunter.retained_object_delay_ms"
        const val META_RETAINED_OBJECT_FORCE_GC_ENABLED = "io.jankhunter.retained_object_force_gc_enabled"
        const val META_RETAINED_HEAP_DUMP_ENABLED = "io.jankhunter.retained_heap_dump_enabled"
        const val META_RETAINED_HEAP_DUMP_PRIVACY_APPROVED = "io.jankhunter.retained_heap_dump_privacy_approved"
        const val META_RETAINED_HEAP_DUMP_MIN_INTERVAL_MS = "io.jankhunter.retained_heap_dump_min_interval_ms"
        const val META_RETAINED_HEAP_DUMP_MAX_COUNT = "io.jankhunter.retained_heap_dump_max_count"
        const val META_RETAINED_HEAP_DUMP_MIN_RETAINED_AGE_MS =
            "io.jankhunter.retained_heap_dump_min_retained_age_ms"
        const val META_FPS_MONITOR_ENABLED = "io.jankhunter.fps_monitor_enabled"
        const val META_JANKSTATS_ENABLED = "io.jankhunter.jankstats_enabled"
        const val META_FPS_WINDOW_MS = "io.jankhunter.fps_window_ms"
        const val META_JANK_FRAME_THRESHOLD_MS = "io.jankhunter.jank_frame_threshold_ms"
        const val META_UI_WINDOW_P95_THRESHOLD_MS = "io.jankhunter.ui_window_p95_threshold_ms"
        const val META_MAX_QUEUE_SIZE = "io.jankhunter.max_queue_size"
        const val META_MAX_LOG_BYTES = "io.jankhunter.max_log_bytes"
        const val META_MAX_LOG_DIRECTORY_BYTES = "io.jankhunter.max_log_directory_bytes"
        const val META_LOG_BUCKET = "io.jankhunter.log_bucket"
        const val META_MAX_DICTIONARY_ENTRIES = "io.jankhunter.max_dictionary_entries"
        const val META_MAX_DICTIONARY_VALUE_BYTES = "io.jankhunter.max_dictionary_value_bytes"
        const val META_FLUSH_INTERVAL_MS = "io.jankhunter.flush_interval_ms"
        const val META_ADAPTIVE_SAMPLING_ENABLED = "io.jankhunter.adaptive_sampling_enabled"
        const val META_ADAPTIVE_MEMORY_STABLE_INTERVAL_MS = "io.jankhunter.adaptive_memory_stable_interval_ms"
        const val META_ADAPTIVE_CONTEXT_STABLE_INTERVAL_MS = "io.jankhunter.adaptive_context_stable_interval_ms"
        const val META_METRIC_AGGREGATION_ENABLED = "io.jankhunter.metric_aggregation_enabled"
        const val META_METRIC_AGGREGATION_WINDOW_MS = "io.jankhunter.metric_aggregation_window_ms"
        const val META_MAX_METRIC_AGGREGATION_KEYS = "io.jankhunter.max_metric_aggregation_keys"
        const val META_MAX_LOG_SPAM_KEYS = "io.jankhunter.max_log_spam_keys"
        const val META_MAX_RUNTIME_CALL_GRAPH_KEYS = "io.jankhunter.max_runtime_call_graph_keys"
        const val META_MAX_HANDLER_TRACKING_ENTRIES = "io.jankhunter.max_handler_tracking_entries"
        const val META_MAX_HANDLER_WRAPPERS_PER_RUNNABLE = "io.jankhunter.max_handler_wrappers_per_runnable"
        const val META_MAIN_PROCESS_ONLY = "io.jankhunter.main_process_only"
        const val META_ALLOWED_PROCESSES = "io.jankhunter.allowed_processes"
        const val META_DEVICE_INFO_ENABLED = "io.jankhunter.device_info_enabled"

        @JvmStatic
        fun builder(): Builder = Builder()

        @JvmStatic
        fun fromManifest(context: Context): JankHunterConfig {
            val metadata = metadata(context)
            val defaultEnabled = isDebuggable(context)
            return builder()
                .enabled(metadataBoolean(metadata, META_ENABLED, defaultEnabled))
                .runtimeEnabled(metadataBoolean(metadata, META_RUNTIME_ENABLED, true))
                .autoStartCollectors(metadataBoolean(metadata, META_AUTO_START_COLLECTORS, true))
                .mainThreadStallThresholdMs(metadataLong(metadata, META_MAIN_THREAD_STALL_THRESHOLD_MS, 700L))
                .ownerBlockThresholdMs(metadataLong(metadata, META_OWNER_BLOCK_THRESHOLD_MS, 250L))
                .httpSlowThresholdMs(metadataLong(metadata, META_HTTP_SLOW_THRESHOLD_MS, 1_000L))
                .memorySampleIntervalMs(metadataLong(metadata, META_MEMORY_SAMPLE_INTERVAL_MS, 10_000L))
                .systemSamplerEnabled(metadataBoolean(metadata, META_SYSTEM_SAMPLER_ENABLED, true))
                .systemSampleIntervalMs(metadataLong(metadata, META_SYSTEM_SAMPLE_INTERVAL_MS, 15_000L))
                .mainLooperDispatchMonitorEnabled(
                    metadataBoolean(metadata, META_MAIN_LOOPER_DISPATCH_MONITOR_ENABLED, true),
                )
                .processExitInfoEnabled(metadataBoolean(metadata, META_PROCESS_EXIT_INFO_ENABLED, true))
                .objectWatcherEnabled(metadataBoolean(metadata, META_OBJECT_WATCHER_ENABLED, true))
                .retainedObjectDelayMs(metadataLong(metadata, META_RETAINED_OBJECT_DELAY_MS, 5_000L))
                .retainedObjectForceGcEnabled(metadataBoolean(metadata, META_RETAINED_OBJECT_FORCE_GC_ENABLED, false))
                .retainedHeapDumpEnabled(metadataBoolean(metadata, META_RETAINED_HEAP_DUMP_ENABLED, true))
                .retainedHeapDumpPrivacyApproved(
                    metadataBoolean(metadata, META_RETAINED_HEAP_DUMP_PRIVACY_APPROVED, true),
                )
                .retainedHeapDumpMinIntervalMs(
                    metadataLong(metadata, META_RETAINED_HEAP_DUMP_MIN_INTERVAL_MS, 10 * 60_000L),
                )
                .retainedHeapDumpMaxCount(metadataInt(metadata, META_RETAINED_HEAP_DUMP_MAX_COUNT, 1))
                .retainedHeapDumpMinRetainedAgeMs(
                    metadataLong(metadata, META_RETAINED_HEAP_DUMP_MIN_RETAINED_AGE_MS, 30_000L),
                )
                .fpsMonitorEnabled(metadataBoolean(metadata, META_FPS_MONITOR_ENABLED, true))
                .jankStatsEnabled(metadataBoolean(metadata, META_JANKSTATS_ENABLED, true))
                .fpsWindowMs(metadataLong(metadata, META_FPS_WINDOW_MS, 1_000L))
                .jankFrameThresholdMs(metadataLong(metadata, META_JANK_FRAME_THRESHOLD_MS, 32L))
                .uiWindowP95ThresholdMs(metadataLong(metadata, META_UI_WINDOW_P95_THRESHOLD_MS, 32L))
                .maxQueueSize(metadataInt(metadata, META_MAX_QUEUE_SIZE, 2048))
                .maxLogBytes(metadataLong(metadata, META_MAX_LOG_BYTES, 5L * 1024L * 1024L))
                .maxLogDirectoryBytes(
                    metadataLong(metadata, META_MAX_LOG_DIRECTORY_BYTES, 25L * 1024L * 1024L),
                )
                .logBucket(metadataLogBucket(metadata, META_LOG_BUCKET, JankHunterLogBucket.SESSION))
                .maxDictionaryEntries(metadataInt(metadata, META_MAX_DICTIONARY_ENTRIES, 8192))
                .maxDictionaryValueBytes(metadataInt(metadata, META_MAX_DICTIONARY_VALUE_BYTES, 256))
                .flushIntervalMs(metadataLong(metadata, META_FLUSH_INTERVAL_MS, 5_000L))
                .adaptiveSamplingEnabled(metadataBoolean(metadata, META_ADAPTIVE_SAMPLING_ENABLED, true))
                .adaptiveMemoryStableIntervalMs(
                    metadataLong(metadata, META_ADAPTIVE_MEMORY_STABLE_INTERVAL_MS, 60_000L),
                )
                .adaptiveContextStableIntervalMs(
                    metadataLong(metadata, META_ADAPTIVE_CONTEXT_STABLE_INTERVAL_MS, 60_000L),
                )
                .metricAggregationEnabled(metadataBoolean(metadata, META_METRIC_AGGREGATION_ENABLED, true))
                .metricAggregationWindowMs(metadataLong(metadata, META_METRIC_AGGREGATION_WINDOW_MS, 5_000L))
                .maxMetricAggregationKeys(metadataInt(metadata, META_MAX_METRIC_AGGREGATION_KEYS, 2048))
                .maxLogSpamKeys(metadataInt(metadata, META_MAX_LOG_SPAM_KEYS, 2048))
                .maxRuntimeCallGraphKeys(metadataInt(metadata, META_MAX_RUNTIME_CALL_GRAPH_KEYS, 4096))
                .maxHandlerTrackingEntries(metadataInt(metadata, META_MAX_HANDLER_TRACKING_ENTRIES, 4096))
                .maxHandlerWrappersPerRunnable(
                    metadataInt(metadata, META_MAX_HANDLER_WRAPPERS_PER_RUNNABLE, 32),
                )
                .mainProcessOnly(metadataBoolean(metadata, META_MAIN_PROCESS_ONLY, true))
                .allowedProcesses(parseProcessList(metadataString(metadata, META_ALLOWED_PROCESSES)))
                .deviceInfoEnabled(metadataBoolean(metadata, META_DEVICE_INFO_ENABLED, defaultEnabled))
                .build()
        }

        private fun parseProcessList(raw: String?): List<String> {
            return raw
                ?.split(',')
                ?.mapNotNull { it.trim().takeIf(String::isNotEmpty) }
                ?: emptyList()
        }

        internal fun metadataBoolean(metadata: Bundle?, key: String, defaultValue: Boolean): Boolean {
            return coerceMetadataBoolean(metadataValue(metadata, key), defaultValue)
        }

        internal fun metadataLong(metadata: Bundle?, key: String, defaultValue: Long): Long {
            return coerceMetadataLong(metadataValue(metadata, key), defaultValue)
        }

        internal fun metadataInt(metadata: Bundle?, key: String, defaultValue: Int): Int {
            return coerceMetadataInt(metadataValue(metadata, key), defaultValue)
        }

        internal fun metadataLogBucket(
            metadata: Bundle?,
            key: String,
            defaultValue: JankHunterLogBucket,
        ): JankHunterLogBucket {
            return coerceMetadataLogBucket(metadataValue(metadata, key), defaultValue)
        }

        internal fun coerceMetadataBoolean(value: Any?, defaultValue: Boolean): Boolean {
            return when (value) {
                is Boolean -> value
                is Number -> value.toInt() != 0
                is String -> when (value.trim().lowercase()) {
                    "true", "1" -> true
                    "false", "0" -> false
                    else -> defaultValue
                }
                else -> defaultValue
            }
        }

        internal fun coerceMetadataLong(value: Any?, defaultValue: Long): Long {
            return when (value) {
                is Number -> value.toLong()
                is String -> value.trim().toLongOrNull() ?: defaultValue
                else -> defaultValue
            }
        }

        internal fun coerceMetadataInt(value: Any?, defaultValue: Int): Int {
            return when (value) {
                is Number -> value.toInt()
                is String -> value.trim().toIntOrNull() ?: defaultValue
                else -> defaultValue
            }
        }

        internal fun coerceMetadataLogBucket(
            value: Any?,
            defaultValue: JankHunterLogBucket,
        ): JankHunterLogBucket {
            return when (value) {
                is JankHunterLogBucket -> value
                is String -> JankHunterLogBucket.from(value) ?: defaultValue
                else -> defaultValue
            }
        }

        private fun metadataString(metadata: Bundle?, key: String): String? {
            return when (val value = metadataValue(metadata, key)) {
                is String -> value
                null -> null
                else -> value.toString()
            }
        }

        @Suppress("DEPRECATION")
        private fun metadataValue(metadata: Bundle?, key: String): Any? = metadata?.get(key)

        private fun metadata(context: Context): Bundle? {
            return try {
                @Suppress("DEPRECATION")
                context.packageManager
                    .getApplicationInfo(context.packageName, android.content.pm.PackageManager.GET_META_DATA)
                    .metaData
            } catch (_: Exception) {
                null
            }
        }

        private fun isDebuggable(context: Context): Boolean {
            val flags = context.applicationInfo?.flags ?: 0
            return flags and ApplicationInfo.FLAG_DEBUGGABLE != 0
        }
    }
}
