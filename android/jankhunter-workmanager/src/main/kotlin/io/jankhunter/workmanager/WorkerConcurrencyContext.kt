package io.jankhunter.workmanager

import io.jankhunter.runtime.JankHunterCausalContext
import io.jankhunter.runtime.JankHunterWorkerRuntime
import kotlin.coroutines.AbstractCoroutineContextElement
import kotlin.coroutines.CoroutineContext
import kotlinx.coroutines.ThreadContextElement

/** Restores the exact attempt context on every resume, including dispatcher migration. */
internal class WorkerConcurrencyContext(
    private val attempt: JankHunterCausalContext,
) : ThreadContextElement<Long>, AbstractCoroutineContextElement(Key) {
    override fun updateThreadContext(context: CoroutineContext): Long =
        JankHunterWorkerRuntime.installConcurrencyContext(attempt)

    override fun restoreThreadContext(context: CoroutineContext, oldState: Long) =
        JankHunterWorkerRuntime.restoreConcurrencyContext(oldState)

    companion object Key : CoroutineContext.Key<WorkerConcurrencyContext>
}
