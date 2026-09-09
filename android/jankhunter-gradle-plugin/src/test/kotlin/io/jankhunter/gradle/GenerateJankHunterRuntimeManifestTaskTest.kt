package io.jankhunter.gradle

import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class GenerateJankHunterRuntimeManifestTaskTest {
    @Test
    fun writesExactRuntimeMetadataAndAutoInitProvider() {
        val task = manifestTask("generateRuntimeManifest")
        configureTask(
            task = task,
            autoInit = true,
            retainedHeapDumpEnabled = true,
            ioTracingEnabled = true,
            sessionLogSizeLimitEnabled = true,
            maxSessionLogSizeMiB = 16,
            availableRuntimeFeatures = "HANDLERS,HTTP",
        )

        task.writeManifest()

        val manifest = task.outputFile.get().asFile.readText()
        assertEquals(
            expectedMetadata(
                retainedHeapDumpEnabled = true,
                ioTracingEnabled = true,
                sessionLogSizeLimitEnabled = true,
                maxSessionLogSizeMiB = 16,
                availableRuntimeFeatures = "HANDLERS,HTTP",
            ),
            manifestMetadata(manifest),
        )
        assertTrue(manifest.contains("io.jankhunter.runtime.JankHunterAutoInitProvider"))
        assertTrue(manifest.contains("""android:authorities="${'$'}{applicationId}.jankhunter-init""""))
        assertTrue(manifest.contains("""android:exported="false"""))
        assertTrue(manifest.contains("""android:initOrder="100"""))
        assertFalse(manifest.contains("tools:replace"))
    }

    @Test
    fun writesExactRuntimeMetadataButOmitsProviderWhenAutoInitIsDisabled() {
        val task = manifestTask("generateRuntimeManifestWithoutAutoInit")
        configureTask(
            task = task,
            autoInit = false,
            retainedHeapDumpEnabled = false,
            ioTracingEnabled = false,
            sessionLogSizeLimitEnabled = false,
            maxSessionLogSizeMiB = 32,
            availableRuntimeFeatures = "JANK_STATS",
        )

        task.writeManifest()

        val manifest = task.outputFile.get().asFile.readText()
        assertEquals(
            expectedMetadata(
                retainedHeapDumpEnabled = false,
                ioTracingEnabled = false,
                sessionLogSizeLimitEnabled = false,
                maxSessionLogSizeMiB = 32,
                availableRuntimeFeatures = "JANK_STATS",
            ),
            manifestMetadata(manifest),
        )
        assertFalse(manifest.contains("io.jankhunter.runtime.JankHunterAutoInitProvider"))
        assertFalse(manifest.contains("tools:replace"))
    }

    private fun manifestTask(name: String): GenerateJankHunterRuntimeManifestTask {
        val project = ProjectBuilder.builder().build()
        return project.tasks.register(name, GenerateJankHunterRuntimeManifestTask::class.java).get().also { task ->
            task.outputFile.set(project.layout.buildDirectory.file("jankhunter/$name/AndroidManifest.xml"))
        }
    }

    private fun configureTask(
        task: GenerateJankHunterRuntimeManifestTask,
        autoInit: Boolean,
        retainedHeapDumpEnabled: Boolean,
        ioTracingEnabled: Boolean,
        sessionLogSizeLimitEnabled: Boolean,
        maxSessionLogSizeMiB: Int,
        availableRuntimeFeatures: String,
    ) {
        task.autoInit.set(autoInit)
        task.mainThreadStallThresholdMs.set(700L)
        task.ownerBlockThresholdMs.set(250L)
        task.httpSlowThresholdMs.set(1_000L)
        task.mainLooperDispatchMonitorEnabled.set(true)
        task.retainedHeapDumpEnabled.set(retainedHeapDumpEnabled)
        task.retainedHeapDumpMinIntervalMs.set(123_000L)
        task.retainedHeapDumpMaxCount.set(2)
        task.retainedHeapDumpMinRetainedAgeMs.set(45_000L)
        task.jankStatsEnabled.set(true)
        task.ioTracingEnabled.set(ioTracingEnabled)
        task.jankFrameThresholdMs.set(32L)
        task.uiWindowP95ThresholdMs.set(32L)
        task.mainProcessOnly.set(true)
        task.sessionLogSizeLimitEnabled.set(sessionLogSizeLimitEnabled)
        task.maxSessionLogSizeMiB.set(maxSessionLogSizeMiB)
        task.availableRuntimeFeatures.set(availableRuntimeFeatures)
        task.symbolNamespace.set("0123456789abcdef0123456789abcdef")
    }

    private fun manifestMetadata(manifest: String): List<Pair<String, String>> {
        return META_DATA.findAll(manifest).map { match ->
            match.groupValues[1] to match.groupValues[2]
        }.toList()
    }

    private fun expectedMetadata(
        retainedHeapDumpEnabled: Boolean,
        ioTracingEnabled: Boolean,
        sessionLogSizeLimitEnabled: Boolean,
        maxSessionLogSizeMiB: Int,
        availableRuntimeFeatures: String,
    ): List<Pair<String, String>> {
        return listOf(
            "io.jankhunter.enabled" to "true",
            "io.jankhunter.main_thread_stall_threshold_ms" to "700",
            "io.jankhunter.owner_block_threshold_ms" to "250",
            "io.jankhunter.http_slow_threshold_ms" to "1000",
            "io.jankhunter.main_looper_dispatch_monitor_enabled" to "true",
            "io.jankhunter.retained_heap_dump_enabled" to retainedHeapDumpEnabled.toString(),
            "io.jankhunter.retained_heap_dump_min_interval_ms" to "123000",
            "io.jankhunter.retained_heap_dump_max_count" to "2",
            "io.jankhunter.retained_heap_dump_min_retained_age_ms" to "45000",
            "io.jankhunter.jankstats_enabled" to "true",
            "io.jankhunter.io_tracing_enabled" to ioTracingEnabled.toString(),
            "io.jankhunter.jank_frame_threshold_ms" to "32",
            "io.jankhunter.ui_window_p95_threshold_ms" to "32",
            "io.jankhunter.exact_event_collection_enabled" to "true",
            "io.jankhunter.max_queue_size" to "65536",
            "io.jankhunter.main_thread_admission_wait_ms" to "0",
            "io.jankhunter.background_admission_wait_ms" to "5",
            "io.jankhunter.runtime_call_graph_enabled" to "false",
            "io.jankhunter.available_runtime_features" to availableRuntimeFeatures,
            "io.jankhunter.compose_tracing_enabled" to "true",
            "io.jankhunter.room_tracing_enabled" to "true",
            "io.jankhunter.database_tracing_enabled" to "false",
            "io.jankhunter.worker_tracing_enabled" to "true",
            "io.jankhunter.main_process_only" to "true",
            "io.jankhunter.session_log_size_limit_enabled" to sessionLogSizeLimitEnabled.toString(),
            "io.jankhunter.max_session_log_size_mib" to maxSessionLogSizeMiB.toString(),
            "io.jankhunter.log_growth_analytics_enabled" to "true",
            "io.jankhunter.delete_obsolete_jhlog_formats" to "false",
            "io.jankhunter.symbol_namespace" to "0123456789abcdef0123456789abcdef",
        )
    }

    private companion object {
        val META_DATA = Regex(
            """<meta-data\s+android:name="([^"]+)"\s+android:value="([^"]+)"\s*/>""",
        )
    }
}
