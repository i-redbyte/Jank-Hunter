package io.jankhunter.sample.automatic

import io.jankhunter.runtime.JankHunterTelemetry

import io.jankhunter.sample.graph.MemoryScenarioUseCase
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
        JankHunterTelemetry.traceOperation("sample.auto.memory.allocate_then_release") {
            val checksum = graphScenario.allocateReleased()
            JankHunterTelemetry.gauge("sample.auto.memory.released_checksum", checksum)
        }
    }

    private fun runPressureFlow() {
        JankHunterTelemetry.traceOperation("sample.auto.memory.retain_heap_chunks") {
            val retainedKb = graphScenario.retainPressure()
            JankHunterTelemetry.counter("sample.auto.memory.pressure.allocation.count", PRESSURE_ALLOCATION_COUNT.toLong())
            JankHunterTelemetry.gauge("sample.auto.memory.pressure.retained_kb", retainedKb)
        }
    }

    private fun runRetentionFlows(activityReference: Any) {
        JankHunterTelemetry.traceOperation("sample.auto.retention.watch_collectable_object") {
            graphScenario.watchReleased()
        }
        JankHunterTelemetry.traceOperation("sample.auto.retention.retain_activity_reference") {
            graphScenario.retainScreen(activityReference)
        }
        JankHunterTelemetry.traceOperation("sample.auto.retention.retain_cache_entries") {
            graphScenario.retainCache()
        }
    }

    private fun runLoggingFlows() {
        JankHunterTelemetry.traceOperation("sample.auto.logging.three_messages") {
            graphScenario.recordQuietLogs()
        }
        JankHunterTelemetry.traceOperation("sample.auto.logging.sixty_messages") {
            graphScenario.recordBurstLogs()
            JankHunterTelemetry.counter("sample.auto.logging.expected_burst.count", NOISY_LOG_COUNT.toLong())
        }
    }

    private companion object {
        const val PRESSURE_ALLOCATION_COUNT = 4
        const val NOISY_LOG_COUNT = 60
        const val STAGE_DURATION_MS = 11_000L
        const val FLUSH_ADVANCE_MS = 400L
    }
}
