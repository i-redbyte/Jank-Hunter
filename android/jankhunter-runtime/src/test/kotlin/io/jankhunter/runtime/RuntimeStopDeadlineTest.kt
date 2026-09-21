package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.system.RuntimeMaintenanceScheduler
import java.io.IOException
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeStopDeadlineTest {
    @Test
    fun writerCloseUsesRemainingSessionDeadlineAfterMaintenanceTimeout() {
        val directory = Files.createTempDirectory("jankhunter-stop-deadline").toFile()
        val storageEntered = CountDownLatch(1)
        val releaseStorage = CountDownLatch(1)
        val storage = object : JankHunterBinaryStorage {
            override val fileSizeLimitBytes = Long.MAX_VALUE
            override val archivesSizeLimitBytes = Long.MAX_VALUE
            override fun openWriter(fileName: String): JankHunterBinaryWriter {
                storageEntered.countDown()
                check(releaseStorage.await(12L, TimeUnit.SECONDS))
                throw IOException("released test storage")
            }
            override fun createArtifact(fileName: String): JankHunterBinaryArtifact = error("no artifacts")
            override fun cleanup(protectedPaths: Set<String>) = Unit
            override fun listFiles(): List<String> = emptyList()
        }
        val config = JankHunterConfig.builder().binaryStorage(storage)
            .exactEventCollectionEnabled(false).metricAggregationEnabled(true).build()
        val writer = AsyncLogWriterFactory().open(directory, config, "main")
        val graph = RuntimeComponentGraph(nowMs = { 10L }, nowUs = { 10_000L })
        graph.state.config = config
        graph.state.writer = writer
        val scheduler = RuntimeMaintenanceScheduler()
        graph.state.maintenanceScheduler = scheduler
        val maintenanceEntered = CountDownLatch(1)
        val releaseMaintenance = CountDownLatch(1)
        val executor = Executors.newSingleThreadExecutor()
        try {
            writer.counter("trigger.storage", 1L)
            assertTrue(storageEntered.await(1L, TimeUnit.SECONDS))
            assertTrue(scheduler.execute {
                maintenanceEntered.countDown()
                releaseMaintenance.await(12L, TimeUnit.SECONDS)
            })
            assertTrue(maintenanceEntered.await(1L, TimeUnit.SECONDS))
            // Session stop has one 1,000 ms budget. A second full writer-close wait exceeds it.
            executor.submit { graph.session.stop(clearInit = true) }.get(1_500L, TimeUnit.MILLISECONDS)
        } finally {
            releaseMaintenance.countDown()
            releaseStorage.countDown()
            scheduler.shutdown(1_000L)
            executor.shutdown()
            assertTrue(executor.awaitTermination(2L, TimeUnit.SECONDS))
            writer.close()
            directory.deleteRecursively()
        }
    }
}
