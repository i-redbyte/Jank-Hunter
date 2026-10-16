package io.jankhunter.runtime

/** Execution strategy selected by the host before SDK initialization. */
enum class JankHunterExecutionObservationMode {
    WRAPPERS,
    IN_PLACE,
}

/** Active strategies are fixed for the process, including across shutdown and initialization. */
data class JankHunterExecutionObservationModes(
    val tasks: JankHunterExecutionObservationMode = JankHunterExecutionObservationMode.WRAPPERS,
    val coroutines: JankHunterExecutionObservationMode = JankHunterExecutionObservationMode.WRAPPERS,
)

internal fun JankHunterConfig.forObservationModes(modes: JankHunterExecutionObservationModes): JankHunterConfig {
    if (modes.tasks == JankHunterExecutionObservationMode.WRAPPERS &&
        modes.coroutines == JankHunterExecutionObservationMode.WRAPPERS) return this
    return toBuilder().mainThreadAdmissionWaitMs(0L).backgroundAdmissionWaitMs(0L).build()
}
