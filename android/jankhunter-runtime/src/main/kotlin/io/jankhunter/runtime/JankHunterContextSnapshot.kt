package io.jankhunter.runtime

/**
 * Immutable Jank Hunter attribution captured at the start of asynchronous work.
 *
 * Integrations should treat this value as an opaque token and pass it back to the matching
 * a feature-specific telemetry API when the work completes.
 */
class JankHunterContextSnapshot internal constructor(
    val screen: String?,
    val owner: String?,
    val initiatorPresent: Boolean = false,
    val initiatorId: Long = 0L,
    val initiatorName: String? = null,
    val operationId: Long = 0L,
) {
    internal fun asRuntimeContext(): JankHunterContext {
        return JankHunterContext(screen, owner, operationId)
    }
}
