package io.jankhunter.runtime

import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.io.Jhlog
import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class AsyncEpochWireArtTest {
    @Test
    fun publicPortsPreserveSessionBoundariesAndCurrentTransactionLineage() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val root = File(instrumentation.context.cacheDir, "async-epoch-wire")
        JankHunter.shutdown()
        root.deleteRecursively()
        fun start(name: String) {
            val config = JankHunterConfig.builder().logDirectory(File(root, name)).autoStartCollectors(false)
                .runtimeCallGraphEnabled(false).metricAggregationEnabled(false).build()
            instrumentation.runOnMainSync { JankHunter.init(instrumentation.targetContext, config) }
        }
        val database = Any()
        try {
            start("old")
            val oldWorker = JankHunterWorkerRuntime.started(11L, "old.Worker", 0, 0)
            val oldDatabase = JankHunterHooks.enterDatabase()
            val oldManual = JankHunterDatabaseTracing.beginCall("old.manual", "SELECT 1", JankHunterDatabaseOperation.QUERY)
            val oldTransaction = JankHunterDatabaseTracing.beginTransaction("old.transaction")
            JankHunterHooks.beginDatabaseTransaction(database, 101L, "old.automatic", 1)
            val oldHttp = JankHunterNetworkRuntime.captureHttpContext()
            assertTrue(oldWorker > 0L && oldDatabase > 0L && oldManual != null && oldTransaction != null)
            instrumentation.runOnMainSync { JankHunter.shutdown() }
            start("new")
            finishWorker(oldWorker, 11L, "old.Worker")
            finishDatabase(oldDatabase, "old.query")
            JankHunterDatabaseTracing.endCall(oldManual)
            JankHunterDatabaseTracing.endTransaction(oldTransaction, JankHunterDatabaseTransactionOutcome.SUCCESS)
            JankHunterHooks.endDatabaseTransaction(database, null)
            JankHunterNetworkRuntime.recordHttp(http(oldHttp, "GET /old"))

            val transaction = JankHunterDatabaseTracing.beginTransaction("new.manual.transaction")
            val query = JankHunterHooks.enterDatabase()
            finishDatabase(query, "new.query")
            finishDatabase(query, "new.query")
            val manual = JankHunterDatabaseTracing.beginCall("new.manual.query", "SELECT 2", JankHunterDatabaseOperation.QUERY)
            JankHunterDatabaseTracing.endCall(manual)
            JankHunterDatabaseTracing.endTransaction(transaction, JankHunterDatabaseTransactionOutcome.SUCCESS)
            JankHunterHooks.beginDatabaseTransaction(database, 201L, "new.automatic", 1)
            finishDatabase(JankHunterHooks.enterDatabase(), "new.automatic.query")
            JankHunterHooks.markDatabaseTransactionSuccessful(database)
            JankHunterHooks.endDatabaseTransaction(database, null)
            val worker = JankHunterWorkerRuntime.started(22L, "new.Worker", 0, 0)
            finishWorker(worker, 22L, "new.Worker")
            finishWorker(worker, 22L, "new.Worker")
            val http = http(JankHunterNetworkRuntime.captureHttpContext(), "GET /new")
            JankHunterNetworkRuntime.recordHttp(http)
            JankHunterNetworkRuntime.recordHttp(http)
            JankHunter.flush()
            assertTrue(JankHunter.initDiagnostics().toString(), JankHunter.isStarted())
        } finally {
            instrumentation.runOnMainSync { JankHunter.shutdown() }
        }
        for (name in listOf("old", "new")) {
            val files = File(root, name).walkTopDown().filter { it.isFile && it.extension == "jhlog" }.toList()
            assertEquals(1, files.size)
            files.single().copyTo(File(instrumentation.context.filesDir, "async-epoch-$name-5.1.0.jhlog"), overwrite = true)
        }
    }

    private fun finishWorker(token: Long, id: Long, name: String) = JankHunterWorkerRuntime.finished(
        token, id, name, JankHunterWorkerOutcome.SUCCESS, 0, 0, 0, false)

    private fun finishDatabase(token: Long, source: String) = JankHunterHooks.exitDatabase(
        token, JankHunterSemanticWork.stableId("epoch.fixture.", source), source, "SELECT ?", 1L, Jhlog.DATABASE_FRAMEWORK_SQLITE.toInt(),
        Jhlog.DATABASE_OPERATION_QUERY.toInt(), Jhlog.DATABASE_BOUNDARY_EXECUTE.toInt(),
        false, 0, 0, 0L, true, null)

    private fun http(context: JankHunterContextSnapshot, route: String) = JankHunterHttpEvent(
        context, route, null, 1L, 0L, 0L, 0L, 0L, 0L, 1L, 0L,
        200, 0, 0, JankHunterHttpEvent.PROTOCOL_HTTP_1_1, 0L, 0L, 1, 0, 0, 0, 0, 0, 0, 0L)
}
