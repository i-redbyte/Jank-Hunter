package io.jankhunter.runtime

import java.util.concurrent.locks.ReentrantLock

/** Exact epoch-local causal identity; contains no application object. */
class JankHunterCausalContext internal constructor(
    val epochId: Long,
    val entityKind: Int,
    val entityId: Long,
)

/** Explicit tracked-resource lease; the facade owning it must keep it until close or transfer. */
class JankHunterResourceLease internal constructor(
    val epochId: Long,
    val entityId: Long,
    val resourceKind: Int,
    internal val originKind: Int,
    internal val originId: Long,
    internal var scopeKind: Int,
    internal var scopeId: Long,
    internal var boundToScope: Boolean,
) {
    internal val lock = ReentrantLock()
    /** 0 attempting, 1 acquired, 2 failed/released. Guarded by the private diagnostic lock. */
    internal var state: Int = 0
}
