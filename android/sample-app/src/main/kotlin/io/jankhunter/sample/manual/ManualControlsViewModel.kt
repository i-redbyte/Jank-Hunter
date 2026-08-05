package io.jankhunter.sample.manual

import androidx.lifecycle.ViewModel
import io.jankhunter.sample.R
import io.jankhunter.sample.SampleApplication
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.receiveAsFlow

internal class ManualControlsViewModel(
    initialState: ManualControlsUiState,
    runnerFactory: (ManualStateSink) -> ManualScenarioRunner,
) : ViewModel() {
    constructor(application: SampleApplication) : this(
        ManualControlsUiState(
            status = application.getString(R.string.status_ready),
            runtime = "",
            leakCanary = "",
        ),
        { stateSink -> JankHunterManualScenarioRunner(application, stateSink) },
    )

    private val mutableState = MutableStateFlow(initialState)
    private val effectChannel = Channel<ManualControlsEffect>(Channel.BUFFERED)
    private val runner = runnerFactory(ManualStateSink(::reduce))

    val state: StateFlow<ManualControlsUiState> = mutableState.asStateFlow()
    val effects = effectChannel.receiveAsFlow()

    init {
        runner.initialize()
    }

    fun onAction(action: ManualAction, activityReference: Any? = null) {
        runner.execute(action, activityReference)
    }

    override fun onCleared() {
        runner.close()
    }

    private fun reduce(update: ManualStateUpdate) {
        mutableState.value = when (update) {
            is ManualStateUpdate.Status -> mutableState.value.copy(status = update.value)
            is ManualStateUpdate.Runtime -> mutableState.value.copy(
                status = update.reason,
                runtime = update.value,
            )
            is ManualStateUpdate.LeakCanary -> mutableState.value.copy(leakCanary = update.value)
            ManualStateUpdate.ShareDiagnostics -> {
                effectChannel.trySend(ManualControlsEffect.ShareDiagnostics)
                mutableState.value
            }
        }
    }
}
