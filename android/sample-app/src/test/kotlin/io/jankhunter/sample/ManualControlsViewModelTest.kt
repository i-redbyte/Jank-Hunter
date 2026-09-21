package io.jankhunter.sample

import io.jankhunter.sample.manual.ManualAction
import io.jankhunter.sample.manual.ManualControlsEffect
import io.jankhunter.sample.manual.ManualControlsUiState
import io.jankhunter.sample.manual.ManualControlsViewModel
import io.jankhunter.sample.manual.ManualScenarioRunner
import io.jankhunter.sample.manual.ManualStateSink
import io.jankhunter.sample.manual.ManualStateUpdate
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Test

class ManualControlsViewModelTest {
    @Test
    fun reducesRunnerUpdatesIntoUiState() {
        lateinit var runner: FakeManualScenarioRunner
        val viewModel = ManualControlsViewModel(INITIAL_STATE) { stateSink ->
            FakeManualScenarioRunner(stateSink).also { runner = it }
        }

        viewModel.onAction(ManualAction.CUSTOM_METRICS)
        runner.emit(ManualStateUpdate.Runtime("enabled", "updated"))
        runner.emit(ManualStateUpdate.LeakCanary("available"))

        assertEquals(
            ManualControlsUiState(
                status = "updated",
                runtime = "enabled",
                leakCanary = "available",
            ),
            viewModel.state.value,
        )
        assertEquals(ManualAction.CUSTOM_METRICS, runner.lastAction)
    }

    @Test
    fun convertsRunnerShareUpdateIntoUiEffect() = runBlocking {
        lateinit var runner: FakeManualScenarioRunner
        val viewModel = ManualControlsViewModel(INITIAL_STATE) { stateSink ->
            FakeManualScenarioRunner(stateSink).also { runner = it }
        }
        val effect = async { viewModel.effects.first() }

        runner.emit(ManualStateUpdate.ShareDiagnostics)

        assertEquals(ManualControlsEffect.ShareDiagnostics, effect.await())
    }

    @Test
    fun convertsOpenCustomViewUpdateIntoNavigationEffect() = runBlocking {
        lateinit var runner: FakeManualScenarioRunner
        val viewModel = ManualControlsViewModel(INITIAL_STATE) { stateSink ->
            FakeManualScenarioRunner(stateSink).also { runner = it }
        }
        val effect = async { viewModel.effects.first() }

        runner.emit(ManualStateUpdate.OpenCustomViewLab)

        assertEquals(ManualControlsEffect.OpenCustomViewLab, effect.await())
    }

    @Test
    fun convertsOpenComposeUpdateIntoNavigationEffect() = runBlocking {
        lateinit var runner: FakeManualScenarioRunner
        val viewModel = ManualControlsViewModel(INITIAL_STATE) { stateSink ->
            FakeManualScenarioRunner(stateSink).also { runner = it }
        }
        val effect = async { viewModel.effects.first() }

        runner.emit(ManualStateUpdate.OpenComposeLab)

        assertEquals(ManualControlsEffect.OpenComposeLab, effect.await())
    }

    private class FakeManualScenarioRunner(
        private val stateSink: ManualStateSink,
    ) : ManualScenarioRunner {
        var lastAction: ManualAction? = null

        override fun initialize() = Unit

        override fun execute(action: ManualAction, activityReference: Any?) {
            lastAction = action
        }

        override fun close() = Unit

        fun emit(update: ManualStateUpdate) {
            stateSink.emit(update)
        }
    }

    private companion object {
        val INITIAL_STATE = ManualControlsUiState(
            status = "ready",
            runtime = "",
            leakCanary = "",
        )
    }
}
