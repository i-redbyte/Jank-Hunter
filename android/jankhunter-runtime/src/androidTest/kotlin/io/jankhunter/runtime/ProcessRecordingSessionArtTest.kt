package io.jankhunter.runtime

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.io.ProcessRecordingSession
import io.jankhunter.runtime.internal.io.ProcessRunCohort
import io.jankhunter.runtime.internal.system.RetainedHeapDumper
import java.io.File
import java.util.zip.ZipFile
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

/** Run repeatedly in separate instrumentation processes to validate real launch boundaries. */
@RunWith(AndroidJUnit4::class)
class ProcessRecordingSessionArtTest {
    @Test
    fun shutdownReleasesRunCohortAfterWriterSeals() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val context = instrumentation.targetContext
        val root = File(context.filesDir, "process-recording-shutdown")
        instrumentation.runOnMainSync { JankHunter.shutdown() }
        root.deleteRecursively()
        val config = JankHunterConfig.builder().autoStartCollectors(false).mainProcessOnly(true).logDirectory(root).build()
        try {
            instrumentation.runOnMainSync { JankHunter.init(context, config) }
            JankHunterTelemetry.counter("shutdown.lease", 1L)
            assertTrue(ProcessRunCohort.activeRunIds(root).isNotEmpty())
        } finally {
            instrumentation.runOnMainSync { JankHunter.shutdown() }
        }
        val deadline = System.nanoTime() + 5_000_000_000L
        while (ProcessRunCohort.activeRunIds(root).isNotEmpty() && System.nanoTime() < deadline) Thread.sleep(10L)
        assertTrue("shutdown left a live run cohort lease", ProcessRunCohort.activeRunIds(root).isEmpty())
    }

    @Test
    fun appLaunchKeepsOneFileAcrossReconfigurationAndExport() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val context = instrumentation.targetContext
        val root = File(context.filesDir, "process-recording-e2e")
        val destination = File(context.filesDir, "process-recording-export")
        val reset = InstrumentationRegistry.getArguments().getString("jankhunter.resetRecording") == "true"
        if (reset) root.deleteRecursively()
        destination.deleteRecursively()
        val config = JankHunterConfig.builder()
            .autoStartCollectors(false)
            .mainProcessOnly(true)
            .retainedHeapDumpEnabled(true)
            .logDirectory(root)
            .build()
        instrumentation.runOnMainSync { JankHunter.init(context, config) }
        try {
            repeat(3) { epoch ->
                if (epoch > 0) assertTrue(JankHunter.reconfigure("epoch.$epoch") { })
                JankHunterTelemetry.counter("e2e.epoch.$epoch", (epoch + 1).toLong())
                JankHunter.captureLogSnapshot().use { snapshot ->
                    assertNotNull(JankHunter.initDiagnostics().toString(), snapshot)
                    assertEquals(1, checkNotNull(snapshot).logPaths.size)
                    File(snapshot.logPaths.single()).inputStream().use { input ->
                        val magic = ByteArray(ProcessRecordingSession.MAGIC.size)
                        assertEquals(magic.size, input.read(magic))
                        assertArrayEquals(ProcessRecordingSession.MAGIC, magic)
                    }
                }
                assertEquals(1, root.walkTopDown().count { it.isFile && it.extension == "jhlog" })
            }
            val log = root.walkTopDown().single { it.isFile && it.extension == "jhlog" }
            val process = checkNotNull(log.parentFile)
            // Exercise the real ART dump path after a detector callback, not a forced export dump.
            val dumper = RetainedHeapDumper(process, minIntervalMs = 0L, maxDumpCount = 1, minRetainedAgeMs = 1L)
            val dump = dumper.maybeDump("e2e.RetainedActivity", "e2e.Root", 10L, 1L)
            assertTrue("ART dump result: $dump", dump is RetainedHeapDumper.Result.Dumped)
            val exported = checkNotNull(JankHunter.captureSessionArchives(destination))
            assertEquals(root.listFiles().orEmpty().count { it.name.endsWith(".jhlog.zip") } + 1, exported.size)
            exported.sorted().forEachIndexed { index, path ->
                ZipFile(path).use { zip ->
                    val entries = zip.entries().toList()
                    assertEquals(1, entries.count { it.name.endsWith(".jhlog") })
                    assertEquals(if (index == exported.lastIndex) 1 else 0, entries.count { it.name.endsWith(".hprof") })
                }
            }
            val boundedDestination = File(context.filesDir, "process-recording-bounded-export")
            boundedDestination.deleteRecursively()
            val bounded = checkNotNull(JankHunter.captureSessionArchives(boundedDestination, 16L * 1024L))
            assertEquals(exported.size, bounded.size)
            for (path in bounded) {
                ZipFile(path).use { zip ->
                    val entries = zip.entries().toList()
                    assertEquals(1, entries.count { it.name.endsWith(".jhlog") })
                    assertFalse(entries.any { it.name.endsWith(".hprof") })
                }
            }
            assertTrue(process.listFiles().orEmpty().any { it.extension == "hprof" && it.length() > 0L })
        } finally {
            instrumentation.runOnMainSync { JankHunter.shutdown() }
        }
    }
}
