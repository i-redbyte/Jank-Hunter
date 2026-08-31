package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.BinaryLogWriter
import io.jankhunter.runtime.internal.io.Jhlog
import io.jankhunter.runtime.internal.io.LogEventContext
import java.io.File
import java.nio.file.Files
import org.junit.Test

class RuntimeDatabaseBenchmarkTest {
    @Test
    fun disabledHookHasBoundedLatencyAndNoSteadyStateAllocation() {
        assumeBenchmarksEnabled()
        val iterations = iterations(DISABLED_MIN_ITERATIONS)
        val result = RuntimeBenchmarkHarness.measure(iterations, WARMUP_ITERATIONS) {
            val token = JankHunterHooks.enterDatabase()
            JankHunterHooks.exitDatabase(
                token = token,
                sourceId = SOURCE_ID,
                sourceName = SOURCE,
                query = QUERY,
                statementFingerprint = FINGERPRINT,
                framework = Jhlog.DATABASE_FRAMEWORK_SQLITE.toInt(),
                operation = Jhlog.DATABASE_OPERATION_QUERY.toInt(),
                boundary = Jhlog.DATABASE_BOUNDARY_EXECUTE.toInt(),
                resultKnown = false,
                resultKind = Jhlog.DATABASE_RESULT_UNKNOWN.toInt(),
                resultCountBucket = Jhlog.DATABASE_COUNT_UNKNOWN.toInt(),
                statementToken = 0L,
                succeeded = true,
                throwable = null,
            )
            token
        }

        RuntimeBenchmarkHarness.report("database hook disabled", result)
        RuntimeBenchmarkHarness.assertBudget(
            "database hook disabled",
            result,
            DISABLED_LATENCY_BUDGET_NS,
            ZERO_ALLOCATION_BUDGET,
        )
    }

    @Test
    fun activeHookHasBoundedProducerCost() {
        assumeBenchmarksEnabled()
        val iterations = iterations(ACTIVE_MIN_ITERATIONS)
        ActiveDatabaseHarness().use { harness ->
            val result = RuntimeBenchmarkHarness.measure(iterations, WARMUP_ITERATIONS) {
                val token = harness.telemetry.enter()
                harness.telemetry.exit(
                    token = token,
                    sourceId = SOURCE_ID,
                    sourceName = SOURCE,
                    query = QUERY,
                    statementFingerprint = FINGERPRINT,
                    framework = Jhlog.DATABASE_FRAMEWORK_SQLITE.toInt(),
                    operation = Jhlog.DATABASE_OPERATION_QUERY.toInt(),
                    boundary = Jhlog.DATABASE_BOUNDARY_EXECUTE.toInt(),
                    resultKnown = false,
                    resultKind = Jhlog.DATABASE_RESULT_UNKNOWN.toInt(),
                    resultCountBucket = Jhlog.DATABASE_COUNT_UNKNOWN.toInt(),
                    statementToken = 0L,
                    succeeded = true,
                    throwable = null,
                )
                token
            }

            RuntimeBenchmarkHarness.report("database hook active", result)
            RuntimeBenchmarkHarness.assertBudget(
                "database hook active",
                result,
                ACTIVE_LATENCY_BUDGET_NS,
                ACTIVE_ALLOCATION_BUDGET,
            )
        }
    }

    @Test
    fun preparedLookupHasBoundedLatencyAndNoSteadyStateAllocation() {
        assumeBenchmarksEnabled()
        val iterations = iterations(PREPARED_MIN_ITERATIONS)
        val registry = PreparedStatementRegistry()
        val statement = Any()
        registry.register(statement, QUERY, FINGERPRINT)

        val result = RuntimeBenchmarkHarness.measure(iterations, WARMUP_ITERATIONS) {
            registry.resolve(statement)?.token ?: 0L
        }

        RuntimeBenchmarkHarness.report("database prepared lookup active", result)
        RuntimeBenchmarkHarness.assertBudget(
            "database prepared lookup active",
            result,
            PREPARED_LATENCY_BUDGET_NS,
            ZERO_ALLOCATION_BUDGET,
        )
    }

    @Test
    fun transactionStateReuseHasNoSteadyStateAllocation() {
        assumeBenchmarksEnabled()
        val iterations = iterations(PREPARED_MIN_ITERATIONS)
        val database = Any()
        val tracker = DatabaseTransactionTracker(nanoTime = System::nanoTime)

        val result = RuntimeBenchmarkHarness.measure(iterations, WARMUP_ITERATIONS) {
            val transactionId = tracker.begin(
                database,
                SOURCE_ID,
                SOURCE,
                Jhlog.DATABASE_TRANSACTION_IMMEDIATE,
            )
            tracker.finish(database, DatabaseFailureKind.OTHER, failed = false)
            transactionId
        }

        RuntimeBenchmarkHarness.report("database transaction state reuse", result)
        RuntimeBenchmarkHarness.assertBudget(
            "database transaction state reuse",
            result,
            TRANSACTION_LATENCY_BUDGET_NS,
            ZERO_ALLOCATION_BUDGET,
        )
    }

