package io.jankhunter.sample.automatic

import io.jankhunter.runtime.JankHunterTelemetry

import io.jankhunter.sample.graph.DatabaseScenarioUseCase
import kotlinx.coroutines.delay

internal class AutomaticDatabaseScenario(
    private val graphScenario: DatabaseScenarioUseCase,
) : AutomaticScenarioStage {
    override val step = ScenarioStep.DATABASE

    override suspend fun execute(context: AutomaticStageContext) {
        val result = graphScenario.execute()
        JankHunterTelemetry.traceOperation("sample.auto.database.record_expected_values") {
            JankHunterTelemetry.gauge("sample.auto.database.repeated_reads", result.repeatedReads.toLong())
            JankHunterTelemetry.gauge("sample.auto.database.affected_rows", result.affectedRows.toLong())
            JankHunterTelemetry.counter("sample.auto.database.transaction.count", result.transactionCompleted.asCount())
            JankHunterTelemetry.counter("sample.auto.database.expected_failure.count", result.expectedFailureObserved.asCount())
            JankHunterTelemetry.counter("sample.auto.database.background.count", result.backgroundCompleted.asCount())
            JankHunterTelemetry.counter("sample.auto.database.custom_adapter.count", result.customAdapterCompleted.asCount())
        }
        completeAutomaticStage(step)
        delay(FLUSH_ADVANCE_MS)
    }

    override fun close() {
        graphScenario.close()
    }

    private fun Boolean.asCount(): Long = if (this) 1L else 0L

    private companion object {
        const val FLUSH_ADVANCE_MS = 250L
    }
}
