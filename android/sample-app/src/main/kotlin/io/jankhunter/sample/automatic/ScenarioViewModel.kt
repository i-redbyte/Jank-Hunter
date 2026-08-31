package io.jankhunter.sample.automatic

import io.jankhunter.runtime.JankHunterTelemetry

import androidx.annotation.StringRes
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import io.jankhunter.sample.R
import io.jankhunter.sample.SampleApplication
import io.jankhunter.runtime.JankHunter
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.launch

internal sealed interface SampleRoute {
    val screenName: String

    data class Automatic(val step: ScenarioStep) : SampleRoute {
        override val screenName: String = step.screenName
    }

    data object Manual : SampleRoute {
        override val screenName: String = "sample.compose.manual"
    }
}

internal data class ScenarioUiState(
    val route: SampleRoute = SampleRoute.Automatic(ScenarioStep.LAUNCH),
)

internal enum class ScenarioAction(@param:StringRes val labelRes: Int) {
    OPEN_MANUAL_MODE(R.string.action_open_manual_mode),
    SHARE_DIAGNOSTICS(R.string.action_share_diagnostics),
}

internal sealed interface ScenarioEffect {
    data object OpenMemoryStage : ScenarioEffect
    data object ShareDiagnostics : ScenarioEffect
}

internal class ScenarioViewModel(
    private val runner: AutomaticScenarioRunner,
) : ViewModel() {
    constructor(application: SampleApplication) : this(JankHunterAutomaticScenarioRunner(application))

    private val mutableState = MutableStateFlow(ScenarioUiState())
    private val effectChannel = Channel<ScenarioEffect>(Channel.BUFFERED)
    private var runJob: Job? = null
    private var memoryCompletion: CompletableDeferred<Unit>? = null

    val state: StateFlow<ScenarioUiState> = mutableState.asStateFlow()
    val effects = effectChannel.receiveAsFlow()

    fun start() {
        if (runJob != null) return
        runJob = viewModelScope.launch {
            runner.run(
                onStage = ::showStage,
                runMemoryStage = ::runMemoryStage,
            )
        }
    }

    fun onAction(action: ScenarioAction) {
        when (action) {
            ScenarioAction.OPEN_MANUAL_MODE -> showManualMode()
            ScenarioAction.SHARE_DIAGNOSTICS -> effectChannel.trySend(ScenarioEffect.ShareDiagnostics)
        }
    }

    fun onMemoryStageCompleted() {
        memoryCompletion?.complete(Unit)
    }

    fun currentScreenName(): String {
        return mutableState.value.route.screenName
    }

    override fun onCleared() {
        runner.close()
    }

    private fun showManualMode() {
        runJob?.cancel()
        mutableState.value = ScenarioUiState(SampleRoute.Manual)
        JankHunterTelemetry.setScreen(SampleRoute.Manual.screenName)
    }

    private fun showStage(step: ScenarioStep) {
        JankHunterTelemetry.setScreen(step.screenName)
        mutableState.value = ScenarioUiState(SampleRoute.Automatic(step))
    }

    private suspend fun runMemoryStage() {
        val completion = CompletableDeferred<Unit>()
        memoryCompletion = completion
        effectChannel.send(ScenarioEffect.OpenMemoryStage)
        completion.await()
        memoryCompletion = null
    }
}
