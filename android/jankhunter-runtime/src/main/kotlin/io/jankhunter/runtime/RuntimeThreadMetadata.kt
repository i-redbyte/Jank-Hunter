package io.jankhunter.runtime

private val threadAccessorPolicies = BoundedWeakIdentityCache<Class<*>, Int>(128)

/** Reflection resolves declarations only; telemetry must never invoke a host override. */
private fun threadAccessorPolicy(thread: Thread): Int {
    val type = thread.javaClass
    if (type === Thread::class.java) return 3
    return threadAccessorPolicies.getOrPut(type) {
        RuntimeHookGuard.value(0) {
            (if (type.getMethod("getId").declaringClass === Thread::class.java) 1 else 0) or
                (if (type.getMethod("getState").declaringClass === Thread::class.java) 2 else 0)
        }
    }
}

internal fun observedThreadId(thread: Thread): Long =
    if (threadAccessorPolicy(thread) and 1 != 0) thread.id.coerceAtLeast(0L) else 0L

internal fun observedThreadState(thread: Thread): Thread.State? =
    if (threadAccessorPolicy(thread) and 2 != 0) thread.state else null
