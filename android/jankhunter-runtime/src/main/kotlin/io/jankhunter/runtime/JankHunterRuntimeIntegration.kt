package io.jankhunter.runtime

import android.content.Context

/**
 * Dependency-light lifecycle seam for optional Jank Hunter artifacts.
 *
 * Implementations are build-injected and discovered from generated manifest metadata. [start]
 * must return quickly and move expensive initialization to its own bounded control path.
 */
interface JankHunterRuntimeIntegration {
    val id: String

    fun start(context: Context, eventSink: JankHunterAgentEventSink)

    fun stop(timeoutMs: Long)

    fun onContextChanged(
        thread: Thread,
        screen: String?,
        owner: String?,
        flow: String?,
        step: String?,
    ) = Unit

    fun onMainThreadStall(thread: Thread, context: JankHunterContextSnapshot) = Unit
}
