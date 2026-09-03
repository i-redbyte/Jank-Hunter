package io.jankhunter.runtime

import android.content.Context
import android.content.pm.ApplicationInfo
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import io.jankhunter.runtime.internal.io.DictionaryIds

internal interface ManifestMetadata {
    fun boolean(key: String, defaultValue: Boolean): Boolean
    fun long(key: String, defaultValue: Long): Long
    fun int(key: String, defaultValue: Int): Int
    fun string(key: String): String?
}

private class BundleManifestMetadata(private val values: Bundle?) : ManifestMetadata {
    override fun boolean(key: String, defaultValue: Boolean): Boolean =
        values?.getBoolean(key, defaultValue) ?: defaultValue

    override fun long(key: String, defaultValue: Long): Long {
        val metadata = values ?: return defaultValue
        if (!metadata.containsKey(key)) return defaultValue
        return metadata.getInt(key).toLong()
    }

    override fun int(key: String, defaultValue: Int): Int = values?.getInt(key, defaultValue) ?: defaultValue

    override fun string(key: String): String? = values?.getString(key)
}

/** Android manifest adapter for the platform-neutral runtime configuration value. */
class JankHunterManifestConfig private constructor() {
    companion object {
        internal const val META_ENABLED = "io.jankhunter.enabled"
        internal const val META_RUNTIME_ENABLED = "io.jankhunter.runtime_enabled"
        internal const val META_RUNTIME_CALL_GRAPH_ENABLED = "io.jankhunter.runtime_call_graph_enabled"
        internal const val META_AUTO_START_COLLECTORS = "io.jankhunter.auto_start_collectors"
        internal const val META_MAIN_THREAD_STALL_THRESHOLD_MS = "io.jankhunter.main_thread_stall_threshold_ms"
        internal const val META_OWNER_BLOCK_THRESHOLD_MS = "io.jankhunter.owner_block_threshold_ms"
        internal const val META_HTTP_SLOW_THRESHOLD_MS = "io.jankhunter.http_slow_threshold_ms"
        internal const val META_MEMORY_SAMPLE_INTERVAL_MS = "io.jankhunter.memory_sample_interval_ms"
        internal const val META_SYSTEM_SAMPLER_ENABLED = "io.jankhunter.system_sampler_enabled"
        internal const val META_SYSTEM_SAMPLE_INTERVAL_MS = "io.jankhunter.system_sample_interval_ms"
        internal const val META_MAIN_LOOPER_DISPATCH_MONITOR_ENABLED =
            "io.jankhunter.main_looper_dispatch_monitor_enabled"
        internal const val META_PROCESS_EXIT_INFO_ENABLED = "io.jankhunter.process_exit_info_enabled"
        internal const val META_IO_TRACING_ENABLED = "io.jankhunter.io_tracing_enabled"
        internal const val META_COMPOSE_TRACING_ENABLED = "io.jankhunter.compose_tracing_enabled"
        internal const val META_ROOM_TRACING_ENABLED = "io.jankhunter.room_tracing_enabled"
        internal const val META_DATABASE_TRACING_ENABLED = "io.jankhunter.database_tracing_enabled"
        internal const val META_WORKER_TRACING_ENABLED = "io.jankhunter.worker_tracing_enabled"
        internal const val META_OBJECT_WATCHER_ENABLED = "io.jankhunter.object_watcher_enabled"
        internal const val META_RETAINED_OBJECT_DELAY_MS = "io.jankhunter.retained_object_delay_ms"
        internal const val META_RETAINED_OBJECT_FORCE_GC_ENABLED = "io.jankhunter.retained_object_force_gc_enabled"
        internal const val META_RETAINED_HEAP_DUMP_ENABLED = "io.jankhunter.retained_heap_dump_enabled"
        internal const val META_RETAINED_HEAP_DUMP_MIN_INTERVAL_MS =
            "io.jankhunter.retained_heap_dump_min_interval_ms"
        internal const val META_RETAINED_HEAP_DUMP_MAX_COUNT = "io.jankhunter.retained_heap_dump_max_count"
        internal const val META_RETAINED_HEAP_DUMP_MIN_RETAINED_AGE_MS =
            "io.jankhunter.retained_heap_dump_min_retained_age_ms"
        internal const val META_FPS_MONITOR_ENABLED = "io.jankhunter.fps_monitor_enabled"
        internal const val META_JANKSTATS_ENABLED = "io.jankhunter.jankstats_enabled"
        internal const val META_FPS_WINDOW_MS = "io.jankhunter.fps_window_ms"
        internal const val META_JANK_FRAME_THRESHOLD_MS = "io.jankhunter.jank_frame_threshold_ms"
        internal const val META_UI_WINDOW_P95_THRESHOLD_MS = "io.jankhunter.ui_window_p95_threshold_ms"
        internal const val META_EXACT_EVENT_COLLECTION_ENABLED = "io.jankhunter.exact_event_collection_enabled"
        internal const val META_MAX_QUEUE_SIZE = "io.jankhunter.max_queue_size"
        internal const val META_MAIN_THREAD_ADMISSION_WAIT_MS = "io.jankhunter.main_thread_admission_wait_ms"
        internal const val META_BACKGROUND_ADMISSION_WAIT_MS = "io.jankhunter.background_admission_wait_ms"
        internal const val META_SESSION_LOG_SIZE_LIMIT_ENABLED = "io.jankhunter.session_log_size_limit_enabled"
        internal const val META_MAX_SESSION_LOG_SIZE_MIB = "io.jankhunter.max_session_log_size_mib"
        internal const val META_LOG_GROWTH_ANALYTICS_ENABLED = "io.jankhunter.log_growth_analytics_enabled"
        internal const val META_DELETE_OBSOLETE_JHLOG_FORMATS = "io.jankhunter.delete_obsolete_jhlog_formats"
        internal const val META_MAX_DICTIONARY_ENTRIES = "io.jankhunter.max_dictionary_entries"
        internal const val META_MAX_DICTIONARY_VALUE_BYTES = "io.jankhunter.max_dictionary_value_bytes"
        internal const val META_FLUSH_INTERVAL_MS = "io.jankhunter.flush_interval_ms"
        internal const val META_ADAPTIVE_SAMPLING_ENABLED = "io.jankhunter.adaptive_sampling_enabled"
        internal const val META_ADAPTIVE_MEMORY_STABLE_INTERVAL_MS =
            "io.jankhunter.adaptive_memory_stable_interval_ms"
        internal const val META_ADAPTIVE_CONTEXT_STABLE_INTERVAL_MS =
            "io.jankhunter.adaptive_context_stable_interval_ms"
        internal const val META_METRIC_AGGREGATION_ENABLED = "io.jankhunter.metric_aggregation_enabled"
        internal const val META_METRIC_AGGREGATION_WINDOW_MS = "io.jankhunter.metric_aggregation_window_ms"
        internal const val META_MAX_METRIC_AGGREGATION_KEYS = "io.jankhunter.max_metric_aggregation_keys"
        internal const val META_MAX_LOG_SPAM_KEYS = "io.jankhunter.max_log_spam_keys"
        internal const val META_MAX_RUNTIME_CALL_GRAPH_KEYS = "io.jankhunter.max_runtime_call_graph_keys"
        internal const val META_MAX_HANDLER_TRACKING_ENTRIES = "io.jankhunter.max_handler_tracking_entries"
        internal const val META_MAX_HANDLER_WRAPPERS_PER_RUNNABLE =
            "io.jankhunter.max_handler_wrappers_per_runnable"
        internal const val META_MAIN_PROCESS_ONLY = "io.jankhunter.main_process_only"
        internal const val META_ALLOWED_PROCESSES = "io.jankhunter.allowed_processes"
        internal const val META_SYMBOL_NAMESPACE = "io.jankhunter.symbol_namespace"

        @JvmStatic
        fun read(context: Context): JankHunterConfig {
            return fromMetadata(metadata(context), isDebuggable(context))
        }

        internal fun fromMetadata(metadata: ManifestMetadata, defaultEnabled: Boolean): JankHunterConfig {
            return JankHunterConfig.builder()
                .enabled(metadata.boolean(META_ENABLED, defaultEnabled))
                .runtimeEnabled(metadata.boolean(META_RUNTIME_ENABLED, true))
                .runtimeCallGraphEnabled(metadata.boolean(META_RUNTIME_CALL_GRAPH_ENABLED, true))
                .autoStartCollectors(metadata.boolean(META_AUTO_START_COLLECTORS, true))
                .mainThreadStallThresholdMs(metadata.long(META_MAIN_THREAD_STALL_THRESHOLD_MS, 700L))
                .ownerBlockThresholdMs(metadata.long(META_OWNER_BLOCK_THRESHOLD_MS, 250L))
                .httpSlowThresholdMs(metadata.long(META_HTTP_SLOW_THRESHOLD_MS, 1_000L))
                .memorySampleIntervalMs(metadata.long(META_MEMORY_SAMPLE_INTERVAL_MS, 10_000L))
                .systemSamplerEnabled(metadata.boolean(META_SYSTEM_SAMPLER_ENABLED, true))
                .systemSampleIntervalMs(metadata.long(META_SYSTEM_SAMPLE_INTERVAL_MS, 15_000L))
                .mainLooperDispatchMonitorEnabled(metadata.boolean(META_MAIN_LOOPER_DISPATCH_MONITOR_ENABLED, false))
                .processExitInfoEnabled(metadata.boolean(META_PROCESS_EXIT_INFO_ENABLED, true))
                .ioTracingEnabled(metadata.boolean(META_IO_TRACING_ENABLED, true))
                .composeTracingEnabled(metadata.boolean(META_COMPOSE_TRACING_ENABLED, true))
                .roomTracingEnabled(metadata.boolean(META_ROOM_TRACING_ENABLED, true))
                .databaseTracingEnabled(metadata.boolean(META_DATABASE_TRACING_ENABLED, true))
                .workerTracingEnabled(metadata.boolean(META_WORKER_TRACING_ENABLED, true))
                .objectWatcherEnabled(metadata.boolean(META_OBJECT_WATCHER_ENABLED, true))
                .retainedObjectDelayMs(metadata.long(META_RETAINED_OBJECT_DELAY_MS, 5_000L))
                .retainedObjectForceGcEnabled(metadata.boolean(META_RETAINED_OBJECT_FORCE_GC_ENABLED, false))
                .retainedHeapDumpEnabled(metadata.boolean(META_RETAINED_HEAP_DUMP_ENABLED, false))
                .retainedHeapDumpMinIntervalMs(metadata.long(META_RETAINED_HEAP_DUMP_MIN_INTERVAL_MS, 600_000L))
                .retainedHeapDumpMaxCount(metadata.int(META_RETAINED_HEAP_DUMP_MAX_COUNT, 1))
                .retainedHeapDumpMinRetainedAgeMs(metadata.long(META_RETAINED_HEAP_DUMP_MIN_RETAINED_AGE_MS, 30_000L))
                .fpsMonitorEnabled(metadata.boolean(META_FPS_MONITOR_ENABLED, true))
                .jankStatsEnabled(metadata.boolean(META_JANKSTATS_ENABLED, true))
                .fpsWindowMs(metadata.long(META_FPS_WINDOW_MS, 1_000L))
                .jankFrameThresholdMs(metadata.long(META_JANK_FRAME_THRESHOLD_MS, 32L))
                .uiWindowP95ThresholdMs(metadata.long(META_UI_WINDOW_P95_THRESHOLD_MS, 32L))
                .exactEventCollectionEnabled(metadata.boolean(META_EXACT_EVENT_COLLECTION_ENABLED, true))
                .maxQueueSize(metadata.int(META_MAX_QUEUE_SIZE, 65_536))
                .mainThreadAdmissionWaitMs(metadata.long(META_MAIN_THREAD_ADMISSION_WAIT_MS, 0L))
                .backgroundAdmissionWaitMs(metadata.long(META_BACKGROUND_ADMISSION_WAIT_MS, 5L))
                .sessionLogSizeLimitEnabled(metadata.boolean(META_SESSION_LOG_SIZE_LIMIT_ENABLED, true))
                .maxSessionLogSizeMiB(metadata.int(META_MAX_SESSION_LOG_SIZE_MIB, 50))
                .logGrowthAnalyticsEnabled(metadata.boolean(META_LOG_GROWTH_ANALYTICS_ENABLED, true))
                .deleteObsoleteJhlogFormats(metadata.boolean(META_DELETE_OBSOLETE_JHLOG_FORMATS, true))
                .maxDictionaryEntries(metadata.int(META_MAX_DICTIONARY_ENTRIES, 8192))
                .maxDictionaryValueBytes(metadata.int(META_MAX_DICTIONARY_VALUE_BYTES, DictionaryIds.DEFAULT_MAX_VALUE_BYTES))
                .flushIntervalMs(metadata.long(META_FLUSH_INTERVAL_MS, 5_000L))
                .adaptiveSamplingEnabled(metadata.boolean(META_ADAPTIVE_SAMPLING_ENABLED, true))
                .adaptiveMemoryStableIntervalMs(metadata.long(META_ADAPTIVE_MEMORY_STABLE_INTERVAL_MS, 60_000L))
                .adaptiveContextStableIntervalMs(metadata.long(META_ADAPTIVE_CONTEXT_STABLE_INTERVAL_MS, 60_000L))
                .metricAggregationEnabled(metadata.boolean(META_METRIC_AGGREGATION_ENABLED, true))
                .metricAggregationWindowMs(metadata.long(META_METRIC_AGGREGATION_WINDOW_MS, 5_000L))
                .maxMetricAggregationKeys(metadata.int(META_MAX_METRIC_AGGREGATION_KEYS, 2048))
                .maxLogSpamKeys(metadata.int(META_MAX_LOG_SPAM_KEYS, 2048))
                .maxRuntimeCallGraphKeys(metadata.int(META_MAX_RUNTIME_CALL_GRAPH_KEYS, 4096))
                .maxHandlerTrackingEntries(metadata.int(META_MAX_HANDLER_TRACKING_ENTRIES, 4096))
                .maxHandlerWrappersPerRunnable(metadata.int(META_MAX_HANDLER_WRAPPERS_PER_RUNNABLE, 32))
                .mainProcessOnly(metadata.boolean(META_MAIN_PROCESS_ONLY, false))
                .allowedProcesses(parseProcessList(metadata.string(META_ALLOWED_PROCESSES)))
                .symbolNamespace(decodeSymbolNamespace(metadata.string(META_SYMBOL_NAMESPACE)))
                .build()
        }

        internal fun mergeBuildSymbolNamespace(config: JankHunterConfig, context: Context): JankHunterConfig {
            return config.toBuilder()
                .symbolNamespace(decodeSymbolNamespace(metadata(context).string(META_SYMBOL_NAMESPACE)))
                .build()
        }

        internal fun decodeSymbolNamespace(raw: String?): ByteArray {
            val value = raw.orEmpty()
            if (value.length != SYMBOL_NAMESPACE_HEX_CHARS) return ByteArray(0)
            val decoded = ByteArray(SYMBOL_NAMESPACE_BYTES)
            for (index in decoded.indices) {
                val high = value[index * 2].hexDigit()
                val low = value[index * 2 + 1].hexDigit()
                if (high < 0 || low < 0) return ByteArray(0)
                decoded[index] = ((high shl 4) or low).toByte()
            }
            return decoded
        }

        private fun parseProcessList(raw: String?): List<String> {
            return raw?.split(',')?.mapNotNull { it.trim().takeIf(String::isNotEmpty) } ?: emptyList()
        }

        private fun Char.hexDigit(): Int = when (this) {
            in '0'..'9' -> code - '0'.code
            in 'a'..'f' -> code - 'a'.code + 10
            else -> -1
        }

        private fun metadata(context: Context): ManifestMetadata {
            return try {
                val packageManager = context.packageManager
                val applicationInfo = if (Build.VERSION.SDK_INT >= 33) {
                    packageManager.getApplicationInfo(
                        context.packageName,
                        PackageManager.ApplicationInfoFlags.of(PackageManager.GET_META_DATA.toLong()),
                    )
                } else {
                    packageManager.getApplicationInfo(context.packageName, PackageManager.GET_META_DATA)
                }
                BundleManifestMetadata(applicationInfo.metaData)
            } catch (_: Exception) {
                BundleManifestMetadata(null)
            }
        }

        private fun isDebuggable(context: Context): Boolean {
            val flags = context.applicationInfo?.flags ?: 0
            return flags and ApplicationInfo.FLAG_DEBUGGABLE != 0
        }

        private const val SYMBOL_NAMESPACE_BYTES = 16
        private const val SYMBOL_NAMESPACE_HEX_CHARS = SYMBOL_NAMESPACE_BYTES * 2
    }
}
