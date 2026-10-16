package io.jankhunter.runtime

import androidx.work.OneTimeWorkRequest
import androidx.work.Worker
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.CausalKind
import io.jankhunter.runtime.internal.io.CausalPhase
import java.io.File
import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeCausalWorkerTelemetryTest {
    @Test
    fun conflictingEnqueueSitesForOneUuidLeaveWorkerCreatorUnknown() = withSession(
        verify = { directory ->
            val bound = workerBindings(directory).last()
            assertEquals(0L, bound.parentKind)
            assertEquals(0L, bound.parentId)
            assertEquals(0L, bound.presence and APP_CALLER_PRESENT)
        },
    ) { graph ->
        val request = OneTimeWorkRequest.Builder(Worker::class.java).build()
        val parentWorker = Any()
        val parent = checkNotNull(graph.concurrencyTelemetry.beginConcurrencyWorker(parentWorker, 41L, 0, 0, 1L, 2L))
        val previous = graph.concurrencyTelemetry.installConcurrencyContext(parent)
        graph.concurrencyTelemetry.observeCausalWorkSubmissionRequests(
            request, 10L, "app.First#enqueue()V", 12, 1,
        )
        graph.concurrencyTelemetry.restoreConcurrencyContext(previous)
        graph.concurrencyTelemetry.observeCausalWorkSubmissionRequests(
            request, 11L, "app.Second#enqueue()V", 21, 1,
        )
        val target = Any()
        val bound = checkNotNull(graph.concurrencyTelemetry.beginConcurrencyWorker(
            target, 42L, 0, 0, request.id.mostSignificantBits, request.id.leastSignificantBits,
        ))
        graph.concurrencyTelemetry.endConcurrencyWorker(target, bound, JankHunterWorkerOutcome.SUCCESS)
        graph.concurrencyTelemetry.endConcurrencyWorker(parentWorker, parent, JankHunterWorkerOutcome.SUCCESS)
    }

    @Test
    fun repeatedEnqueueAtSameSiteKeepsExactParentAndAppCaller() = withSession(
        verify = { directory ->
            val bound = workerBindings(directory).last()
            assertEquals(CausalKind.WORKER_ATTEMPT.toLong(), bound.parentKind)
            assertTrue(bound.parentId > 0L)
            assertEquals(APP_CALLER_PRESENT, bound.presence and APP_CALLER_PRESENT)
        },
    ) { graph ->
        val request = OneTimeWorkRequest.Builder(Worker::class.java).build()
        val parentWorker = Any()
        val parent = checkNotNull(graph.concurrencyTelemetry.beginConcurrencyWorker(parentWorker, 41L, 0, 0, 1L, 2L))
        val previous = graph.concurrencyTelemetry.installConcurrencyContext(parent)
        repeat(2) {
            graph.concurrencyTelemetry.observeCausalWorkSubmissionRequests(
                request, 10L, "app.First#enqueue()V", 12, 1,
            )
        }
        graph.concurrencyTelemetry.restoreConcurrencyContext(previous)
        val target = Any()
        val bound = checkNotNull(graph.concurrencyTelemetry.beginConcurrencyWorker(
            target, 42L, 0, 0, request.id.mostSignificantBits, request.id.leastSignificantBits,
        ))
        graph.concurrencyTelemetry.endConcurrencyWorker(target, bound, JankHunterWorkerOutcome.SUCCESS)
        graph.concurrencyTelemetry.endConcurrencyWorker(parentWorker, parent, JankHunterWorkerOutcome.SUCCESS)
    }

    @Test
    fun resumedWorkerImplementationSiteIsRecordedOncePerAttemptAndSite() = withSession { graph ->
        val worker = Any()
        val context = checkNotNull(graph.concurrencyTelemetry.beginConcurrencyWorker(
            worker, 42L, 0, 0, 1L, 2L,
        ))
        val previous = graph.concurrencyTelemetry.installConcurrencyContext(context)
        repeat(3) {
            graph.concurrencyTelemetry.observeWorkerImplementationSite(
                worker, 10L, "test.Worker#doJankHunterWork()V", 10, 1,
            )
        }
        assertEquals(1, graph.concurrencyTelemetry.workerImplementationSiteCountForTest())
        graph.concurrencyTelemetry.observeWorkerImplementationSite(
            worker, 11L, "test.Worker#other()V", 11, 1,
        )
        assertEquals(2, graph.concurrencyTelemetry.workerImplementationSiteCountForTest())
        graph.concurrencyTelemetry.restoreConcurrencyContext(previous)
        graph.concurrencyTelemetry.endConcurrencyWorker(worker, context, JankHunterWorkerOutcome.SUCCESS)
        assertEquals(0, graph.concurrencyTelemetry.workerImplementationSiteCountForTest())
    }

    @Test
    fun negativeLegacyWorkerInstanceIdKeepsUnsignedWireBits() = withSession { graph ->
        val worker = Any()
        val context = checkNotNull(graph.concurrencyTelemetry.beginConcurrencyWorker(
            worker, -42L, 0, 0, 0x1234L, 0x5678L,
        ))
        assertTrue(context.entityId > 0L)
        assertEquals(context.entityId,
            graph.state.collectionEpochs.current?.causalRegistry?.find(CausalKind.WORKER_ATTEMPT, worker))
        graph.concurrencyTelemetry.endConcurrencyWorker(worker, context, JankHunterWorkerOutcome.SUCCESS)
    }

    @Test
    fun retryClosesOnlyItsExactAttemptAndNextAttemptGetsANewIdentity() = withSession { graph ->
        val worker = Any()
        val first = checkNotNull(graph.concurrencyTelemetry.beginConcurrencyWorker(worker, 42L, 0, 0, 1L, 2L))
        val previous = graph.concurrencyTelemetry.installConcurrencyContext(first)
        assertEquals(CausalKind.WORKER_ATTEMPT, graph.concurrencyTelemetry.captureCausalContext()?.entityKind)
        graph.concurrencyTelemetry.endConcurrencyWorker(worker, first, JankHunterWorkerOutcome.RETRY)
        graph.concurrencyTelemetry.restoreConcurrencyContext(previous)
        assertNull(graph.concurrencyTelemetry.captureCausalContext())
        assertEquals(0L, graph.state.collectionEpochs.current?.causalRegistry?.find(CausalKind.WORKER_ATTEMPT, worker))

        val next = checkNotNull(graph.concurrencyTelemetry.beginConcurrencyWorker(worker, 42L, 1, 0, 1L, 2L))
        assertNotEquals(first.entityId, next.entityId)
        graph.concurrencyTelemetry.endConcurrencyWorker(worker, first, JankHunterWorkerOutcome.SUCCESS)
        assertEquals(next.entityId, graph.state.collectionEpochs.current?.causalRegistry?.find(CausalKind.WORKER_ATTEMPT, worker))
        graph.concurrencyTelemetry.endConcurrencyWorker(worker, next, JankHunterWorkerOutcome.SUCCESS)
    }

    @Test
    fun overlappingWorkerObjectsKeepSeparateContextsEvenForTheSameRequest() = withSession { graph ->
        val firstWorker = Any()
        val secondWorker = Any()
        val first = checkNotNull(graph.concurrencyTelemetry.beginConcurrencyWorker(firstWorker, 42L, 0, 0, 1L, 2L))
        val second = checkNotNull(graph.concurrencyTelemetry.beginConcurrencyWorker(secondWorker, 42L, 0, 0, 1L, 2L))
        assertNotEquals(first.entityId, second.entityId)
        val original = graph.concurrencyTelemetry.installConcurrencyContext(first)
        val nested = graph.concurrencyTelemetry.installConcurrencyContext(second)
        assertEquals(second.entityId, graph.concurrencyTelemetry.captureCausalContext()?.entityId)
        graph.concurrencyTelemetry.restoreConcurrencyContext(nested)
        assertEquals(first.entityId, graph.concurrencyTelemetry.captureCausalContext()?.entityId)
        graph.concurrencyTelemetry.restoreConcurrencyContext(original)
        graph.concurrencyTelemetry.endConcurrencyWorker(firstWorker, first, JankHunterWorkerOutcome.SUCCESS)
        graph.concurrencyTelemetry.endConcurrencyWorker(secondWorker, second, JankHunterWorkerOutcome.SUCCESS)
        assertNull(graph.concurrencyTelemetry.captureCausalContext())
    }

    private fun workerBindings(directory: File): List<RecordedCausalEvent> =
        CausalJhlogTestReader.read(directory).filter {
            it.kind == CausalKind.WORKER_ATTEMPT.toLong() && it.phase == CausalPhase.WorkerAttempt.BOUND.toLong()
        }.also { assertTrue(it.isNotEmpty()) }

    private fun withSession(verify: (File) -> Unit = {}, block: (RuntimeComponentGraph) -> Unit) {
        val directory = Files.createTempDirectory("jh-causal-worker").toFile()
        val config = JankHunterConfig.builder()
            .availableRuntimeFeatures(setOf(JankHunterRuntimeFeature.CONCURRENCY_DIAGNOSTICS))
            .build()
        val writer = AsyncLogWriterFactory().open(directory, config, "causal-worker")
        val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1_000L })
        graph.state.bindCurrentConfiguration(config)
        graph.state.runtimeEnabled.set(true)
        graph.state.writer = writer
        graph.coordinator.markStarted(config)
        try { block(graph) } finally {
            graph.coordinator.beginStop()
            writer.close(1_000L)
            val failure = writer.terminalFailureCause()
            try {
                assertNull(failure?.stackTraceToString(), failure)
                verify(directory)
            } finally {
                directory.deleteRecursively()
            }
        }
    }

    private companion object {
        const val APP_CALLER_PRESENT = 8L
    }
}
