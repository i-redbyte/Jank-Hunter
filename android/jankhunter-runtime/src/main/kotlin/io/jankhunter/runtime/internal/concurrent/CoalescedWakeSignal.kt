package io.jankhunter.runtime.internal.concurrent

import java.util.concurrent.atomic.AtomicBoolean

/** Converts a producer burst into at most one expensive cross-thread wake request. */
internal class CoalescedWakeSignal {
    private val pending = AtomicBoolean()

    fun tryRequest(): Boolean {
        // Even a coalesced request publishes preceding queue writes. A read-only fast path
        // can observe an old signal while the consumer misses a new release publication.
        return !pending.getAndSet(true)
    }

    fun clear() {
        // Acquire publications covered by the old signal before inspecting the queues.
        // A producer ordered after this exchange observes false and requests a new wake.
        pending.getAndSet(false)
    }

    // Internal waits may consume unpark's permit without servicing the coalesced publication.
    fun isPending(): Boolean = pending.get()
}
