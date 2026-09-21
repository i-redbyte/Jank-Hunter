package io.jankhunter.runtime.internal.io

internal fun validateStallLifecycle(incidentId: Long, state: Long) {
    require(incidentId >= 0L) { "Stall incident ID must be nonnegative" }
    require(state in Jhlog.STALL_STATE_UNKNOWN..Jhlog.STALL_STATE_INTERRUPTED) { "Unknown stall state: $state" }
    require(incidentId != 0L || state == Jhlog.STALL_STATE_UNKNOWN || state == Jhlog.STALL_STATE_RECOVERED) {
        "An unfinished stall requires an incident ID"
    }
}
