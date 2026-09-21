package io.jankhunter.sample.automatic

import io.jankhunter.runtime.JankHunterTelemetry

import io.jankhunter.sample.SampleApplication
import io.jankhunter.sample.graph.ScenarioGraphComponent
import io.jankhunter.runtime.JankHunter
import java.io.Closeable
import kotlinx.coroutines.delay

internal data class AutomaticStageContext(
    val application: SampleApplication,
    val runMemoryStage: suspend () -> Unit,
)

internal interface AutomaticScenarioStage : Closeable {
    val step: ScenarioStep

    suspend fun execute(context: AutomaticStageContext)

    override fun close() = Unit
}

internal class AutomaticScenarioRegistry(
    val stages: List<AutomaticScenarioStage>,
) : Closeable {
    private var closed = false

    init {
        require(stages.map(AutomaticScenarioStage::step) == ScenarioStep.entries)
    }

    override fun close() {
        if (closed) return
        closed = true
        stages.asReversed().forEach(AutomaticScenarioStage::close)
    }

    companion object {
        fun create(application: SampleApplication): AutomaticScenarioRegistry {
            val graph = ScenarioGraphComponent(application)
            return AutomaticScenarioRegistry(
                listOf(
                    LaunchAutomaticScenarioStage(),
                    AutomaticBaselineScenario(graph.baseline),
                    AutomaticUiCpuScenario(graph.performance),
                    AutomaticNetworkScenario(graph.network),
                    AutomaticDatabaseScenario(graph.database),
                    MemoryAutomaticScenarioStage(),
                    ResultAutomaticScenarioStage(),
                ),
            )
        }
    }
}

private class LaunchAutomaticScenarioStage : AutomaticScenarioStage {
    override val step = ScenarioStep.LAUNCH

    override suspend fun execute(context: AutomaticStageContext) {
        context.application.resetScenario()
        JankHunterTelemetry.traceOperation("sample.auto.launch.prepare") {
            JankHunterTelemetry.counter("sample.auto.run.started.count", 1)
            JankHunterTelemetry.gauge("sample.auto.expected_stage_count", ScenarioStep.entries.lastIndex.toLong())
        }
        delay(LAUNCH_DURATION_MS)
    }

    private companion object {
        const val LAUNCH_DURATION_MS = 3_500L
    }
}

private class MemoryAutomaticScenarioStage : AutomaticScenarioStage {
    override val step = ScenarioStep.MEMORY

    override suspend fun execute(context: AutomaticStageContext) {
        context.runMemoryStage()
    }
}

private class ResultAutomaticScenarioStage : AutomaticScenarioStage {
    override val step = ScenarioStep.RESULT

    override suspend fun execute(context: AutomaticStageContext) {
        JankHunterTelemetry.traceOperation("sample.auto.result.scenario_completed") {
            JankHunterTelemetry.counter("sample.auto.run.completed.count", 1)
            JankHunterTelemetry.gauge("sample.auto.actual_stage_count", ScenarioStep.entries.lastIndex.toLong())
        }
        JankHunter.flush()
    }
}
