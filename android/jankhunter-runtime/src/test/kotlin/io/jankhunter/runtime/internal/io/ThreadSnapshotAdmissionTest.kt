package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterConfig
import io.jankhunter.runtime.internal.system.ProcThreadSnapshot
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ThreadSnapshotAdmissionTest {
    @Test fun blockedWriterRetainsOnlyOneSnapshotAndAccountsForLoss() {
        val directory = Files.createTempDirectory("thread-admission").toFile()
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val quality = LogQualityCounters()
        val writer = AsyncLogWriter(
            directory, JankHunterConfig.builder().build(), "threads", setOf("threads"), true,
            1L, "2026-10-06", 1L, { 1L }, quality, { null }, AsyncWriterTerminalObserver { _, _, _ -> },
            workerThreadFactory = { task, name -> Thread({
                entered.countDown()
                check(release.await(5, TimeUnit.SECONDS))
                task.run()
            }, name) },
        )
        try {
            val snapshot = ProcThreadSnapshot(emptyList(), true, 0, null)
            repeat(10) { writer.threadSnapshot(snapshot, 1, 0, 5000, 100) }
            assertTrue(entered.await(1, TimeUnit.SECONDS))
            val counters = quality.snapshot().associate { it.counterId to it.value }
            assertEquals(1L, counters[QualityCounterId.ACCEPTED_EVENT_TOTAL])
            assertEquals(9L, counters[QualityCounterId.QUEUE_FULL_TOTAL])
            release.countDown()
            assertTrue(writer.close(1000))
        } finally {
            release.countDown()
            writer.close(1000)
            directory.deleteRecursively()
        }
    }
}
