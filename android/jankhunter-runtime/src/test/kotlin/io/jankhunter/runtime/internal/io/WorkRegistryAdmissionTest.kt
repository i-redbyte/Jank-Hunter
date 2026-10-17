package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterConfig
import io.jankhunter.runtime.JankHunterWorkRegistryResult
import io.jankhunter.runtime.JankHunterWorkInfo
import io.jankhunter.runtime.JankHunterWorkState
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class WorkRegistryAdmissionTest {
    @Test fun blockedWriterRetainsOnlyOneSnapshotAndAccountsForLoss() {
        val directory = Files.createTempDirectory("registry-admission").toFile()
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
            val snapshot = JankHunterWorkRegistryResult.Snapshot(List(128) { index ->
                JankHunterWorkInfo(
                    uuid = java.util.UUID(0, index.toLong() + 1).toString(), instanceId = index.toLong() + 1,
                    state = JankHunterWorkState.ENQUEUED, runAttempt = 0, generation = 0, tags = emptyList(), omittedTags = 0,
                    initialDelayMs = 0, repeatIntervalMs = null, flexIntervalMs = null, earliestEligibleUnixMs = null,
                    stopReason = null, networkType = "NOT_REQUIRED", constraintsMask = 0, contentTriggers = emptyList(),
                    omittedContentTriggers = 0, progress = emptyList(), output = emptyList(), omittedProgressKeys = 0,
                    omittedOutputKeys = 0, truncatedFields = 0,
                )
            }, 128)
            repeat(10) { writer.workRegistry(1, 10, 20, 30000, snapshot) }
            assertTrue(entered.await(1, TimeUnit.SECONDS))
            val counters = quality.snapshot().associate { it.counterId to it.value }
            assertEquals(16L, counters[QualityCounterId.ACCEPTED_EVENT_TOTAL])
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
