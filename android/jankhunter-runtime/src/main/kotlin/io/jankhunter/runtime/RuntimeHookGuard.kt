package io.jankhunter.runtime

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
}
