package io.jankhunter.sample

import io.jankhunter.sample.automatic.AutomaticScenarioRunner
import io.jankhunter.sample.automatic.SampleRoute
import io.jankhunter.sample.automatic.ScenarioAction
import io.jankhunter.sample.automatic.ScenarioStep
import io.jankhunter.sample.automatic.ScenarioViewModel
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ScenarioViewModelTest {
    private val runner = object : AutomaticScenarioRunner {
        override suspend fun run(
            onStage: (ScenarioStep) -> Unit,
            runMemoryStage: suspend () -> Unit,
        ) = Unit

        override fun close() = Unit
    }

    @Test
    fun opensManualRouteFromAutomaticScenario() {
        val viewModel = ScenarioViewModel(runner)

        viewModel.onAction(ScenarioAction.OPEN_MANUAL_MODE)

        assertEquals(SampleRoute.Manual, viewModel.state.value.route)
        assertEquals(SampleRoute.Manual.screenName, viewModel.currentScreenName())
    }

    @Test
    fun everyStageHasUniqueSemanticScreenName() {
        val screenNames = ScenarioStep.entries.map { it.screenName }

        assertEquals(screenNames.size, screenNames.toSet().size)
        assertTrue(screenNames.all { it.startsWith("sample.compose.") })
    }

    @Test
    fun actionsAreAvailableOnlyOnInteractiveStages() {
        assertEquals(listOf(ScenarioAction.OPEN_MANUAL_MODE), ScenarioStep.LAUNCH.actions)
        assertTrue(ScenarioStep.BASELINE.actions.isEmpty())
        assertTrue(ScenarioStep.UI_CPU.actions.isEmpty())
        assertTrue(ScenarioStep.NETWORK.actions.isEmpty())
        assertTrue(ScenarioStep.MEMORY.actions.isEmpty())
        assertEquals(
            listOf(ScenarioAction.SHARE_DIAGNOSTICS, ScenarioAction.OPEN_MANUAL_MODE),
            ScenarioStep.RESULT.actions,
        )
    }
}
