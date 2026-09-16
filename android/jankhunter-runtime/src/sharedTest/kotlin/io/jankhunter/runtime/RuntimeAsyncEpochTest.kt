package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.Jhlog
import io.jankhunter.runtime.internal.io.QualityCounterId
import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeAsyncEpochTest {
    @Test
    fun terminalEncodingFailureEndsTheEpochAndAccountsForActiveWork() = withSessions { f ->
        f.graph.networkAdapterTelemetry.captureHttpContext()
        f.graph.databaseTelemetry.enter()
        // A conflicting stable dictionary identity deterministically rejects encoding.
        f.graph.workerTelemetry.started(1L, 77L, "first.Worker", 0, 0)
        f.graph.workerTelemetry.started(2L, 77L, "different.Worker", 0, 0)
        f.flush()
        assertTrue(f.quality(0, QualityCounterId.WRITER_IO_ERROR_TOTAL) > 0L)
        assertTrue(f.graph.telemetryAccess.collectionEpoch == null)
        assertEquals(1L, f.quality(0, QualityCounterId.ASYNC_UNFINISHED_HTTP))
        assertEquals(1L, f.quality(0, QualityCounterId.ASYNC_UNFINISHED_DATABASE))
        assertEquals(2L, f.quality(0, QualityCounterId.ASYNC_UNFINISHED_WORKER))
    }

    @Test
    fun segmentRotationPreservesAnActiveOperationAndItsCollectionEpoch() = withSessions { f ->
        val epoch = f.graph.telemetryAccess.collectionEpoch
        val token = f.graph.workerTelemetry.started(1L, "app.Worker", 0, 0)
        f.sealSegment()
        assertTrue(epoch === f.graph.telemetryAccess.collectionEpoch)
        f.finishWorker(token)
        assertEquals(2L, f.accepted())
        assertEquals(0L, f.quality(0, QualityCounterId.ASYNC_COMPLETION_STALE))
        f.restart()
        assertEquals(0L, f.quality(0, QualityCounterId.ASYNC_UNFINISHED_WORKER))
    }

    @Test
    fun completionWhileCollectionIsOffIsReportedWhenCollectionResumes() = withSessions { f ->
        val token = f.graph.databaseTelemetry.enter()
        f.graph.session.stop(clearInit = false)
        f.finishDatabase(token)
        assertEquals(1L, f.quality(0, QualityCounterId.ASYNC_UNFINISHED_DATABASE))
        f.restart()
        assertEquals(1L, f.quality(1, QualityCounterId.ASYNC_COMPLETION_STALE))
        assertEquals(0L, f.accepted())
    }

    @Test
    fun legacyHttpContextIsOneShotAndItsLimitedCoverageIsExplicit() = withSessions { f ->
        val snapshot = f.graph.networkAdapterTelemetry.captureContext()
        f.graph.networkAdapterTelemetry.recordHttp(http(snapshot))
        f.graph.networkAdapterTelemetry.recordHttp(http(snapshot))
        assertEquals(1L, f.accepted())
        assertEquals(1L, f.quality(0, QualityCounterId.HTTP_LEGACY_CONTEXT_COMPLETION))
        assertEquals(1L, f.quality(0, QualityCounterId.ASYNC_COMPLETION_DUPLICATE))
    }

    @Test
    fun duplicateManualCompletionsAreReportedWithoutAnotherEvent() = withSessions { f ->
        val transaction = f.graph.manualDatabaseTracing.beginTransaction("db", JankHunterDatabaseTransactionMode.IMMEDIATE)
        val call = f.graph.manualDatabaseTracing.beginCall("db", "SELECT 1",
            JankHunterDatabaseOperation.QUERY, JankHunterDatabaseBoundary.EXECUTE)
        f.graph.manualDatabaseTracing.endCall(call, null, 0L, null)
        f.graph.manualDatabaseTracing.endTransaction(transaction, JankHunterDatabaseTransactionOutcome.SUCCESS, null)
        val before = f.accepted()
        f.graph.manualDatabaseTracing.endCall(call, null, 0L, null)
        f.graph.manualDatabaseTracing.endTransaction(transaction, JankHunterDatabaseTransactionOutcome.SUCCESS, null)
        assertEquals(before, f.accepted())
        assertEquals(2L, f.quality(0, QualityCounterId.ASYNC_COMPLETION_DUPLICATE))
    }

    @Test
    fun invalidDatabaseCompletionDoesNotKeepItsActiveSlot() = withSessions { f ->
        val token = f.graph.databaseTelemetry.enter()
        f.finishDatabase(token, sourceId = 0L)
        assertEquals(1L, f.quality(0, QualityCounterId.ASYNC_COMPLETION_INVALID))
        f.restart()
        assertEquals(0L, f.quality(0, QualityCounterId.ASYNC_UNFINISHED_DATABASE))
    }

    @Test
    fun invalidWorkerCompletionDoesNotKeepItsActiveSlot() = withSessions { f ->
        val token = f.graph.workerTelemetry.started(1L, "app.Worker", 0, 0)
        f.graph.workerTelemetry.finished(token, 0L, "app.Worker", JankHunterWorkerOutcome.SUCCESS, 0, 0, 0, false)
        assertEquals(1L, f.quality(0, QualityCounterId.ASYNC_COMPLETION_INVALID))
        f.restart()
        assertEquals(0L, f.quality(0, QualityCounterId.ASYNC_UNFINISHED_WORKER))
    }

    @Test
    fun writerClosureEndsItsEpochBeforeTheSessionControllerCallback() = withSessions { f ->
        val worker = f.graph.workerTelemetry.started(1L, "app.Worker", 0, 0)
        assertTrue(worker > 0L)
        f.closeWriter()
        assertEquals(1L, f.quality(0, QualityCounterId.ASYNC_UNFINISHED_WORKER))
        assertTrue(f.graph.telemetryAccess.collectionEpoch == null)
        f.restart()
        f.finishWorker(worker)
        assertEquals(0L, f.accepted())
    }

    @Test
    fun manualDatabaseCompletionFromStoppedSessionCannotEnterReplacementWriter() = withSessions { f ->
        val token = f.graph.manualDatabaseTracing.beginCall("db", "SELECT 1",
            JankHunterDatabaseOperation.QUERY, JankHunterDatabaseBoundary.EXECUTE)
        assertTrue(token != null)
        f.restart()
        f.graph.manualDatabaseTracing.endCall(token, null, 0L, null)
        assertEquals(0L, f.accepted())
    }

    @Test
    fun manualTransactionFromStoppedSessionCannotEnterReplacementWriter() = withSessions { f ->
        val token = f.graph.manualDatabaseTracing.beginTransaction("db", JankHunterDatabaseTransactionMode.IMMEDIATE)
        assertTrue(token != null)
        f.restart()
        f.graph.manualDatabaseTracing.endTransaction(token, JankHunterDatabaseTransactionOutcome.SUCCESS, null)
        assertEquals(0L, f.accepted())
    }

    @Test
    fun automaticTransactionFromStoppedSessionCannotEnterReplacementWriter() = withSessions { f ->
        val database = Any()
        assertTrue(f.graph.databaseTelemetry.beginTransaction(database, 1L, "db",
            Jhlog.DATABASE_TRANSACTION_IMMEDIATE.toInt()) > 0L)
        f.restart()
        f.graph.databaseTelemetry.endTransaction(database, null)
        assertEquals(0L, f.accepted())
    }

    @Test
    fun trackedHttpCompletionIsConsumedOnlyOnce() = withSessions { f ->
        val snapshot = f.graph.networkAdapterTelemetry.captureHttpContext()
        f.graph.networkAdapterTelemetry.recordHttp(http(snapshot))
        assertEquals(1L, f.accepted())
        f.graph.networkAdapterTelemetry.recordHttp(http(snapshot))
        assertEquals(1L, f.accepted())
        assertEquals(1L, f.quality(0, QualityCounterId.ASYNC_COMPLETION_DUPLICATE))
    }

    @Test
    fun unfinishedWorkBelongsToOldSessionAndLateRejectionsDoNotCountAsNewDeliveryLoss() = withSessions { f ->
        val worker = f.graph.workerTelemetry.started(1L, "app.Worker", 0, 0)
        val database = f.graph.databaseTelemetry.enter()
        val http = f.graph.networkAdapterTelemetry.captureHttpContext()
        f.restart()
        assertEquals(1L, f.quality(0, QualityCounterId.ASYNC_UNFINISHED_WORKER))
        assertEquals(1L, f.quality(0, QualityCounterId.ASYNC_UNFINISHED_DATABASE))
        assertEquals(1L, f.quality(0, QualityCounterId.ASYNC_UNFINISHED_HTTP))
        f.finishWorker(worker)
        f.finishDatabase(database)
        f.graph.networkAdapterTelemetry.recordHttp(http(http))
        assertEquals(0L, f.accepted())
        assertEquals(3L, f.quality(1, QualityCounterId.ASYNC_COMPLETION_STALE))
    }

    @Test
    fun workerCompletionFromStoppedSessionCannotEnterReplacementWriter() = withSessions { f ->
        val token = f.graph.workerTelemetry.started(1L, "app.Worker", 0, 0)
        assertTrue(token != 0L)
        f.restart()
        f.finishWorker(token)
        assertEquals(0L, f.accepted())
    }

    @Test
    fun databaseCompletionFromStoppedSessionCannotEnterReplacementWriter() = withSessions { f ->
        val token = f.graph.databaseTelemetry.enter()
        assertTrue(token != 0L)
        f.restart()
        f.finishDatabase(token)
        assertEquals(0L, f.accepted())
    }

    @Test
    fun httpSnapshotFromStoppedSessionCannotEnterReplacementWriter() = withSessions { f ->
        val snapshot = f.graph.networkAdapterTelemetry.captureContext()
        f.restart()
        f.graph.networkAdapterTelemetry.recordHttp(http(snapshot))
        assertEquals(0L, f.accepted())
    }

    @Test
    fun httpCompletionRespectsTheCurrentFeatureGate() = withSessions { f ->
        val snapshot = f.graph.networkAdapterTelemetry.captureContext()
        val disabled = f.config.toBuilder().runtimeFeatureEnabled(JankHunterRuntimeFeature.HTTP, false).build()
        f.graph.state.bindCurrentConfiguration(disabled)
        f.graph.state.featureGate.activate(disabled)
        f.graph.networkAdapterTelemetry.recordHttp(http(snapshot))
        assertEquals(0L, f.accepted())
    }

    @Test
    fun duplicateWorkerCompletionDoesNotCreateAnotherExecution() = withSessions { f ->
        val token = f.graph.workerTelemetry.started(1L, "app.Worker", 0, 0)
        f.finishWorker(token)
        val accepted = f.accepted()
        f.finishWorker(token)
        assertEquals(accepted, f.accepted())
    }

    @Test
    fun duplicateDatabaseCompletionDoesNotCreateAnotherQuery() = withSessions { f ->
        val token = f.graph.databaseTelemetry.enter()
        f.finishDatabase(token)
        val accepted = f.accepted()
        f.finishDatabase(token)
        assertEquals(accepted, f.accepted())
    }

    private class Sessions {
        val directory = Files.createTempDirectory("jankhunter-async-epoch").toFile()
        val config = JankHunterConfig.builder().autoStartCollectors(false).runtimeCallGraphEnabled(false).build()
        val graph = RuntimeComponentGraph({ 1L }, { 1_000L })
        private val writers = List(2) { AsyncLogWriterFactory().open(directory.resolve("session-$it"), config, "main") }
        private var index = 0

        init { activate() }

        private fun activate() {
            graph.state.bindCurrentConfiguration(config)
            graph.state.runtimeEnabled.set(true)
            graph.state.writer = writers[index]
            graph.coordinator.markStarted(config)
        }

        fun restart() {
            graph.session.stop(clearInit = false)
            index++
            activate()
        }

        fun finishWorker(token: Long) = graph.workerTelemetry.finished(
            token, 1L, "app.Worker", JankHunterWorkerOutcome.SUCCESS, 0, 0, 0, false,
        )

        fun finishDatabase(token: Long, sourceId: Long = 1L) = graph.databaseTelemetry.exit(
            token, sourceId, "app.Repository", "SELECT 1", 1L, Jhlog.DATABASE_FRAMEWORK_SQLITE.toInt(),
            Jhlog.DATABASE_OPERATION_QUERY.toInt(), Jhlog.DATABASE_BOUNDARY_EXECUTE.toInt(),
            false, 0, 0, 0L, true, null,
        )

        fun accepted(): Long {
            assertTrue(writers[index].flushBlocking())
            return generationQuality(writers[index], QualityCounterId.ACCEPTED_EVENT_TOTAL)
        }

        fun quality(session: Int, id: Int): Long = generationQuality(writers[session], id)

        fun flush() = writers[index].flushBlocking()

        fun sealSegment() {
            assertTrue(writers[index].captureSnapshotBlocking() != null)
        }

        fun closeWriter() = writers[index].close()

        fun close() {
            graph.session.stop(clearInit = true)
            writers.forEach { it.close() }
            directory.deleteRecursively()
        }
    }

    private fun withSessions(block: (Sessions) -> Unit) {
        val sessions = Sessions()
        try { block(sessions) } finally { sessions.close() }
    }

    private fun http(context: JankHunterContextSnapshot) = JankHunterHttpEvent(
        context, "GET /epoch-test", null, 1L, 0L, 0L, 0L, 0L, 0L, 1L, 0L,
        200, 0, 0, JankHunterHttpEvent.PROTOCOL_HTTP_1_1, 0L, 0L, 1, 0, 0, 0, 0, 0, 0, 0L,
    )
}
