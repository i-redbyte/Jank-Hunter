package io.jankhunter.sample

import android.database.sqlite.SQLiteDatabase
import android.os.SystemClock
import androidx.test.core.app.ActivityScenario
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.runner.lifecycle.ActivityLifecycleMonitorRegistry
import androidx.test.runner.lifecycle.Stage
import io.jankhunter.sample.automatic.ScenarioStep
import io.jankhunter.sample.graph.SampleRoomDatabase
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterConfig
import io.jankhunter.runtime.JankHunterManifestConfig
import io.jankhunter.runtime.JankHunterTelemetry
import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class SampleEndToEndLogTest {
    @Test
    fun recordsCompleteAutomaticScenario() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val context = instrumentation.targetContext
        val logDir = File(context.filesDir, "jankhunter-e2e")
        val config = JankHunterManifestConfig.read(context)
            .toBuilder()
            .flushIntervalMs(250)
            .logDirectory(logDir)
            .retainedHeapDumpDirectory(logDir)
            .build()
        instrumentation.runOnMainSync {
            JankHunter.shutdown()
            logDir.deleteRecursively()
            logDir.mkdirs()
            JankHunter.init(context, config)
        }

        try {
            ActivityScenario.launch(MainActivity::class.java).use {
                waitForResultRoute(instrumentation)
                assertDatabaseScenarioResult(context.getDatabasePath(SampleRoomDatabase.NAME))
                SystemClock.sleep(config.retainedObjectDelayMs() + GC_AND_SCHEDULER_SETTLE_MS)
                val heapDump = waitForStableArtifact(logDir, "hprof", HEAP_DUMP_TIMEOUT_MS)
                assertTrue("expected non-empty .hprof at ${heapDump.absolutePath}", heapDump.length() > 0)

                instrumentation.runOnMainSync {
                    JankHunter.flush()
                }
                SystemClock.sleep(FINAL_FLUSH_DELAY_MS)
            }
        } finally {
            instrumentation.runOnMainSync {
                JankHunter.shutdown()
            }
        }
        val logFile = waitForLog(logDir)
        assertTrue("expected non-empty .jhlog at ${logFile.absolutePath}", logFile.length() > 0)
    }

    private fun assertDatabaseScenarioResult(databaseFile: File) {
        assertTrue("expected Room database at ${databaseFile.absolutePath}", databaseFile.length() > 0)
        val rowCount = SQLiteDatabase.openDatabase(
            databaseFile.absolutePath,
            null,
            SQLiteDatabase.OPEN_READONLY,
        ).use { database ->
            database.rawQuery("SELECT count(*) FROM sample_database_record", null).use { cursor ->
                assertTrue("expected count row", cursor.moveToFirst())
                cursor.getLong(0)
            }
        }
        assertEquals(64L, rowCount)
    }

    private fun waitForResultRoute(instrumentation: android.app.Instrumentation) {
        val deadline = SystemClock.elapsedRealtime() + SCENARIO_TIMEOUT_MS
        while (SystemClock.elapsedRealtime() < deadline) {
            var resultVisible = false
            instrumentation.runOnMainSync {
                resultVisible = ActivityLifecycleMonitorRegistry.getInstance()
                    .getActivitiesInStage(Stage.RESUMED)
                    .any { activity -> activity is MainActivity } &&
                    JankHunterTelemetry.currentScreen() == ScenarioStep.RESULT.screenName
            }
            if (resultVisible) return
            SystemClock.sleep(POLL_INTERVAL_MS)
        }
        fail("automatic scenario did not reach ${ScenarioStep.RESULT.screenName}")
    }

    private fun waitForStableArtifact(logDir: File, extension: String, timeoutMs: Long): File {
        val deadline = SystemClock.elapsedRealtime() + timeoutMs
        var lastPath = ""
        var lastSize = -1L
        var stableSamples = 0
        while (SystemClock.elapsedRealtime() < deadline) {
            val file = logDir
                .listFiles { candidate -> candidate.extension == extension && candidate.length() > 0 }
                ?.maxByOrNull { it.lastModified() }
            if (file != null) {
                if (file.absolutePath == lastPath && file.length() == lastSize) {
                    stableSamples++
                    if (stableSamples >= REQUIRED_STABLE_SAMPLES) return file
                } else {
                    lastPath = file.absolutePath
                    lastSize = file.length()
                    stableSamples = 0
                }
            }
            SystemClock.sleep(POLL_INTERVAL_MS)
        }
        fail("no stable .$extension created in ${logDir.absolutePath}")
        throw AssertionError("unreachable")
    }

    private fun waitForLog(logDir: File): File {
        val deadline = SystemClock.elapsedRealtime() + 5_000
        while (SystemClock.elapsedRealtime() < deadline) {
            val file = logDir
                .listFiles { candidate -> candidate.extension == "jhlog" && candidate.length() > 0 }
                ?.maxByOrNull { it.lastModified() }
            if (file != null) return file
            SystemClock.sleep(100)
        }
        fail("no .jhlog created in ${logDir.absolutePath}")
        throw AssertionError("unreachable")
    }

    private companion object {
        const val SCENARIO_TIMEOUT_MS = 70_000L
        const val HEAP_DUMP_TIMEOUT_MS = 45_000L
        const val GC_AND_SCHEDULER_SETTLE_MS = 6_000L
        const val FINAL_FLUSH_DELAY_MS = 1_000L
        const val POLL_INTERVAL_MS = 250L
        const val REQUIRED_STABLE_SAMPLES = 3
    }
}
