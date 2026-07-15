package io.jankhunter.runtime

import android.os.SystemClock
import kotlin.coroutines.Continuation
import kotlin.coroutines.CoroutineContext
import kotlin.coroutines.intrinsics.COROUTINE_SUSPENDED

@Suppress("UNCHECKED_CAST")
internal class JankHunterCoroutineFunction2 internal constructor(
    private val delegate: Function2<Any?, Any?, Any?>,
    private val ownerName: String?,
) : Function2<Any?, Any?, Any?> {
    private val capturedContext = JankHunter.captureContext(ownerOverride = ownerName)

    override fun invoke(p1: Any?, p2: Any?): Any? {
        if (!JankHunter.isRuntimeActiveForCallbacks()) return delegate.invoke(p1, p2)
        val start = RuntimeHookGuard.value(0L) { SystemClock.elapsedRealtime() }
        var completedByContinuation = false
        var failed = false
        val continuation = if (p2 is Continuation<*>) {
            val wrapped = RuntimeHookGuard.value<Any?>(p2) {
                JankHunterContinuation(p2 as Continuation<Any?>, ownerName, capturedContext, start)
            }
            if (wrapped === p2) return delegate.invoke(p1, p2)
            completedByContinuation = true
            wrapped
        } else {
            p2
        }

        try {
            val result = JankHunter.callWithContext(capturedContext, ownerName) {
                delegate.invoke(p1, continuation)
            }
            if (result !== COROUTINE_SUSPENDED) {
                completedByContinuation = false
            }
            return result
        } catch (throwable: Throwable) {
            failed = true
            completedByContinuation = false
            throw throwable
        } finally {
            if (!completedByContinuation) {
                recordCompletion(start, failed)
            }
        }
    }

    private fun recordCompletion(startedAtMs: Long, failed: Boolean) {
        RuntimeHookGuard.run {
            val durationMs = if (startedAtMs > 0L) {
                (SystemClock.elapsedRealtime() - startedAtMs).coerceAtLeast(0L)
            } else {
                0L
            }
            JankHunter.recordWrappedWork(ownerName, "coroutine", durationMs, failed)
        }
    }
}

private class JankHunterContinuation<T>(
    private val delegate: Continuation<T>,
    private val ownerName: String?,
    private val capturedContext: JankHunterContext,
    private val startedAtMs: Long,
) : Continuation<T> {
    override val context: CoroutineContext
        get() = delegate.context

    override fun resumeWith(result: Result<T>) {
        if (!JankHunter.isRuntimeActiveForCallbacks()) {
            delegate.resumeWith(result)
            return
        }
        val failed = result.exceptionOrNull() != null
        try {
            JankHunter.callWithContext(capturedContext, ownerName) {
                delegate.resumeWith(result)
            }
        } finally {
            RuntimeHookGuard.run {
                val durationMs = if (startedAtMs > 0L) {
                    (SystemClock.elapsedRealtime() - startedAtMs).coerceAtLeast(0L)
                } else {
                    0L
                }
                JankHunter.recordWrappedWork(ownerName, "coroutine", durationMs, failed)
            }
        }
    }
}
