package io.jankhunter.runtime

import android.os.Debug
import android.os.SystemClock
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.ProcessRecordingSession
import io.jankhunter.runtime.internal.io.SessionArchiveExporter
import io.jankhunter.runtime.internal.io.SessionStorageBudget
import io.jankhunter.runtime.internal.system.ObjectRetentionWatcher
import io.jankhunter.runtime.internal.system.RetainedHeapDumper
import io.jankhunter.runtime.internal.system.RuntimeMaintenanceScheduler
import java.io.File
import java.security.MessageDigest
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReference
import java.util.zip.ZipFile
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class HostStoragePolicyArtTest {
    @Test
    fun detectorHeapKeepsHashThroughVerifiedAndBoundedExports() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val root = File(context.filesDir, "host-policy-art")
        val exported = File(context.filesDir, "host-policy-export")
        val bounded = File(context.filesDir, "host-policy-bounded")
        listOf(root, exported, bounded).forEach { it.deleteRecursively() }
        val policy = JankHunterStoragePolicy(
            root, 2L * MIB, 256L * MIB, setOf("jhlog", "hprof"), setOf("hprof"), 32_768, true,
        )
        val config = JankHunterConfig.builder().autoStartCollectors(false).storagePolicy(policy).build()
        val recording = ProcessRecordingSession()
        val writer = AsyncLogWriterFactory(recording).open(root, config, "main")
        writer.counter("host.policy.start", 1L)
        assertTrue(writer.flushBlocking(5_000L))
        val scheduler = RuntimeMaintenanceScheduler()
        val now = AtomicLong(1L)
        val result = AtomicReference<RetainedHeapDumper.Result>()
        val process = checkNotNull(writer.awaitSessionProcessDirectory(5_000L))
        val dumper = RetainedHeapDumper(
            process, minIntervalMs = 0L, maxDumpCount = 1, storagePolicy = policy, storageRoot = root,
        )
        val watcher = ObjectRetentionWatcher(
            retainedDelayMs = 1_000L, clock = now::get, exactAdmission = true,
            heapDumpReporter = { name, owner, _, age, count -> result.set(dumper.maybeDump(name, owner, age, count)) },
        )
        val retained = ByteArray(24 * MIB.toInt()) { 7 }
        try {
            writer.counter("host.policy.art", 1L)
            assertTrue(writer.flushBlocking(5_000L))
            watcher.start(scheduler)
            watcher.watch(retained, "ControlledRetainedObject", null, null)
            assertEquals(null, result.get())
            now.set(2_001L)
            val started = SystemClock.elapsedRealtime()
            watcher.checkRetained()
            val dumpMillis = SystemClock.elapsedRealtime() - started
            assertTrue(result.get().toString(), result.get() is RetainedHeapDumper.Result.Dumped)
            val heap = (result.get() as RetainedHeapDumper.Result.Dumped).file
            assertTrue(heap.length() > 20L * MIB)
            assertTrue(SessionStorageBudget.physicalBytes(root) <= policy.archivesSizeLimitBytes)
            assertTrue(writer.close(5_000L))
            val sourceHash = sha256(heap.inputStream())
            val beforePss = Debug.getPss()
            val exportStarted = SystemClock.elapsedRealtime()
            val artifacts = SessionArchiveExporter.exportArtifacts(root, exported, null)
            val exportMillis = SystemClock.elapsedRealtime() - exportStarted
            val artifact = artifacts.single()
            assertEquals(1, artifact.completedHeapDumpCount)
            ZipFile(artifact.archivePath).use { zip ->
                val entries = zip.entries().asSequence().toList()
                assertEquals(1, entries.count { it.name.endsWith(".jhlog") })
                val entry = entries.single { it.name.endsWith(".hprof") }
                assertEquals(heap.length(), entry.size)
                assertEquals(sourceHash, sha256(zip.getInputStream(entry)))
            }
            val limited = SessionArchiveExporter.exportArtifacts(root, bounded, null, maxBytesIncludingHeapDumps = 4_096L)
            assertFalse(limited.single().containsCompletedHeapDump)
            assertEquals(sourceHash, sha256(heap.inputStream()))
            assertEquals(7.toByte(), retained.last())
            File(context.filesDir, "host-policy-evidence.json").writeText(
                JSONObject().put("heapBytes", heap.length()).put("sha256", sourceHash)
                    .put("dumpMillis", dumpMillis).put("exportMillis", exportMillis)
                    .put("archiveBytes", artifact.sizeBytes).put("pssBeforeKiB", beforePss)
                    .put("pssAfterKiB", Debug.getPss()).toString(),
            )
        } finally {
            watcher.stop(5_000L)
            scheduler.shutdown(5_000L)
            writer.close(5_000L)
            recording.close()
        }
    }

    private fun sha256(input: java.io.InputStream): String = input.use {
        val digest = MessageDigest.getInstance("SHA-256")
        val buffer = ByteArray(32_768)
        while (true) {
            val count = it.read(buffer)
            if (count < 0) break
            digest.update(buffer, 0, count)
        }
        digest.digest().joinToString("") { value -> "%02x".format(value) }
    }

    private companion object { const val MIB = 1_048_576L }
}
