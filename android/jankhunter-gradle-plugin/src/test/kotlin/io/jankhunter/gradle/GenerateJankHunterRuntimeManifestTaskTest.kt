package io.jankhunter.gradle

import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class GenerateJankHunterRuntimeManifestTaskTest {
    @Test
    fun writesRetainedHeapDumpMetadata() {
        val project = ProjectBuilder.builder().build()
        val task = project.tasks.register(
            "generateRuntimeManifest",
            GenerateJankHunterRuntimeManifestTask::class.java,
        ).get()
        task.outputFile.set(project.layout.buildDirectory.file("jankhunter/AndroidManifest.xml"))
        task.autoInit.set(true)
        task.mainThreadStallThresholdMs.set(700L)
        task.ownerBlockThresholdMs.set(250L)
        task.httpSlowThresholdMs.set(1_000L)
        task.mainLooperDispatchMonitorEnabled.set(true)
        task.retainedHeapDumpEnabled.set(true)
        task.retainedHeapDumpPrivacyApproved.set(true)
        task.retainedHeapDumpMinIntervalMs.set(123_000L)
        task.retainedHeapDumpMaxCount.set(2)
        task.retainedHeapDumpMinRetainedAgeMs.set(45_000L)
        task.jankStatsEnabled.set(true)
        task.jankFrameThresholdMs.set(32L)
        task.uiWindowP95ThresholdMs.set(32L)
        task.mainProcessOnly.set(true)
        task.sessionLogSizeLimitEnabled.set(true)
        task.maxSessionLogSizeMiB.set(16)
        task.symbolNamespace.set("0123456789abcdef0123456789abcdef")

        task.writeManifest()

        val manifest = task.outputFile.get().asFile.readText()
        assertTrue(manifest.contains("io.jankhunter.enabled"))
        assertTrue(manifest.contains("io.jankhunter.runtime.JankHunterAutoInitProvider"))
        assertTrue(manifest.contains("""android:authorities="${'$'}{applicationId}.jankhunter-init""""))
        assertTrue(manifest.contains("""android:exported="false""""))
        assertTrue(manifest.contains("""android:initOrder="100""""))
        assertTrue(manifest.contains("io.jankhunter.retained_heap_dump_enabled"))
        assertTrue(manifest.contains("io.jankhunter.main_thread_stall_threshold_ms"))
        assertTrue(manifest.contains("io.jankhunter.owner_block_threshold_ms"))
        assertTrue(manifest.contains("io.jankhunter.http_slow_threshold_ms"))
        assertTrue(manifest.contains("io.jankhunter.main_looper_dispatch_monitor_enabled"))
        assertTrue(manifest.contains("io.jankhunter.jankstats_enabled"))
        assertTrue(manifest.contains("io.jankhunter.jank_frame_threshold_ms"))
        assertTrue(manifest.contains("io.jankhunter.ui_window_p95_threshold_ms"))
        assertTrue(manifest.contains("""android:value="true""""))
        assertTrue(manifest.contains("io.jankhunter.retained_heap_dump_privacy_approved"))
        assertTrue(manifest.contains("io.jankhunter.retained_heap_dump_min_interval_ms"))
        assertTrue(manifest.contains("""android:value="123000""""))
        assertTrue(manifest.contains("io.jankhunter.retained_heap_dump_max_count"))
        assertTrue(manifest.contains("""android:value="2""""))
        assertTrue(manifest.contains("io.jankhunter.retained_heap_dump_min_retained_age_ms"))
        assertTrue(manifest.contains("""android:value="45000""""))
        assertTrue(manifest.contains("io.jankhunter.main_process_only"))
        assertTrue(manifest.contains("io.jankhunter.session_log_size_limit_enabled"))
        assertTrue(manifest.contains("io.jankhunter.max_session_log_size_mib"))
        assertTrue(manifest.contains("""android:value="16""""))
        assertFalse(manifest.contains("io.jankhunter.max_session_log_bytes"))
        assertFalse(manifest.contains("io.jankhunter.max_session_logs_bytes"))
        assertTrue(manifest.contains("io.jankhunter.symbol_namespace"))
        assertTrue(manifest.contains("0123456789abcdef0123456789abcdef"))
        assertFalse(manifest.contains("tools:replace"))
    }

    @Test
    fun keepsRuntimeMetadataButOmitsProviderWhenAutoInitIsDisabled() {
        val project = ProjectBuilder.builder().build()
        val task = project.tasks.register(
            "generateRuntimeManifest",
            GenerateJankHunterRuntimeManifestTask::class.java,
        ).get()
        task.outputFile.set(project.layout.buildDirectory.file("jankhunter/AndroidManifest.xml"))
        task.autoInit.set(false)
        task.mainThreadStallThresholdMs.set(700L)
        task.ownerBlockThresholdMs.set(250L)
        task.httpSlowThresholdMs.set(1_000L)
        task.mainLooperDispatchMonitorEnabled.set(true)
        task.retainedHeapDumpEnabled.set(false)
        task.retainedHeapDumpPrivacyApproved.set(false)
        task.retainedHeapDumpMinIntervalMs.set(123_000L)
        task.retainedHeapDumpMaxCount.set(2)
        task.retainedHeapDumpMinRetainedAgeMs.set(45_000L)
        task.jankStatsEnabled.set(true)
        task.jankFrameThresholdMs.set(32L)
        task.uiWindowP95ThresholdMs.set(32L)
        task.mainProcessOnly.set(true)
        task.sessionLogSizeLimitEnabled.set(false)
        task.maxSessionLogSizeMiB.set(32)
        task.symbolNamespace.set("0123456789abcdef0123456789abcdef")

        task.writeManifest()

        val manifest = task.outputFile.get().asFile.readText()
        assertTrue(manifest.contains("io.jankhunter.enabled"))
        assertTrue(manifest.contains("""android:value="true""""))
        assertFalse(manifest.contains("io.jankhunter.runtime.JankHunterAutoInitProvider"))
        assertTrue(manifest.contains("io.jankhunter.session_log_size_limit_enabled"))
        assertTrue(manifest.contains("""android:value="false""""))
        assertTrue(manifest.contains("io.jankhunter.max_session_log_size_mib"))
        assertTrue(manifest.contains("""android:value="32""""))
        assertFalse(manifest.contains("io.jankhunter.max_session_log_bytes"))
        assertFalse(manifest.contains("io.jankhunter.max_session_logs_bytes"))
        assertTrue(manifest.contains("io.jankhunter.symbol_namespace"))
        assertTrue(manifest.contains("0123456789abcdef0123456789abcdef"))
        assertFalse(manifest.contains("tools:replace"))
    }
}
