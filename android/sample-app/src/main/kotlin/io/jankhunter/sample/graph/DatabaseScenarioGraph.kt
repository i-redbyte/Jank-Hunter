package io.jankhunter.sample.graph

import io.jankhunter.runtime.JankHunterTelemetry

import io.jankhunter.runtime.JankHunter
import java.io.Closeable
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

internal interface DatabaseScenarioStore : Closeable {
    fun resetAndSeed()

    fun runSlowMainQuery()

    fun loadRecord(id: Long): Boolean

    fun executePreparedUpdates(): Int

    fun replaceInTransaction(): Boolean

    fun observeExpectedConstraintFailure(): Boolean

    fun runSlowBackgroundQuery()

    fun runCustomAdapterProbe()
}

internal data class DatabaseScenarioResult(
    val repeatedReads: Int,
    val affectedRows: Int,
    val transactionCompleted: Boolean,
    val expectedFailureObserved: Boolean,
    val backgroundCompleted: Boolean,
    val customAdapterCompleted: Boolean,
)

internal class DatabaseScenarioUseCase(
    private val store: DatabaseScenarioStore,
) : Closeable {
    suspend fun execute(): DatabaseScenarioResult {
        store.resetAndSeed()
        JankHunterTelemetry.traceOperation("sample.auto.database.slow_main") {
            store.runSlowMainQuery()
        }
        val repeatedReads = JankHunterTelemetry.traceOperation("sample.auto.database.repeated_reads") {
            var found = 0
            for (id in 1L..REPEATED_READ_COUNT) {
                if (store.loadRecord(id)) found++
            }
            found
        }
        val affectedRows = JankHunterTelemetry.traceOperation("sample.auto.database.prepared_updates") {
            store.executePreparedUpdates()
        }
        val transactionCompleted = JankHunterTelemetry.traceOperation("sample.auto.database.transaction") {
            store.replaceInTransaction()
        }
        val expectedFailureObserved = JankHunterTelemetry.traceOperation("sample.auto.database.expected_failure") {
            store.observeExpectedConstraintFailure()
        }
        val backgroundCompleted = withContext(Dispatchers.IO) {
            JankHunterTelemetry.traceOperation("sample.auto.database.slow_background") {
                store.runSlowBackgroundQuery()
                true
            }
        }
        val customAdapterCompleted = withContext(Dispatchers.IO) {
            JankHunterTelemetry.traceOperation("sample.auto.database.custom_adapter") {
                store.runCustomAdapterProbe()
                true
            }
        }
        return DatabaseScenarioResult(
            repeatedReads = repeatedReads,
            affectedRows = affectedRows,
            transactionCompleted = transactionCompleted,
            expectedFailureObserved = expectedFailureObserved,
            backgroundCompleted = backgroundCompleted,
            customAdapterCompleted = customAdapterCompleted,
        )
    }

    override fun close() {
        store.close()
    }

    private companion object {
        const val REPEATED_READ_COUNT = 6L
    }
}
