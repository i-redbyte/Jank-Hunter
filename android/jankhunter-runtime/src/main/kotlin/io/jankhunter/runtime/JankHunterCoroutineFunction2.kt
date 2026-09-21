package io.jankhunter.runtime

import android.os.SystemClock
import java.util.concurrent.atomic.AtomicIntegerFieldUpdater
import kotlin.coroutines.Continuation
import kotlin.coroutines.CoroutineContext
import kotlin.coroutines.intrinsics.COROUTINE_SUSPENDED

@Suppress("UNCHECKED_CAST")
internal class JankHunterCoroutineFunction2 internal constructor(
    private val delegate: Function2<Any?, Any?, Any?>,
    private val ownerName: String?,
    private val callbacks: RuntimeAsyncCallbacks,
) : Function2<Any?, Any?, Any?> {
    private val capturedContext = callbacks.captureContext(ownerName)

    override fun invoke(p1: Any?, p2: Any?): Any? {
        if (!callbacks.isActive()) return delegate.invoke(p1, p2)
        val start = RuntimeHookGuard.value(0L, RuntimeHookFailureReason.ASYNC_WRAPPER) { SystemClock.elapsedRealtime() }
        var completedByContinuation = false
        var failed = false
        var wrappedContinuation: JankHunterContinuation<Any?>? = null
        val continuation = if (p2 is Continuation<*>) {
            val wrapped = RuntimeHookGuard.value<JankHunterContinuation<Any?>?>(
                null,
                RuntimeHookFailureReason.ASYNC_WRAPPER,
            ) {
                JankHunterContinuation(p2 as Continuation<Any?>, ownerName, capturedContext, start, callbacks)
            }
            if (wrapped == null) return delegate.invoke(p1, p2)
            wrappedContinuation = wrapped
            completedByContinuation = true
            wrapped
        } else {
            p2
        }

        try {
            val result = callbacks.callWithContext(capturedContext, ownerName) {
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
                wrappedContinuation?.recordCompletion(failed) ?: recordCompletion(start, failed)
            }
        }
    }

    private fun recordCompletion(startedAtMs: Long, failed: Boolean) {
        RuntimeHookGuard.run(RuntimeHookFailureReason.ASYNC_WRAPPER) {
            val durationMs = if (startedAtMs > 0L) {
                (SystemClock.elapsedRealtime() - startedAtMs).coerceAtLeast(0L)
            } else {
                0L
            }
            callbacks.recordWrappedWork(ownerName, "coroutine", durationMs, failed)
        }
    }
}

private class JankHunterContinuation<T>(
    private val delegate: Continuation<T>,
    private val ownerName: String?,
    private val capturedContext: JankHunterContext,
    private val startedAtMs: Long,
    private val callbacks: RuntimeAsyncCallbacks,
) : Continuation<T> {
    @JvmSynthetic
    @JvmField
    @Volatile
    internal var completionState = COMPLETION_PENDING

    override val context: CoroutineContext
        get() = delegate.context

    override fun resumeWith(result: Result<T>) {
        if (!callbacks.isActive()) {
            delegate.resumeWith(result)
            return
        }
        var failed = result.exceptionOrNull() != null
        try {
            callbacks.callWithContext(capturedContext, ownerName) {
                delegate.resumeWith(result)
            }
        } catch (throwable: Throwable) {
            failed = true
            throw throwable
        } finally {
            recordCompletion(failed)
        }
    }

    fun recordCompletion(failed: Boolean) {
        if (!COMPLETION.compareAndSet(this, COMPLETION_PENDING, COMPLETION_RECORDED)) return
        RuntimeHookGuard.run(RuntimeHookFailureReason.ASYNC_WRAPPER) {
            callbacks.recordWrappedWork(ownerName, "coroutine", elapsedRealtimeSince(startedAtMs), failed)
        }
    }

    private companion object {
        const val COMPLETION_PENDING = 0
        const val COMPLETION_RECORDED = 1

        val COMPLETION = AtomicIntegerFieldUpdater.newUpdater(
            JankHunterContinuation::class.java,
            "completionState",
        )
    }
}
