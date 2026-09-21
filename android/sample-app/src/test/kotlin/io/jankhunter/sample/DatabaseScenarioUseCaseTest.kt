package io.jankhunter.sample

import io.jankhunter.sample.graph.DatabaseScenarioStore
import io.jankhunter.sample.graph.DatabaseScenarioUseCase
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class DatabaseScenarioUseCaseTest {
    @Test
    fun executesEveryDatabaseEvidencePathInDeterministicOrder() = runBlocking {
        val store = FakeDatabaseScenarioStore()
        val result = DatabaseScenarioUseCase(store).execute()

        assertEquals(
            listOf("reset", "main", "query:1", "query:2", "query:3", "query:4", "query:5", "query:6", "prepared", "transaction", "failure", "background", "custom"),
            store.calls,
        )
        assertEquals(6, result.repeatedReads)
        assertEquals(3, result.affectedRows)
        assertTrue(result.transactionCompleted)
        assertTrue(result.expectedFailureObserved)
        assertTrue(result.backgroundCompleted)
        assertTrue(result.customAdapterCompleted)
    }

    private class FakeDatabaseScenarioStore : DatabaseScenarioStore {
        val calls = mutableListOf<String>()

        override fun resetAndSeed() {
            calls += "reset"
        }

        override fun runSlowMainQuery() {
            calls += "main"
        }

        override fun loadRecord(id: Long): Boolean {
            calls += "query:$id"
            return true
        }

        override fun executePreparedUpdates(): Int {
            calls += "prepared"
            return 3
        }

        override fun replaceInTransaction(): Boolean {
            calls += "transaction"
            return true
        }

        override fun observeExpectedConstraintFailure(): Boolean {
            calls += "failure"
            return true
        }

        override fun runSlowBackgroundQuery() {
            calls += "background"
        }

        override fun runCustomAdapterProbe() {
            calls += "custom"
        }

        override fun close() = Unit
    }
}
