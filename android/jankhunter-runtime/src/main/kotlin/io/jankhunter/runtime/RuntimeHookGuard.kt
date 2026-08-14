package io.jankhunter.runtime

import android.os.SystemClock

internal object RuntimeHookGuard {
    inline fun run(block: () -> Unit) {
        try {
            block()
        } catch (throwable: Throwable) {
            if (throwable is VirtualMachineError || throwable is ThreadDeath) throw throwable
        }
    }

    inline fun <T> value(fallback: T, block: () -> T): T {
        return try {
            block()
        } catch (throwable: Throwable) {
            if (throwable is VirtualMachineError || throwable is ThreadDeath) throw throwable
            fallback
        }
    }

    inline fun swallow(block: () -> Unit) {
        try {
            block()
        } catch (_: Throwable) {
        }
    }

}

internal fun elapsedRealtimeSince(startMs: Long): Long {
    if (startMs <= 0L) return 0L
    return (SystemClock.elapsedRealtime() - startMs).coerceAtLeast(0L)
}
