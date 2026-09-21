package io.jankhunter.runtime

/**
 * Immutable Jank Hunter attribution captured at the start of asynchronous work.
 *
 * Integrations should treat this value as an opaque token and pass it back to the matching
 * a feature-specific telemetry API when the work completes.
 */
class JankHunterContextSnapshot {
    val screen: String?
    val owner: String?
    val initiatorPresent: Boolean
    val initiatorId: Long
    val initiatorName: String?
    val operationId: Long
    internal val collectionEpochId: Long
    internal val httpToken: Long

    internal constructor(
        screen: String?, owner: String?, initiatorPresent: Boolean = false, initiatorId: Long = 0L,
        initiatorName: String? = null, operationId: Long = 0L,
    ) : this(screen, owner, initiatorPresent, initiatorId, initiatorName, operationId, 0L, 0L)

    internal constructor(
        screen: String?, owner: String?, initiatorPresent: Boolean, initiatorId: Long,
        initiatorName: String?, operationId: Long, collectionEpochId: Long, httpToken: Long,
    ) {
        this.screen = screen
        this.owner = owner
        this.initiatorPresent = initiatorPresent
        this.initiatorId = initiatorId
        this.initiatorName = initiatorName
        this.operationId = operationId
        this.collectionEpochId = collectionEpochId
        this.httpToken = httpToken
    }

    private var httpCompleted = false

    @Synchronized
    internal fun completeHttpOnce(): Boolean {
        if (httpCompleted) return false
        httpCompleted = true
        return true
    }

    internal fun asRuntimeContext(): JankHunterContext {
        return JankHunterContext(screen, owner, operationId)
    }
}
