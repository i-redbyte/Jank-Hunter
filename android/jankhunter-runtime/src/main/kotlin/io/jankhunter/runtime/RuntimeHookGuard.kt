package io.jankhunter.runtime

import android.os.SystemClock

internal object RuntimeHookGuard {
    fun rethrowFatal(throwable: Throwable) {
        if (throwable is VirtualMachineError || throwable is ThreadDeath) throw throwable
    }

    inline fun run(block: () -> Unit) = run(RuntimeHookFailureReason.UNCLASSIFIED, block)

    inline fun run(
        reason: RuntimeHookFailureReason,
        block: () -> Unit,
    ) {
        try {
            block()
        } catch (throwable: Throwable) {
            rethrowFatal(throwable)
            RuntimeHookFailureTracker.record(reason)
        }
    }

    inline fun <T> value(fallback: T, block: () -> T): T =
        value(fallback, RuntimeHookFailureReason.UNCLASSIFIED, block)

    inline fun <T> value(
        fallback: T,
        reason: RuntimeHookFailureReason,
        block: () -> T,
    ): T {
        return try {
            block()
        } catch (throwable: Throwable) {
            rethrowFatal(throwable)
            RuntimeHookFailureTracker.record(reason)
            fallback
        }
    }

    inline fun swallow(block: () -> Unit) = swallow(RuntimeHookFailureReason.UNCLASSIFIED, block)

    inline fun swallow(
        reason: RuntimeHookFailureReason,
        block: () -> Unit,
    ) {
        try {
            block()
        } catch (throwable: Throwable) {
            rethrowFatal(throwable)
            RuntimeHookFailureTracker.record(reason)
        }
    }

}

internal fun elapsedRealtimeSince(startMs: Long): Long {
    if (startMs <= 0L) return 0L
    return (SystemClock.elapsedRealtime() - startMs).coerceAtLeast(0L)
}
