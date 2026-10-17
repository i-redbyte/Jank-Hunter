package io.jankhunter.sample

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.work.WorkInfo
import androidx.work.WorkManager
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterExecutionObservationMode
import io.jankhunter.runtime.JankHunterManifestConfig
import io.jankhunter.runtime.JankHunterRuntimeFeature
import io.jankhunter.sample.concurrency.ConcurrencyDiagnosticScenario
import java.io.File
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

/** Real Android recordings for Kotlin writer → Go CLI → HTML validation, independent of ICQ. */
@RunWith(AndroidJUnit4::class)
class ConcurrencyDiagnosticsLogTest {
    @Test fun normalModeRecordsCausalWork() = recordScenario(detailed = false)

    @Test fun detailedModeRecordsResourcesAndRetryThenSuccess() = recordScenario(detailed = true)

    private fun recordScenario(detailed: Boolean) = runBlocking {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val context = instrumentation.targetContext
        val observation = JankHunterExecutionObservationMode.valueOf(
            InstrumentationRegistry.getArguments().getString("observation_mode") ?: "WRAPPERS")
        val name = if (detailed) "detailed" else "normal"
        val directory = File(context.filesDir, "concurrency-diagnostics-$name")
        val archive = File(context.getExternalFilesDir(null) ?: context.filesDir, "concurrency-diagnostics-$name.zip")
        check(!archive.exists() || archive.delete()) { "cannot replace this test's previous archive" }
        val config = JankHunterManifestConfig.read(context).toBuilder()
            .taskObservationMode(observation).coroutineObservationMode(observation)
            .logDirectory(directory)
            .runtimeFeatureEnabled(JankHunterRuntimeFeature.HEAP_DUMPS, false)
            .runtimeFeatureEnabled(JankHunterRuntimeFeature.CALL_GRAPH, false)
            .runtimeFeatureEnabled(JankHunterRuntimeFeature.METHOD_COUNTERS, false)
            .runtimeFeatureEnabled(JankHunterRuntimeFeature.MAIN_LOOPER, false)
            .runtimeCallGraphEnabled(false)
            .concurrencyDetailedEnabled(detailed)
            .flushIntervalMs(250)
            .build()
        assertTrue("Gradle capability must reach manifest", config.concurrencyDiagnosticsEnabled())
        instrumentation.runOnMainSync {
            JankHunter.shutdown()
            directory.deleteRecursively()
            check(directory.mkdirs())
            JankHunter.init(context, config)
        }
        assertEquals(observation, JankHunter.executionObservationModes().tasks)
        try {
            ConcurrencyDiagnosticScenario(context).run()
            JankHunter.captureLogSnapshot().use { snapshot ->
                assertNotNull("scenario writer stopped: ${JankHunter.initDiagnostics()}", snapshot)
            }
            if (detailed) {
                val workId = ConcurrencyDiagnosticScenario(context).enqueueRetryWorker()
                val manager = WorkManager.getInstance(context)
                val terminal = withTimeout(45_000L) {
                    manager.getWorkInfoByIdFlow(workId).filterNotNull().first { it.state.isFinished }
                }
                assertEquals(WorkInfo.State.SUCCEEDED, terminal.state)
                assertEquals(1, terminal.outputData.getInt("observed_attempt", -1))
                JankHunter.captureLogSnapshot().use { snapshot ->
                    assertNotNull("Worker writer stopped: ${JankHunter.initDiagnostics()}", snapshot)
                }
            }
            // The coordinated export is stable before shutdown's asynchronous drain finishes.
            assertNotNull("expected a stable causal archive", JankHunter.captureLogArchive(archive))
        } finally {
            instrumentation.runOnMainSync { JankHunter.shutdown() }
        }
        assertTrue(archive.length() > 0L)
    }
}
