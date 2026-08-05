package io.jankhunter.sample

import io.jankhunter.sample.automatic.AutomaticScenarioRegistry
import io.jankhunter.sample.automatic.AutomaticScenarioStage
import io.jankhunter.sample.automatic.AutomaticStageContext
import io.jankhunter.sample.automatic.ScenarioStep
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class AutomaticScenarioRegistryTest {
    @Test
    fun acceptsOneStageForEveryScenarioStepInOrder() {
        val registry = AutomaticScenarioRegistry(ScenarioStep.entries.map(::FakeStage))

        assertEquals(ScenarioStep.entries, registry.stages.map(AutomaticScenarioStage::step))
    }

    @Test
    fun rejectsIncompleteOrReorderedScenarioPlans() {
        val stages = ScenarioStep.entries.dropLast(1).map(::FakeStage)

        assertThrows(IllegalArgumentException::class.java) {
            AutomaticScenarioRegistry(stages)
        }
    }

    @Test
    fun closesStagesInReverseOrderOnlyOnce() {
        val closeOrder = mutableListOf<ScenarioStep>()
        val registry = AutomaticScenarioRegistry(
            ScenarioStep.entries.map { step -> FakeStage(step) { closeOrder += step } },
        )

        registry.close()
        registry.close()

        assertEquals(ScenarioStep.entries.reversed(), closeOrder)
    }

    private class FakeStage(
        override val step: ScenarioStep,
        private val onClose: () -> Unit = {},
    ) : AutomaticScenarioStage {
        override suspend fun execute(context: AutomaticStageContext) = Unit

        override fun close() {
            onClose()
        }
    }
}
