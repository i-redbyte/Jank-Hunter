package io.jankhunter.gradle

import org.gradle.api.DefaultTask
import org.gradle.api.file.RegularFileProperty
import org.gradle.api.provider.Property
import org.gradle.api.tasks.Input
import org.gradle.api.tasks.OutputFile
import org.gradle.api.tasks.TaskAction

abstract class GenerateJankHunterRuntimeManifestTask : DefaultTask() {
    @get:Input
    abstract val autoInit: Property<Boolean>

    @get:Input
    abstract val mainThreadStallThresholdMs: Property<Long>

    @get:Input
    abstract val ownerBlockThresholdMs: Property<Long>

    @get:Input
    abstract val httpSlowThresholdMs: Property<Long>

    @get:Input
    abstract val mainLooperDispatchMonitorEnabled: Property<Boolean>

    @get:Input
    abstract val retainedHeapDumpEnabled: Property<Boolean>

    @get:Input
    abstract val retainedHeapDumpPrivacyApproved: Property<Boolean>

    @get:Input
    abstract val retainedHeapDumpMinIntervalMs: Property<Long>

    @get:Input
    abstract val retainedHeapDumpMaxCount: Property<Int>

    @get:Input
    abstract val retainedHeapDumpMinRetainedAgeMs: Property<Long>

    @get:Input
    abstract val jankStatsEnabled: Property<Boolean>

    @get:Input
    abstract val jankFrameThresholdMs: Property<Long>

    @get:Input
    abstract val uiWindowP95ThresholdMs: Property<Long>

    @get:Input
    abstract val mainProcessOnly: Property<Boolean>

    @get:Input
    abstract val sessionLogSizeLimitEnabled: Property<Boolean>

    @get:Input
    abstract val maxSessionLogSizeMiB: Property<Int>

    @get:Input
    abstract val symbolNamespace: Property<String>

    @get:OutputFile
    abstract val outputFile: RegularFileProperty

    init {
        autoInit.convention(true)
        mainLooperDispatchMonitorEnabled.convention(false)
    }

    @TaskAction
    fun writeManifest() {
        val file = outputFile.get().asFile
        file.parentFile.mkdirs()
        val autoInitProvider = if (autoInit.get()) {
            """
                    <provider
                        android:name="io.jankhunter.runtime.JankHunterAutoInitProvider"
                        android:authorities="${'$'}{applicationId}.jankhunter-init"
                        android:exported="false"
                        android:initOrder="100" />
            """.trimIndent()
        } else {
            ""
        }
        file.writeText(
            """
            <manifest xmlns:android="http://schemas.android.com/apk/res/android">
                <application>
                    <meta-data
                        android:name="io.jankhunter.enabled"
                        android:value="true" />
                    <meta-data
                        android:name="io.jankhunter.main_thread_stall_threshold_ms"
                        android:value="${mainThreadStallThresholdMs.get()}" />
                    <meta-data
                        android:name="io.jankhunter.owner_block_threshold_ms"
                        android:value="${ownerBlockThresholdMs.get()}" />
                    <meta-data
                        android:name="io.jankhunter.http_slow_threshold_ms"
                        android:value="${httpSlowThresholdMs.get()}" />
                    <meta-data
                        android:name="io.jankhunter.main_looper_dispatch_monitor_enabled"
                        android:value="${mainLooperDispatchMonitorEnabled.get()}" />
                    <meta-data
                        android:name="io.jankhunter.retained_heap_dump_enabled"
                        android:value="${retainedHeapDumpEnabled.get()}" />
                    <meta-data
                        android:name="io.jankhunter.retained_heap_dump_privacy_approved"
                        android:value="${retainedHeapDumpPrivacyApproved.get()}" />
                    <meta-data
                        android:name="io.jankhunter.retained_heap_dump_min_interval_ms"
                        android:value="${retainedHeapDumpMinIntervalMs.get()}" />
                    <meta-data
                        android:name="io.jankhunter.retained_heap_dump_max_count"
                        android:value="${retainedHeapDumpMaxCount.get()}" />
                    <meta-data
                        android:name="io.jankhunter.retained_heap_dump_min_retained_age_ms"
                        android:value="${retainedHeapDumpMinRetainedAgeMs.get()}" />
                    <meta-data
                        android:name="io.jankhunter.jankstats_enabled"
                        android:value="${jankStatsEnabled.get()}" />
                    <meta-data
                        android:name="io.jankhunter.jank_frame_threshold_ms"
                        android:value="${jankFrameThresholdMs.get()}" />
                    <meta-data
                        android:name="io.jankhunter.ui_window_p95_threshold_ms"
                        android:value="${uiWindowP95ThresholdMs.get()}" />
                    <meta-data
                        android:name="io.jankhunter.main_process_only"
                        android:value="${mainProcessOnly.get()}" />
                    <meta-data
                        android:name="io.jankhunter.session_log_size_limit_enabled"
                        android:value="${sessionLogSizeLimitEnabled.get()}" />
                    <meta-data
                        android:name="io.jankhunter.max_session_log_size_mib"
                        android:value="${maxSessionLogSizeMiB.get()}" />
                    <meta-data
                        android:name="io.jankhunter.symbol_namespace"
                        android:value="${symbolNamespace.get()}" />
            $autoInitProvider
                </application>
            </manifest>
            """.trimIndent() + "\n",
        )
    }
}
