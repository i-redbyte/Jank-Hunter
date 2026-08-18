package io.jankhunter.sample.manual

internal data class ManualControlsUiState(
    val status: String,
    val runtime: String,
    val leakCanary: String,
)

internal enum class ManualAction(val requiresActivityReference: Boolean = false) {
    RUN_CLEAN_BASELINE,
    RUN_NOISY_CANDIDATE(true),
    FLUSH_DIAGNOSTICS,
    SHARE_DIAGNOSTICS,
    ENABLE_RUNTIME,
    DISABLE_RUNTIME,
    RECORD_FLAG_PROBE,
    UI_STALL,
    BACKGROUND_WORK,
    HTTP_SUCCESS,
    HTTP_503,
    MEMORY_PRESSURE,
    LOG_SPAM,
    CUSTOM_METRICS,
    OPEN_CUSTOM_VIEW_LAB,
    OPEN_COMPOSE_LAB,
    CLEAN_OBJECT,
    ACTIVITY_REFERENCE(true),
    VIEW_BINDING(true),
    LISTENER_CALLBACK,
    CACHE_ENTRIES,
    CLEAR_RETAINED,
    BOTH_CLEAN_OBJECT,
    BOTH_RETAINED_OBJECT(true),
    BOTH_CACHE_BURST,
    HOW_TO_COMPARE,
    CANDIDATE_LEAK_BURST(true),
    CANDIDATE_PERF_BURST,
    PULL_REPORT_HINT,
}

internal sealed interface ManualControlsEffect {
    data object ShareDiagnostics : ManualControlsEffect
    data object OpenCustomViewLab : ManualControlsEffect
    data object OpenComposeLab : ManualControlsEffect
}

internal sealed interface ManualStateUpdate {
    data class Status(val value: String) : ManualStateUpdate
    data class Runtime(val value: String, val reason: String) : ManualStateUpdate
    data class LeakCanary(val value: String) : ManualStateUpdate
    data object ShareDiagnostics : ManualStateUpdate
    data object OpenCustomViewLab : ManualStateUpdate
    data object OpenComposeLab : ManualStateUpdate
}

internal fun interface ManualStateSink {
    fun emit(update: ManualStateUpdate)
}

internal typealias ManualActionHandler = (Any?) -> Unit

internal interface ManualScenarioRunner {
    fun initialize()
    fun execute(action: ManualAction, activityReference: Any?)
    fun close()
}
