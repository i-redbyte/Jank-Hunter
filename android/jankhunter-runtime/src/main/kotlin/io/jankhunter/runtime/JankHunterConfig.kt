package io.jankhunter.runtime

import android.content.Context
import android.content.pm.ApplicationInfo
import android.os.Bundle
import java.io.File

class JankHunterConfig private constructor(builder: Builder) {
    private val enabled = builder.enabled
    private val autoStartCollectors = builder.autoStartCollectors
    private val mainThreadStallThresholdMs = builder.mainThreadStallThresholdMs
    private val memorySampleIntervalMs = builder.memorySampleIntervalMs
    private val systemSamplerEnabled = builder.systemSamplerEnabled
    private val systemSampleIntervalMs = builder.systemSampleIntervalMs
    private val mainLooperDispatchMonitorEnabled = builder.mainLooperDispatchMonitorEnabled
    private val processExitInfoEnabled = builder.processExitInfoEnabled
    private val objectWatcherEnabled = builder.objectWatcherEnabled
    private val retainedObjectDelayMs = builder.retainedObjectDelayMs
    private val retainedObjectForceGcEnabled = builder.retainedObjectForceGcEnabled
    private val fpsMonitorEnabled = builder.fpsMonitorEnabled
    private val jankStatsEnabled = builder.jankStatsEnabled
    private val fpsWindowMs = builder.fpsWindowMs
    private val jankFrameThresholdMs = builder.jankFrameThresholdMs
    private val maxQueueSize = builder.maxQueueSize
    private val maxLogBytes = builder.maxLogBytes
    private val maxLogDirectoryBytes = builder.maxLogDirectoryBytes
    private val maxDictionaryEntries = builder.maxDictionaryEntries
    private val maxDictionaryValueBytes = builder.maxDictionaryValueBytes
    private val flushIntervalMs = builder.flushIntervalMs
    private val routeRedactor = builder.routeRedactor
    private val logDirectory = builder.logDirectory
    private val mainProcessOnly = builder.mainProcessOnly
    private val allowedProcesses = builder.allowedProcesses.toSet()
    private val processNameRedactor = builder.processNameRedactor

    fun enabled(): Boolean = enabled

    fun autoStartCollectors(): Boolean = autoStartCollectors

    fun mainThreadStallThresholdMs(): Long = mainThreadStallThresholdMs

    fun memorySampleIntervalMs(): Long = memorySampleIntervalMs

    fun systemSamplerEnabled(): Boolean = systemSamplerEnabled

    fun systemSampleIntervalMs(): Long = systemSampleIntervalMs

    fun mainLooperDispatchMonitorEnabled(): Boolean = mainLooperDispatchMonitorEnabled

    fun processExitInfoEnabled(): Boolean = processExitInfoEnabled

    fun objectWatcherEnabled(): Boolean = objectWatcherEnabled

    fun retainedObjectDelayMs(): Long = retainedObjectDelayMs

    fun retainedObjectForceGcEnabled(): Boolean = retainedObjectForceGcEnabled

    fun fpsMonitorEnabled(): Boolean = fpsMonitorEnabled

    fun jankStatsEnabled(): Boolean = jankStatsEnabled

    fun fpsWindowMs(): Long = fpsWindowMs

    fun jankFrameThresholdMs(): Long = jankFrameThresholdMs

    fun maxQueueSize(): Int = maxQueueSize

    fun maxLogBytes(): Long = maxLogBytes

    fun maxLogDirectoryBytes(): Long = maxLogDirectoryBytes

    fun maxDictionaryEntries(): Int = maxDictionaryEntries

    fun maxDictionaryValueBytes(): Int = maxDictionaryValueBytes

    fun flushIntervalMs(): Long = flushIntervalMs

    fun redactRoute(route: String?): String? = routeRedactor.redact(route)

    fun logDirectory(): File? = logDirectory

    fun mainProcessOnly(): Boolean = mainProcessOnly

    fun allowedProcesses(): Set<String> = allowedProcesses

    fun redactProcessName(processName: String?): String? = processNameRedactor.redact(processName)

    fun isProcessAllowed(processName: String, mainProcessName: String): Boolean {
        if (mainProcessOnly && processName != mainProcessName) return false
        if (allowedProcesses.isNotEmpty() && processName !in allowedProcesses) return false
        return true
    }

