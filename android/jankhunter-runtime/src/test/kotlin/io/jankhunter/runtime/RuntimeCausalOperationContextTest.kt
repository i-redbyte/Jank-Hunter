package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.CausalKind
import io.jankhunter.runtime.internal.io.Jhlog
import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeCausalOperationContextTest {
    @Test fun ioAndDatabaseLabelsNeverBecomeCausalCodeSources() {
        val directory = Files.createTempDirectory("jh-causal-label-privacy").toFile()
        val config = JankHunterConfig.builder()
            .availableRuntimeFeatures(setOf(
                JankHunterRuntimeFeature.CONCURRENCY_DIAGNOSTICS,
                JankHunterRuntimeFeature.SQLITE,
            ))
            .build()
        val writer = AsyncLogWriterFactory().open(directory, config, "causal-label-privacy")
        val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1_000L })
        graph.state.bindCurrentConfiguration(config)
        graph.state.runtimeEnabled.set(true)
        graph.state.writer = writer
        graph.coordinator.markStarted(config)
        try {
            val worker = Any()
            val parent = checkNotNull(graph.concurrencyTelemetry.beginConcurrencyWorker(worker, 42L, 0, 0, 1L, 2L))
            val previous = graph.concurrencyTelemetry.installConcurrencyContext(parent)
            val untrustedLabel = "/data/user/0/app/private.db SELECT password FROM account"
            val token = try {
                graph.ioTelemetry.recordInternal(JankHunterIOOperation.FILE_READ, 1_000_000L, 7L,
                    null, JankHunterIOOutcome.SUCCESS, 17L, untrustedLabel, parent)
                graph.databaseTelemetry.enter()
            } finally {
                graph.concurrencyTelemetry.restoreConcurrencyContext(previous)
            }
            assertTrue(token > 0L)
            graph.databaseTelemetry.exit(token, 18L, untrustedLabel, "SELECT password FROM account",
                19L, Jhlog.DATABASE_FRAMEWORK_SQLITE.toInt(), Jhlog.DATABASE_OPERATION_QUERY.toInt(),
                Jhlog.DATABASE_BOUNDARY_EXECUTE.toInt(), false, Jhlog.DATABASE_RESULT_UNKNOWN.toInt(),
                Jhlog.DATABASE_COUNT_UNKNOWN.toInt(), 0L, true, null)
            graph.concurrencyTelemetry.endConcurrencyWorker(worker, parent, JankHunterWorkerOutcome.SUCCESS)
            graph.coordinator.beginStop()
            assertTrue(writer.close(5_000L))
            assertNull(writer.terminalFailureCause())
            val costs = CausalJhlogTestReader.read(directory).filter { it.kind == CausalKind.OPERATION.toLong() }
            assertEquals(2, costs.size)
            costs.forEach { cost ->
                assertEquals(CausalKind.WORKER_ATTEMPT.toLong(), cost.parentKind)
                assertEquals(parent.entityId, cost.parentId)
                assertEquals(0L, cost.presence and SOURCE_METHOD_PRESENT)
            }
        } finally {
            graph.coordinator.beginStop()
            writer.close(5_000L)
            directory.deleteRecursively()
        }
    }

    @Test fun initiationSurvivesClaimAndCannotCrossReusedSlot() {
        val tokens = RuntimeAsyncTokenTable(RuntimeAsyncTokenIds(), capacity = 1)
        val initiator = JankHunterCausalContext(1L, 2, 7L)
        val first = tokens.begin(RuntimeAsyncTokenTable.DATABASE, 10L, initiator)
        assertSame(initiator, tokens.causalContext(first))
        assertEquals(10L, tokens.claim(first, RuntimeAsyncTokenTable.DATABASE))
        assertSame(initiator, tokens.causalContext(first))
        tokens.release(first)
        assertNull(tokens.causalContext(first))
        val next = tokens.begin(RuntimeAsyncTokenTable.DATABASE, 20L)
        assertNull(tokens.causalContext(first))
        assertNull(tokens.causalContext(next))
    }

    @Test fun closingAnEpochReleasesInitiatorContext() {
        val tokens = RuntimeAsyncTokenTable(RuntimeAsyncTokenIds())
        val token = tokens.begin(RuntimeAsyncTokenTable.HTTP, 1L, JankHunterCausalContext(1L, 2, 1L))
        tokens.close { _, _, _ -> }
        assertNull(tokens.causalContext(token))
    }

    @Test fun httpTotalsNeedWholeCallAndExactByteEvidence() {
        val known = JankHunterNetworkEventFlags.HTTP_RESPONSE_BYTES_KNOWN
        val totals = JankHunterNetworkEventFlags.HTTP_BODY_TOTALS
        assertEquals(-1L, httpCausalBytes(1024L, known, known))
        assertEquals(-1L, httpCausalBytes(1024L, totals, known))
        assertEquals(-1L, httpCausalBytes(-1L, totals or known, known))
        assertEquals(0L, httpCausalBytes(0L, totals or known, known))
        assertEquals(1024L, httpCausalBytes(1024L, totals or known, known))
        assertEquals(0L, httpCausalDurationNanos(0L))
        assertEquals(1_000_000L, httpCausalDurationNanos(1L))
        assertEquals(-1L, httpCausalDurationNanos(-1L))
        assertEquals(-1L, httpCausalDurationNanos(Long.MAX_VALUE))
    }

    private companion object {
        const val SOURCE_METHOD_PRESENT = 4L
    }
}
