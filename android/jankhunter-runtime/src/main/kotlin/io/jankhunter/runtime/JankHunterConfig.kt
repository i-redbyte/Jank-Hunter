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
    private val processExitInfoEnabled = builder.processExitInfoEnabled
    private val objectWatcherEnabled = builder.objectWatcherEnabled
    private val retainedObjectDelayMs = builder.retainedObjectDelayMs
    private val fpsMonitorEnabled = builder.fpsMonitorEnabled
    private val fpsWindowMs = builder.fpsWindowMs
    private val jankFrameThresholdMs = builder.jankFrameThresholdMs
    private val maxQueueSize = builder.maxQueueSize
    private val maxLogBytes = builder.maxLogBytes
    private val flushIntervalMs = builder.flushIntervalMs
    private val routeRedactor = builder.routeRedactor
    private val logDirectory = builder.logDirectory

    fun enabled(): Boolean = enabled

    fun autoStartCollectors(): Boolean = autoStartCollectors

    fun mainThreadStallThresholdMs(): Long = mainThreadStallThresholdMs

    fun memorySampleIntervalMs(): Long = memorySampleIntervalMs

    fun systemSamplerEnabled(): Boolean = systemSamplerEnabled

    fun systemSampleIntervalMs(): Long = systemSampleIntervalMs

    fun processExitInfoEnabled(): Boolean = processExitInfoEnabled

    fun objectWatcherEnabled(): Boolean = objectWatcherEnabled

    fun retainedObjectDelayMs(): Long = retainedObjectDelayMs

    fun fpsMonitorEnabled(): Boolean = fpsMonitorEnabled

    fun fpsWindowMs(): Long = fpsWindowMs

    fun jankFrameThresholdMs(): Long = jankFrameThresholdMs

    fun maxQueueSize(): Int = maxQueueSize

    fun maxLogBytes(): Long = maxLogBytes

    fun flushIntervalMs(): Long = flushIntervalMs

    fun redactRoute(route: String?): String? = routeRedactor.redact(route)

    fun logDirectory(): File? = logDirectory

    class Builder {
        internal var enabled = true
        internal var autoStartCollectors = true
        internal var mainThreadStallThresholdMs = 700L
        internal var memorySampleIntervalMs = 10_000L
        internal var systemSamplerEnabled = true
        internal var systemSampleIntervalMs = 15_000L
        internal var processExitInfoEnabled = true
        internal var objectWatcherEnabled = true
        internal var retainedObjectDelayMs = 5_000L
        internal var fpsMonitorEnabled = true
        internal var fpsWindowMs = 1_000L
        internal var jankFrameThresholdMs = 32L
        internal var maxQueueSize = 2048
        internal var maxLogBytes = 5L * 1024L * 1024L
        internal var flushIntervalMs = 5_000L
        internal var routeRedactor: JankHunterRedactor = JankHunterRedactor.default()
        internal var logDirectory: File? = null

        fun enabled(value: Boolean) = apply { enabled = value }

        fun autoStartCollectors(value: Boolean) = apply { autoStartCollectors = value }

        fun mainThreadStallThresholdMs(value: Long) = apply { mainThreadStallThresholdMs = value }

        fun memorySampleIntervalMs(value: Long) = apply { memorySampleIntervalMs = value }

        fun systemSamplerEnabled(value: Boolean) = apply { systemSamplerEnabled = value }

        fun systemSampleIntervalMs(value: Long) = apply { systemSampleIntervalMs = value }

        fun processExitInfoEnabled(value: Boolean) = apply { processExitInfoEnabled = value }

        fun objectWatcherEnabled(value: Boolean) = apply { objectWatcherEnabled = value }

        fun retainedObjectDelayMs(value: Long) = apply { retainedObjectDelayMs = value }

        fun fpsMonitorEnabled(value: Boolean) = apply { fpsMonitorEnabled = value }

        fun fpsWindowMs(value: Long) = apply { fpsWindowMs = value }

        fun jankFrameThresholdMs(value: Long) = apply { jankFrameThresholdMs = value }

        fun maxQueueSize(value: Int) = apply { maxQueueSize = value }

        fun maxLogBytes(value: Long) = apply { maxLogBytes = value }

        fun flushIntervalMs(value: Long) = apply { flushIntervalMs = value }

        fun routeRedactor(value: JankHunterRedactor) = apply { routeRedactor = value }

        fun logDirectory(value: File?) = apply { logDirectory = value }

        fun build(): JankHunterConfig = JankHunterConfig(this)
    }

    companion object {
        const val META_ENABLED = "io.jankhunter.enabled"
        const val META_AUTO_START_COLLECTORS = "io.jankhunter.auto_start_collectors"
        const val META_MAIN_THREAD_STALL_THRESHOLD_MS = "io.jankhunter.main_thread_stall_threshold_ms"
        const val META_MEMORY_SAMPLE_INTERVAL_MS = "io.jankhunter.memory_sample_interval_ms"
        const val META_SYSTEM_SAMPLER_ENABLED = "io.jankhunter.system_sampler_enabled"
        const val META_SYSTEM_SAMPLE_INTERVAL_MS = "io.jankhunter.system_sample_interval_ms"
        const val META_PROCESS_EXIT_INFO_ENABLED = "io.jankhunter.process_exit_info_enabled"
        const val META_OBJECT_WATCHER_ENABLED = "io.jankhunter.object_watcher_enabled"
        const val META_RETAINED_OBJECT_DELAY_MS = "io.jankhunter.retained_object_delay_ms"
        const val META_FPS_MONITOR_ENABLED = "io.jankhunter.fps_monitor_enabled"
        const val META_FPS_WINDOW_MS = "io.jankhunter.fps_window_ms"
        const val META_JANK_FRAME_THRESHOLD_MS = "io.jankhunter.jank_frame_threshold_ms"
        const val META_MAX_QUEUE_SIZE = "io.jankhunter.max_queue_size"
        const val META_MAX_LOG_BYTES = "io.jankhunter.max_log_bytes"
        const val META_FLUSH_INTERVAL_MS = "io.jankhunter.flush_interval_ms"

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
                .processExitInfoEnabled(metadata?.getBoolean(META_PROCESS_EXIT_INFO_ENABLED, true) ?: true)
                .objectWatcherEnabled(metadata?.getBoolean(META_OBJECT_WATCHER_ENABLED, true) ?: true)
                .retainedObjectDelayMs(metadata?.getLong(META_RETAINED_OBJECT_DELAY_MS, 5_000L) ?: 5_000L)
                .fpsMonitorEnabled(metadata?.getBoolean(META_FPS_MONITOR_ENABLED, true) ?: true)
                .fpsWindowMs(metadata?.getLong(META_FPS_WINDOW_MS, 1_000L) ?: 1_000L)
                .jankFrameThresholdMs(metadata?.getLong(META_JANK_FRAME_THRESHOLD_MS, 32L) ?: 32L)
                .maxQueueSize(metadata?.getInt(META_MAX_QUEUE_SIZE, 2048) ?: 2048)
                .maxLogBytes(metadata?.getLong(META_MAX_LOG_BYTES, 5L * 1024L * 1024L) ?: 5L * 1024L * 1024L)
                .flushIntervalMs(metadata?.getLong(META_FLUSH_INTERVAL_MS, 5_000L) ?: 5_000L)
                .build()
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
