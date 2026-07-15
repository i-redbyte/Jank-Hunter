package io.jankhunter.runtime

/**
 * Immutable Jank Hunter attribution captured at the start of asynchronous work.
 *
 * Integrations should treat this value as an opaque token and pass it back to the matching
 * `JankHunter.record*` overload when the work completes.
 */
class JankHunterContextSnapshot internal constructor(
    val screen: String?,
    val owner: String?,
    val flow: String?,
    val step: String?,
) {
    internal fun asRuntimeContext(): JankHunterContext {
        return JankHunterContext(screen, owner, flow, step)
    }
}