    @Test
    fun binaryWriterHasBoundedThroughputAndAllocation() {
        assumeBenchmarksEnabled()
        val iterations = iterations(WRITER_MIN_ITERATIONS)
        val file = File.createTempFile("jankhunter-database-writer-benchmark-", ".jhlog")
        try {
            BinaryLogWriter(file).use { writer ->
                val result = RuntimeBenchmarkHarness.measure(iterations, WARMUP_ITERATIONS) {
                    writer.database(
                        sourceId = SOURCE_ID,
                        sourceName = SOURCE,
                        query = QUERY,
                        framework = Jhlog.DATABASE_FRAMEWORK_SQLITE,
                        operation = Jhlog.DATABASE_OPERATION_QUERY,
                        outcome = Jhlog.DATABASE_OUTCOME_SUCCESS,
                        durationUs = 1_000L,
                        mainThread = false,
                        boundary = Jhlog.DATABASE_BOUNDARY_EXECUTE,
                        statementFingerprint = FINGERPRINT,
                    )
                    FINGERPRINT
                }

                RuntimeBenchmarkHarness.report("database writer active", result)
                RuntimeBenchmarkHarness.assertBudget(
                    "database writer active",
                    result,
                    WRITER_LATENCY_BUDGET_NS,
                    WRITER_ALLOCATION_BUDGET,
                )
            }
        } finally {
            file.delete()
        }
    }

    @Test
    fun binaryWriterReusesContextEnvelopeState() {
        assumeBenchmarksEnabled()
        val iterations = iterations(WRITER_MIN_ITERATIONS)
        val file = File.createTempFile("jankhunter-context-writer-benchmark-", ".jhlog")
        try {
            BinaryLogWriter(file).use { writer ->
                val context = LogEventContext("BenchmarkScreen", "BenchmarkOwner", 7L)
                val writeDatabase: BinaryLogWriter.() -> Unit = {
                    database(
                        sourceId = SOURCE_ID,
                        sourceName = SOURCE,
                        query = QUERY,
                        framework = Jhlog.DATABASE_FRAMEWORK_SQLITE,
                        operation = Jhlog.DATABASE_OPERATION_QUERY,
                        outcome = Jhlog.DATABASE_OUTCOME_SUCCESS,
                        durationUs = 1_000L,
                        mainThread = false,
                        boundary = Jhlog.DATABASE_BOUNDARY_EXECUTE,
                        statementFingerprint = FINGERPRINT,
                    )
                }
                val result = RuntimeBenchmarkHarness.measure(iterations, WARMUP_ITERATIONS) {
                    writer.withProducer(1_000L, 17L, context, writeDatabase)
                    FINGERPRINT
                }

                RuntimeBenchmarkHarness.report("database writer context reuse", result)
                RuntimeBenchmarkHarness.assertBudget(
                    "database writer context reuse",
                    result,
                    WRITER_LATENCY_BUDGET_NS,
                    WRITER_CONTEXT_ALLOCATION_BUDGET,
                )
            }
        } finally {
            file.delete()
        }
    }

    private fun iterations(minimum: Int): Int {
        return RuntimeBenchmarkHarness.iterations(minimum, DEFAULT_ITERATIONS)
    }

    private fun assumeBenchmarksEnabled() {
        RuntimeBenchmarkHarness.assumeEnabled()
    }

    private class ActiveDatabaseHarness : AutoCloseable {
        private val directory = Files.createTempDirectory("jankhunter-active-database-benchmark").toFile()
        private val config = JankHunterConfig.builder()
            .autoStartCollectors(false)
            .databaseTracingEnabled(true)
            .flushIntervalMs(60_000L)
            .build()
        private val state = RuntimeState().apply {
            this.config = this@ActiveDatabaseHarness.config
            writer = AsyncLogWriterFactory().open(directory, this@ActiveDatabaseHarness.config, "benchmark")
            lifecycle = RuntimeLifecycle.STARTED
            started.set(true)
        }
        private val coordinator = RuntimeCoordinator(state) { 1L }
        val telemetry = RuntimeDatabaseTelemetry(
            RuntimeTelemetryAccess(state, ContextTracker(), coordinator, { 1L }, { 100 }),
        )

        override fun close() {
            state.started.set(false)
            state.writer?.close()
            state.writer = null
            directory.deleteRecursively()
        }
    }

    private companion object {
        const val SOURCE_ID = 0x101L
        const val SOURCE = "BenchmarkDatabase.query"
        const val QUERY = "SELECT value FROM benchmark WHERE id = ?"
        const val FINGERPRINT = 0x102L
        const val DEFAULT_ITERATIONS = 100_000
        const val DISABLED_MIN_ITERATIONS = 2_000_000
        const val ACTIVE_MIN_ITERATIONS = 10_000
        const val PREPARED_MIN_ITERATIONS = 100_000
        const val WRITER_MIN_ITERATIONS = 10_000
        const val WARMUP_ITERATIONS = 10_000
        const val DISABLED_LATENCY_BUDGET_NS = 2_500.0
        const val ACTIVE_LATENCY_BUDGET_NS = 100_000.0
        const val PREPARED_LATENCY_BUDGET_NS = 2_500.0
        const val TRANSACTION_LATENCY_BUDGET_NS = 5_000.0
        const val WRITER_LATENCY_BUDGET_NS = 100_000.0
        const val ZERO_ALLOCATION_BUDGET = 0.5
        const val ACTIVE_ALLOCATION_BUDGET = 2_048.0
        const val WRITER_ALLOCATION_BUDGET = 1.0
        const val WRITER_CONTEXT_ALLOCATION_BUDGET = 1.0
    }
}
