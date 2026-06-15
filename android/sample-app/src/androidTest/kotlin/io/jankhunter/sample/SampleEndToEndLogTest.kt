package io.jankhunter.sample

import android.os.SystemClock
import androidx.test.core.app.ActivityScenario
import androidx.test.ext.junit.runners.AndroidJUnit4
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterConfig
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class SampleEndToEndLogTest {
    @Test
    fun writesJhlogAfterSyntheticSignals() {
        ActivityScenario.launch(MainActivity::class.java).use { scenario ->
            lateinit var logDir: File
            scenario.onActivity { activity ->
                logDir = File(activity.filesDir, "jankhunter-e2e")
                JankHunter.shutdown()
                logDir.deleteRecursively()
                logDir.mkdirs()

                JankHunter.init(
                    activity.applicationContext,
                    JankHunterConfig.builder()
                        .enabled(true)
                        .autoStartCollectors(true)
                        .flushIntervalMs(100)
                        .retainedObjectDelayMs(100)
                        .maxLogBytes(262_144)
                        .logDirectory(logDir)
                        .build(),
                )

                JankHunter.setScreen("SampleEndToEnd")
                JankHunter.withOwner("sample.e2e.synthetic_stall") {
                    SystemClock.sleep(280)
                }
                JankHunter.watchObject(RetainedProbe(), "io.jankhunter.sample.RetainedProbe", "sample.e2e.retained_probe")
                JankHunter.recordCounter("sample.e2e.retained.watch.count", 1)
            }

            val workerDone = CountDownLatch(1)
            val executor = Executors.newSingleThreadExecutor()
            executor.execute {
                try {
                    val start = SystemClock.elapsedRealtime()
                    SystemClock.sleep(80)
                    JankHunter.recordGauge("sample.e2e.background.duration_ms", SystemClock.elapsedRealtime() - start)
                    JankHunter.recordCounter("sample.e2e.background.count", 1)
                    JankHunter.flush()
                } finally {
                    workerDone.countDown()
                }
            }
            assertTrue("background work timed out", workerDone.await(5, TimeUnit.SECONDS))
            executor.shutdownNow()

            SystemClock.sleep(250)
            JankHunter.flush()
            JankHunter.shutdown()

            val logFile = waitForLog(logDir)
            assertTrue("expected non-empty .jhlog at ${logFile.absolutePath}", logFile.length() > 0)
        }
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

    private class RetainedProbe
}