    class Builder {
        internal var enabled = true
        internal var autoStartCollectors = true
        internal var mainThreadStallThresholdMs = 700L
        internal var memorySampleIntervalMs = 10_000L
        internal var systemSamplerEnabled = true
        internal var systemSampleIntervalMs = 15_000L
        internal var mainLooperDispatchMonitorEnabled = false
        internal var processExitInfoEnabled = true
        internal var objectWatcherEnabled = true
        internal var retainedObjectDelayMs = 5_000L
        internal var retainedObjectForceGcEnabled = false
        internal var fpsMonitorEnabled = true
        internal var jankStatsEnabled = false
        internal var fpsWindowMs = 1_000L
        internal var jankFrameThresholdMs = 32L
        internal var maxQueueSize = 2048
        internal var maxLogBytes = 5L * 1024L * 1024L
        internal var maxLogDirectoryBytes = 25L * 1024L * 1024L
        internal var maxDictionaryEntries = 8192
        internal var maxDictionaryValueBytes = 256
        internal var flushIntervalMs = 5_000L
        internal var routeRedactor: JankHunterRedactor = JankHunterRedactor.default()
        internal var logDirectory: File? = null
        internal var mainProcessOnly = false
        internal var allowedProcesses: List<String> = emptyList()
        internal var processNameRedactor: JankHunterProcessNameRedactor = JankHunterProcessNameRedactor.none()

        fun enabled(value: Boolean) = apply { enabled = value }

        fun autoStartCollectors(value: Boolean) = apply { autoStartCollectors = value }

        fun mainThreadStallThresholdMs(value: Long) = apply { mainThreadStallThresholdMs = value }

        fun memorySampleIntervalMs(value: Long) = apply { memorySampleIntervalMs = value }

        fun systemSamplerEnabled(value: Boolean) = apply { systemSamplerEnabled = value }

        fun systemSampleIntervalMs(value: Long) = apply { systemSampleIntervalMs = value }

        fun mainLooperDispatchMonitorEnabled(value: Boolean) = apply { mainLooperDispatchMonitorEnabled = value }

        fun processExitInfoEnabled(value: Boolean) = apply { processExitInfoEnabled = value }

        fun objectWatcherEnabled(value: Boolean) = apply { objectWatcherEnabled = value }

        fun retainedObjectDelayMs(value: Long) = apply { retainedObjectDelayMs = value }

        fun retainedObjectForceGcEnabled(value: Boolean) = apply { retainedObjectForceGcEnabled = value }

        fun fpsMonitorEnabled(value: Boolean) = apply { fpsMonitorEnabled = value }

        fun jankStatsEnabled(value: Boolean) = apply { jankStatsEnabled = value }

        fun fpsWindowMs(value: Long) = apply { fpsWindowMs = value }

        fun jankFrameThresholdMs(value: Long) = apply { jankFrameThresholdMs = value }

        fun maxQueueSize(value: Int) = apply { maxQueueSize = value }

        fun maxLogBytes(value: Long) = apply { maxLogBytes = value }

        fun maxLogDirectoryBytes(value: Long) = apply { maxLogDirectoryBytes = value }

        fun maxDictionaryEntries(value: Int) = apply { maxDictionaryEntries = value }

        fun maxDictionaryValueBytes(value: Int) = apply { maxDictionaryValueBytes = value }

        fun flushIntervalMs(value: Long) = apply { flushIntervalMs = value }

        fun routeRedactor(value: JankHunterRedactor) = apply { routeRedactor = value }

        fun logDirectory(value: File?) = apply { logDirectory = value }

        fun mainProcessOnly(value: Boolean) = apply { mainProcessOnly = value }

        fun allowedProcesses(values: Collection<String>) = apply {
            allowedProcesses = values.mapNotNull { it.trim().takeIf(String::isNotEmpty) }
        }

        fun processNameRedactor(value: JankHunterProcessNameRedactor) = apply {
            processNameRedactor = value
        }

        fun build(): JankHunterConfig = JankHunterConfig(this)
    }

