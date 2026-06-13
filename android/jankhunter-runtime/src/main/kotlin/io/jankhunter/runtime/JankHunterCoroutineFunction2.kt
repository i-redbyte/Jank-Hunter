package io.jankhunter.runtime

import android.os.SystemClock
import kotlin.coroutines.Continuation
import kotlin.coroutines.CoroutineContext
import kotlin.coroutines.intrinsics.COROUTINE_SUSPENDED

@Suppress("UNCHECKED_CAST")
class JankHunterCoroutineFunction2 internal constructor(
    private val delegate: Function2<Any?, Any?, Any?>,
    private val ownerName: String?,
) : Function2<Any?, Any?, Any?> {
    override fun invoke(p1: Any?, p2: Any?): Any? {
        val start = SystemClock.elapsedRealtime()
        var completedByContinuation = false
        var failed = false
        val continuation = if (p2 is Continuation<*>) {
            completedByContinuation = true
            JankHunterContinuation(p2 as Continuation<Any?>, ownerName, start)
        } else {
            p2
        }

        try {
            val result = JankHunter.callWithOwner(ownerName) {
                delegate.invoke(p1, continuation)
            }
            if (result !== COROUTINE_SUSPENDED) {
                completedByContinuation = false
            }
            return result
        } catch (throwable: Throwable) {
            failed = true
            throw throwable
        } finally {
            if (!completedByContinuation) {
                JankHunter.recordWrappedWork(
                    ownerName,
                    "coroutine",
                    SystemClock.elapsedRealtime() - start,
                    failed,
                )
            }
        }
    }
}

private class JankHunterContinuation<T>(
    private val delegate: Continuation<T>,
    private val ownerName: String?,
    private val startedAtMs: Long,
) : Continuation<T> {
    override val context: CoroutineContext
        get() = delegate.context

    override fun resumeWith(result: Result<T>) {
        val failed = result.exceptionOrNull() != null
        try {
            JankHunter.callWithOwner(ownerName) {
                delegate.resumeWith(result)
            }
        } finally {
            JankHunter.recordWrappedWork(
                ownerName,
                "coroutine",
                SystemClock.elapsedRealtime() - startedAtMs,
                failed,
            )
        }
    }
}
