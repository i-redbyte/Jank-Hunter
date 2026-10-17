package io.jankhunter.runtime

import java.util.concurrent.atomic.AtomicLongArray
import java.util.concurrent.atomic.AtomicLong

internal enum class RuntimeHookFailureReason {
    INSTRUMENTATION_HOOK,
    ASYNC_WRAPPER,
    RUNTIME_LIFECYCLE,
    COLLECTOR,
    CONTEXT,
    SCHEDULER,
    JANKSTATS_DEPENDENCY_MISSING,
    JANKSTATS_INSTALL,
    JANKSTATS_FRAME,
    JANKSTATS_CONTROL,
    UNCLASSIFIED,
    REENTRANCY,
    COLLECTOR_BREAKER,
    REGISTRY_CONTENTION,
    COUNTER_CONTENTION,
}

/** Monotonic process-lifetime evidence for failures hidden by instrumentation fail-open boundaries. */
internal object RuntimeHookFailureTracker {
    private val failures = AtomicLongArray(RuntimeHookFailureReason.entries.size)
    private val counterContention = AtomicLong()

    fun record(reason: RuntimeHookFailureReason = RuntimeHookFailureReason.UNCLASSIFIED) {
        val index = reason.ordinal
        repeat(32) {
            val current = failures.get(index)
            if (current == Long.MAX_VALUE) return
            if (failures.compareAndSet(index, current, current + 1L)) return
        }
        // Keep evidence without retrying a hot shared counter indefinitely. The original
        // category becomes unknown, and that limitation has its own serialized reason.
        counterContention.getAndIncrement()
    }

    fun count(reason: RuntimeHookFailureReason): Long {
        val value = failures.get(reason.ordinal)
        if (reason != RuntimeHookFailureReason.COUNTER_CONTENTION) return value
        val extra = counterContention.get()
        return if (extra < 0L || Long.MAX_VALUE - value < extra) Long.MAX_VALUE else value + extra
    }

    fun total(): Long {
        var total = 0L
        for (index in 0 until failures.length()) {
            val value = count(RuntimeHookFailureReason.entries[index])
            if (Long.MAX_VALUE - total < value) return Long.MAX_VALUE
            total += value
        }
        return total
    }
}
