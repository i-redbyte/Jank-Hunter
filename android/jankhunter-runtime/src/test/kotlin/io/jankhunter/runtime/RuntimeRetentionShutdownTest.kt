package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.system.RetainedHeapDumper
import io.jankhunter.runtime.internal.system.RetentionEvidence
import java.io.File
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeRetentionShutdownTest {
    @Test
    fun watcherReporterKeepsTheWriterAndDumperOfItsOwnSession() {
        val directory = Files.createTempDirectory("jankhunter-retention-owner").toFile()
        val config = JankHunterConfig.builder().autoStartCollectors(false).build()
        val oldWriter = AsyncLogWriterFactory().open(File(directory, "old"), config, "main")
        val newWriter = AsyncLogWriterFactory().open(File(directory, "new"), config, "main")
        val graph = RuntimeComponentGraph(nowMs = { 10L }, nowUs = { 10_000L })
        var oldDumps = 0
        var newDumps = 0
        graph.state.writer = oldWriter
        graph.state.retainedHeapDumper = RetainedHeapDumper(
            File(directory, "old"), minIntervalMs = 0L, maxDumpCount = 1,
            dumpHprof = { path -> oldDumps++; File(path).writeText("old") },
        )
        val reporter = graph.retentionTelemetry.bindWatcher()
        graph.state.writer = newWriter
        graph.state.retainedHeapDumper = RetainedHeapDumper(
            File(directory, "new"), minIntervalMs = 0L, maxDumpCount = 1,
            dumpHprof = { path -> newDumps++; File(path).writeText("new") },
        )
        try {
            reporter.record("old.Owner", null, JankHunterContext("old.Screen", "old.Holder", 42L),
                1_000L, 1L, RetentionEvidence.AFTER_EXPLICIT_GC)
            reporter.dump("old.Owner", null, null, 1_000L, 1L)
            newWriter.counter("new.session.probe", 1L)
            assertEquals("old callback must not use the new dumper", 0, newDumps)
            assertEquals(1, oldDumps)
        } finally {
            oldWriter.close()
            newWriter.close()
            System.getProperty("jankhunter.test.fixtureDirectory")?.let { output ->
                for (session in listOf("old", "new")) {
                    val source = File(directory, session).walkTopDown()
                        .single { it.isFile && it.extension == "jhlog" }
                    source.copyTo(File(output, "retention-$session.jhlog").apply { parentFile?.mkdirs() }, overwrite = true)
                }
            }
            directory.deleteRecursively()
        }
    }

    @Test
    fun inFlightDumpKeepsExclusiveOwnershipAcrossCollectorReset() {
        val directory = Files.createTempDirectory("jankhunter-retention-shutdown").toFile()
        val config = JankHunterConfig.builder().autoStartCollectors(false).build()
        val writer = AsyncLogWriterFactory().open(directory, config, "main")
        val graph = RuntimeComponentGraph(nowMs = { 10L }, nowUs = { 10_000L })
        graph.state.writer = writer
        graph.state.config = config
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val dumps = AtomicInteger()
        val executor = Executors.newSingleThreadExecutor()
        fun dumper(): RetainedHeapDumper = RetainedHeapDumper(
            directory, minIntervalMs = 0L, maxDumpCount = 10,
            clock = { 10L }, wallClock = { 10L },
            dumpHprof = { path ->
                val index = dumps.incrementAndGet()
                if (index == 1) {
                    entered.countDown()
                    check(release.await(3L, TimeUnit.SECONDS))
                }
                File(path).writeText("heap-$index")
            },
        )
        graph.state.retainedHeapDumper = dumper()
        val first = executor.submit {
            graph.retentionTelemetry.bindWatcher().dump("old.Owner", null, null, 1_000L, 1L)
        }
        try {
            assertTrue(entered.await(1L, TimeUnit.SECONDS))
            graph.collectors.reset()
            graph.state.retainedHeapDumper = dumper()
            graph.retentionTelemetry.bindWatcher().dump("new.Owner", null, null, 1_000L, 1L)
            assertEquals("reset allowed a second platform dump", 1, dumps.get())
            assertTrue("in-flight process impact must remain attributable", graph.state.heapDumpInProgress.get())
        } finally {
            release.countDown()
            first.get(1L, TimeUnit.SECONDS)
            executor.shutdown()
            assertTrue(executor.awaitTermination(1L, TimeUnit.SECONDS))
            writer.close()
            directory.deleteRecursively()
        }
    }
}
