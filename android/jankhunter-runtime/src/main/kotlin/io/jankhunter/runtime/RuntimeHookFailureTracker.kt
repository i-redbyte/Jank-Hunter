package io.jankhunter.runtime

import java.util.concurrent.atomic.AtomicLongArray

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
}

/** Monotonic process-lifetime evidence for failures hidden by instrumentation fail-open boundaries. */
internal object RuntimeHookFailureTracker {
    private val failures = AtomicLongArray(RuntimeHookFailureReason.entries.size)

    fun record(reason: RuntimeHookFailureReason = RuntimeHookFailureReason.UNCLASSIFIED) {
        val index = reason.ordinal
        while (true) {
            val current = failures.get(index)
            if (current == Long.MAX_VALUE) return
            if (failures.compareAndSet(index, current, current + 1L)) return
        }
    }

    fun snapshot(): LongArray = LongArray(failures.length()) { index -> failures.get(index) }

    fun total(): Long {
        var total = 0L
        for (index in 0 until failures.length()) {
            val value = failures.get(index)
            if (Long.MAX_VALUE - total < value) return Long.MAX_VALUE
            total += value
        }
        return total
    }
}
