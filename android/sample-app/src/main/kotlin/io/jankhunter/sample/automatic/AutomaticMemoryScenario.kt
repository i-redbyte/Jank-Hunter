package io.jankhunter.sample.automatic

import io.jankhunter.sample.graph.MemoryScenarioUseCase
import io.jankhunter.runtime.JankHunter
import kotlinx.coroutines.delay

internal class AutomaticMemoryScenario(
    private val graphScenario: MemoryScenarioUseCase,
) {
    suspend fun run(activityReference: Any) {
        runReleasedAllocationFlow()
        runPressureFlow()
        runRetentionFlows(activityReference)
        runLoggingFlows()
        delay(STAGE_DURATION_MS - FLUSH_ADVANCE_MS)
        completeAutomaticStage(ScenarioStep.MEMORY)
        delay(FLUSH_ADVANCE_MS)
    }

    private fun runReleasedAllocationFlow() {
        JankHunter.withFlow("sample.auto.memory.released_allocations") {
            JankHunter.markFlowStep("allocate_then_release")
            val checksum = graphScenario.allocateReleased()
            JankHunter.recordGauge("sample.auto.memory.released_checksum", checksum)
        }
    }

    private fun runPressureFlow() {
        JankHunter.withFlow("sample.auto.memory.retained_pressure") {
            JankHunter.markFlowStep("retain_heap_chunks")
            val retainedKb = graphScenario.retainPressure()
            JankHunter.recordCounter("sample.auto.memory.pressure.allocation.count", PRESSURE_ALLOCATION_COUNT.toLong())
            JankHunter.recordGauge("sample.auto.memory.pressure.retained_kb", retainedKb)
        }
    }

    private fun runRetentionFlows(activityReference: Any) {
        JankHunter.withFlow("sample.auto.retention.released") {
            JankHunter.markFlowStep("watch_collectable_object")
            graphScenario.watchReleased()
        }
        JankHunter.withFlow("sample.auto.retention.activity_reference") {
            JankHunter.markFlowStep("retain_activity_reference")
            graphScenario.retainScreen(activityReference)
        }
        JankHunter.withFlow("sample.auto.retention.cache_entries") {
            JankHunter.markFlowStep("retain_cache_entries")
            graphScenario.retainCache()
        }
    }

    private fun runLoggingFlows() {
        JankHunter.withFlow("sample.auto.logging.quiet") {
            JankHunter.markFlowStep("three_messages")
            graphScenario.recordQuietLogs()
        }
        JankHunter.withFlow("sample.auto.logging.burst") {
            JankHunter.markFlowStep("sixty_messages")
            graphScenario.recordBurstLogs()
            JankHunter.recordCounter("sample.auto.logging.expected_burst.count", NOISY_LOG_COUNT.toLong())
        }
    }

    private companion object {
        const val PRESSURE_ALLOCATION_COUNT = 4
        const val NOISY_LOG_COUNT = 60
        const val STAGE_DURATION_MS = 11_000L
        const val FLUSH_ADVANCE_MS = 400L
    }
}
