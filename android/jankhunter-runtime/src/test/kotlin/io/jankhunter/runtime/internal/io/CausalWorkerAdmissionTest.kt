package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterConfig
import io.jankhunter.runtime.JankHunterRuntimeFeature
import io.jankhunter.runtime.JankHunterWorkerOutcome
import io.jankhunter.runtime.RuntimeComponentGraph
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class CausalWorkerAdmissionTest {
    @Test
    fun causalWorkerBoundaryDoesNotWaitForExactModeQueueSpace() = withBlockedWriter(false) { graph, writer, quality ->
        var accepted = 0
        while (accepted < 256 && writer.causalEvent(CausalEventObservation(
                CausalKind.EXECUTOR_TASK, CausalPhase.ExecutorTask.SUBMITTED, 1L, observedNanos = 1L,
            ))) accepted++
        assertTrue("bulk lane must be saturated before invoking the worker boundary", accepted < 256)
        beginAndEndWorker(graph)
        assertEquals("background submitted loss must retain kind/phase", 1L, quality.value(0x2221))
        assertEquals(1L, quality.value(QualityCounterId.eventReason(
            Jhlog.TYPE_WORKER_IDENTITY, QualityCounterId.REASON_QUEUE_FULL,
        )))
    }

    @Test
    fun causalWorkerBoundaryDoesNotWaitForAnotherWorkerIdentityPublisher() = withBlockedWriter(true) { graph, _, quality ->
        beginAndEndWorker(graph)
        assertEquals(1L, quality.value(QualityCounterId.eventReason(
            Jhlog.TYPE_WORKER_IDENTITY, QualityCounterId.REASON_ADMISSION_CONTENTION,
        )))
    }

    private fun beginAndEndWorker(graph: RuntimeComponentGraph) {
        val worker = Any()
        val context = checkNotNull(graph.concurrencyTelemetry.beginConcurrencyWorker(worker, 42L, 0, 0, 1L, 2L))
        graph.concurrencyTelemetry.endConcurrencyWorker(worker, context, JankHunterWorkerOutcome.SUCCESS)
    }

    private fun withBlockedWriter(
        blockIdentityPublisher: Boolean,
        action: (RuntimeComponentGraph, AsyncLogWriter, LogQualityCounters) -> Unit,
    ) {
        val directory = Files.createTempDirectory("jh-causal-worker-admission").toFile()
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val producers = Executors.newFixedThreadPool(2)
        val quality = LogQualityCounters()
        val config = JankHunterConfig.builder()
            .availableRuntimeFeatures(setOf(JankHunterRuntimeFeature.CONCURRENCY_DIAGNOSTICS))
            .exactEventCollectionEnabled(true)
            .backgroundAdmissionWaitMs(30_000L)
            .maxQueueSize(64)
            .build()
        val writer = AsyncLogWriter(
            directory, config, "causal", setOf("causal"), true, 1L, "2026-10-14", 1L,
            { 1L }, quality, { null }, AsyncWriterTerminalObserver { _, _, _ -> },
            workerThreadFactory = { task, name ->
                if (blockIdentityPublisher) {
                    object : Thread(task, name) {
                        override fun start() {
                            entered.countDown()
                            check(release.await(10L, TimeUnit.SECONDS))
                            super.start()
                        }
                    }
                } else {
                    Thread({
                        entered.countDown()
                        check(release.await(10L, TimeUnit.SECONDS))
                        task.run()
                    }, name)
                }
            },
        )
        val graph = RuntimeComponentGraph({ 1L }, { 1_000L })
        graph.state.bindCurrentConfiguration(config)
        graph.state.runtimeEnabled.set(true)
        graph.state.writer = writer
        graph.coordinator.markStarted(config)
        try {
            // In the second scenario the barrier holds both legacy identity and writer admission locks.
            val bootstrap = producers.submit { writer.workerIdentity(9L, 9L, 9L) }
            assertTrue(entered.await(2L, TimeUnit.SECONDS))
            val boundary = producers.submit { action(graph, writer, quality) }
            try {
                // A deadlock/30-second admission wait fails while the barrier is still closed.
                boundary.get(1L, TimeUnit.SECONDS)
            } finally {
                release.countDown()
                bootstrap.get(2L, TimeUnit.SECONDS)
            }
        } finally {
            release.countDown()
            producers.shutdown()
            assertTrue(producers.awaitTermination(3L, TimeUnit.SECONDS))
            graph.coordinator.beginStop()
            writer.close(1_000L)
            val failure = writer.terminalFailureCause()
            directory.deleteRecursively()
            assertNull(failure?.stackTraceToString(), failure)
        }
    }
}