    companion object {
        const val META_ENABLED = "io.jankhunter.enabled"
        const val META_AUTO_START_COLLECTORS = "io.jankhunter.auto_start_collectors"
        const val META_MAIN_THREAD_STALL_THRESHOLD_MS = "io.jankhunter.main_thread_stall_threshold_ms"
        const val META_MEMORY_SAMPLE_INTERVAL_MS = "io.jankhunter.memory_sample_interval_ms"
        const val META_SYSTEM_SAMPLER_ENABLED = "io.jankhunter.system_sampler_enabled"
        const val META_SYSTEM_SAMPLE_INTERVAL_MS = "io.jankhunter.system_sample_interval_ms"
        const val META_MAIN_LOOPER_DISPATCH_MONITOR_ENABLED = "io.jankhunter.main_looper_dispatch_monitor_enabled"
        const val META_PROCESS_EXIT_INFO_ENABLED = "io.jankhunter.process_exit_info_enabled"
        const val META_OBJECT_WATCHER_ENABLED = "io.jankhunter.object_watcher_enabled"
        const val META_RETAINED_OBJECT_DELAY_MS = "io.jankhunter.retained_object_delay_ms"
        const val META_RETAINED_OBJECT_FORCE_GC_ENABLED = "io.jankhunter.retained_object_force_gc_enabled"
        const val META_FPS_MONITOR_ENABLED = "io.jankhunter.fps_monitor_enabled"
        const val META_JANKSTATS_ENABLED = "io.jankhunter.jankstats_enabled"
        const val META_FPS_WINDOW_MS = "io.jankhunter.fps_window_ms"
        const val META_JANK_FRAME_THRESHOLD_MS = "io.jankhunter.jank_frame_threshold_ms"
        const val META_MAX_QUEUE_SIZE = "io.jankhunter.max_queue_size"
        const val META_MAX_LOG_BYTES = "io.jankhunter.max_log_bytes"
        const val META_MAX_LOG_DIRECTORY_BYTES = "io.jankhunter.max_log_directory_bytes"
        const val META_MAX_DICTIONARY_ENTRIES = "io.jankhunter.max_dictionary_entries"
        const val META_MAX_DICTIONARY_VALUE_BYTES = "io.jankhunter.max_dictionary_value_bytes"
        const val META_FLUSH_INTERVAL_MS = "io.jankhunter.flush_interval_ms"
        const val META_MAIN_PROCESS_ONLY = "io.jankhunter.main_process_only"
        const val META_ALLOWED_PROCESSES = "io.jankhunter.allowed_processes"

        @JvmStatic
        fun builder(): Builder = Builder()

        @JvmStatic
        fun fromManifest(context: Context): JankHunterConfig {
            val metadata = metadata(context)
            return builder()
                .enabled(metadata?.getBoolean(META_ENABLED, isDebuggable(context)) ?: isDebuggable(context))
                .autoStartCollectors(metadata?.getBoolean(META_AUTO_START_COLLECTORS, true) ?: true)
                .mainThreadStallThresholdMs(metadata?.getLong(META_MAIN_THREAD_STALL_THRESHOLD_MS, 700L) ?: 700L)
                .memorySampleIntervalMs(metadata?.getLong(META_MEMORY_SAMPLE_INTERVAL_MS, 10_000L) ?: 10_000L)
                .systemSamplerEnabled(metadata?.getBoolean(META_SYSTEM_SAMPLER_ENABLED, true) ?: true)
                .systemSampleIntervalMs(metadata?.getLong(META_SYSTEM_SAMPLE_INTERVAL_MS, 15_000L) ?: 15_000L)
                .mainLooperDispatchMonitorEnabled(
                    metadata?.getBoolean(META_MAIN_LOOPER_DISPATCH_MONITOR_ENABLED, false) ?: false,
                )
                .processExitInfoEnabled(metadata?.getBoolean(META_PROCESS_EXIT_INFO_ENABLED, true) ?: true)
                .objectWatcherEnabled(metadata?.getBoolean(META_OBJECT_WATCHER_ENABLED, true) ?: true)
                .retainedObjectDelayMs(metadata?.getLong(META_RETAINED_OBJECT_DELAY_MS, 5_000L) ?: 5_000L)
                .retainedObjectForceGcEnabled(metadata?.getBoolean(META_RETAINED_OBJECT_FORCE_GC_ENABLED, false) ?: false)
                .fpsMonitorEnabled(metadata?.getBoolean(META_FPS_MONITOR_ENABLED, true) ?: true)
                .jankStatsEnabled(metadata?.getBoolean(META_JANKSTATS_ENABLED, false) ?: false)
                .fpsWindowMs(metadata?.getLong(META_FPS_WINDOW_MS, 1_000L) ?: 1_000L)
                .jankFrameThresholdMs(metadata?.getLong(META_JANK_FRAME_THRESHOLD_MS, 32L) ?: 32L)
                .maxQueueSize(metadata?.getInt(META_MAX_QUEUE_SIZE, 2048) ?: 2048)
                .maxLogBytes(metadata?.getLong(META_MAX_LOG_BYTES, 5L * 1024L * 1024L) ?: 5L * 1024L * 1024L)
                .maxLogDirectoryBytes(
                    metadata?.getLong(META_MAX_LOG_DIRECTORY_BYTES, 25L * 1024L * 1024L)
                        ?: 25L * 1024L * 1024L,
                )
                .maxDictionaryEntries(metadata?.getInt(META_MAX_DICTIONARY_ENTRIES, 8192) ?: 8192)
                .maxDictionaryValueBytes(metadata?.getInt(META_MAX_DICTIONARY_VALUE_BYTES, 256) ?: 256)
                .flushIntervalMs(metadata?.getLong(META_FLUSH_INTERVAL_MS, 5_000L) ?: 5_000L)
                .mainProcessOnly(metadata?.getBoolean(META_MAIN_PROCESS_ONLY, false) ?: false)
                .allowedProcesses(parseProcessList(metadata?.getString(META_ALLOWED_PROCESSES)))
                .build()
        }

        private fun parseProcessList(raw: String?): List<String> {
            return raw
                ?.split(',')
                ?.mapNotNull { it.trim().takeIf(String::isNotEmpty) }
                ?: emptyList()
        }

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
