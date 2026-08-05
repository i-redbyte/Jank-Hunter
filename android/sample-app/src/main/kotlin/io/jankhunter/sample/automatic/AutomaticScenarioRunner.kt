package io.jankhunter.sample.automatic

import io.jankhunter.sample.SampleApplication
import io.jankhunter.runtime.JankHunter

internal interface AutomaticScenarioRunner {
    suspend fun run(
        onStage: (ScenarioStep) -> Unit,
        runMemoryStage: suspend () -> Unit,
    )

    fun close()
}

internal fun completeAutomaticStage(step: ScenarioStep) {
    JankHunter.recordCounter("sample.auto.stage.${step.screenName.substringAfterLast('.')}.completed.count", 1)
    JankHunter.flush()
}

internal class JankHunterAutomaticScenarioRunner(
    private val application: SampleApplication,
    private val registry: AutomaticScenarioRegistry = AutomaticScenarioRegistry.create(application),
) : AutomaticScenarioRunner {
    override suspend fun run(
        onStage: (ScenarioStep) -> Unit,
        runMemoryStage: suspend () -> Unit,
    ) {
        try {
            val context = AutomaticStageContext(application, runMemoryStage)
            registry.stages.forEach { stage ->
                onStage(stage.step)
                stage.execute(context)
            }
        } finally {
            close()
        }
    }

    override fun close() {
        registry.close()
    }
}
