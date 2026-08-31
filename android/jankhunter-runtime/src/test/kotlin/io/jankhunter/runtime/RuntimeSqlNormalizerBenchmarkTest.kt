package io.jankhunter.runtime

import org.junit.Test

class RuntimeSqlNormalizerBenchmarkTest {
    @Test
    fun activeNormalizationHasBoundedLatencyAndAllocation() {
        RuntimeBenchmarkHarness.assumeEnabled()
        val iterations = iterations()
        val result = RuntimeBenchmarkHarness.measure(iterations, WARMUP_ITERATIONS) {
            RuntimeSqlNormalizer.normalize(SQL)?.length?.toLong() ?: 0L
        }

        RuntimeBenchmarkHarness.report("SQL normalize", result)
        RuntimeBenchmarkHarness.assertBudget("SQL normalize", result, 10_000.0, 1_024.0)
    }

    @Test
    fun cteOperationClassificationHasBoundedLatencyAndNoSteadyStateAllocation() {
        RuntimeBenchmarkHarness.assumeEnabled()
        val iterations = iterations()
        val result = RuntimeBenchmarkHarness.measure(iterations, WARMUP_ITERATIONS) {
            RuntimeSqlNormalizer.operation(CTE_INSERT, 1).toLong()
        }

        RuntimeBenchmarkHarness.report("SQL CTE operation", result)
        RuntimeBenchmarkHarness.assertBudget("SQL CTE operation", result, 2_000.0, 8.0)
    }

    private fun iterations(): Int = RuntimeBenchmarkHarness.iterations(MIN_ITERATIONS, DEFAULT_ITERATIONS)

    private companion object {
        const val SQL = "SELECT \"body\" FROM messages WHERE account_id=:account AND body='private' AND id IN (1,2,3)"
        const val CTE_INSERT = "WITH RECURSIVE seed(id) AS (SELECT ? UNION ALL SELECT id + ? FROM seed WHERE id < ?) " +
            "INSERT INTO messages(id) SELECT id FROM seed"
        const val MIN_ITERATIONS = 100_000
        const val DEFAULT_ITERATIONS = 200_000
        const val WARMUP_ITERATIONS = 20_000
    }
}
