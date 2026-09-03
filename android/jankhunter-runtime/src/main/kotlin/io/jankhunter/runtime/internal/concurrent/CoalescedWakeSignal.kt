package io.jankhunter.runtime.internal.concurrent

import java.util.concurrent.atomic.AtomicBoolean

/** Converts a producer burst into at most one expensive cross-thread wake request. */
internal class CoalescedWakeSignal {
    private val pending = AtomicBoolean()

    fun tryRequest(): Boolean {
        if (pending.get()) return false
        return pending.compareAndSet(false, true)
    }

    fun clear() {
        pending.set(false)
    }
}
