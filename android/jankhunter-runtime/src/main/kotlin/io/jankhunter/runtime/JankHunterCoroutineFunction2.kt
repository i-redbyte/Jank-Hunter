package io.jankhunter.runtime

import android.os.SystemClock
import java.util.concurrent.atomic.AtomicIntegerFieldUpdater
import kotlin.coroutines.Continuation
import kotlin.coroutines.CoroutineContext
import kotlin.coroutines.intrinsics.COROUTINE_SUSPENDED
import kotlin.coroutines.jvm.internal.CoroutineStackFrame

@Suppress("UNCHECKED_CAST")
internal class JankHunterCoroutineFunction2 internal constructor(
    private val delegate: Function2<Any?, Any?, Any?>,
    private val ownerName: String?,
    private val callbacks: RuntimeAsyncCallbacks,
    private val causal: RuntimeCausalCoroutineTelemetry? = null,
    private val causalLaunch: RuntimeCausalCoroutineLaunch? = null,
) : Function2<Any?, Any?, Any?> {
    private val capturedContext = callbacks.captureContext(ownerName)

    override fun invoke(p1: Any?, p2: Any?): Any? {
        val telemetry = causal
        return if (telemetry == null) invokeDelegate(p1, p2, null)
            else telemetry.invoke(causalLaunch) { invocation -> invokeDelegate(p1, p2, invocation) }
    }

    /** A builder submission owns a fresh identity even when the supplied block is already decorated. */
    fun causalPlacement(
        owner: String?, callbacks: RuntimeAsyncCallbacks,
        telemetry: RuntimeCausalCoroutineTelemetry?, launch: RuntimeCausalCoroutineLaunch,
    ): JankHunterCoroutineFunction2 = JankHunterCoroutineFunction2(delegate, owner, callbacks, telemetry, launch)

    fun launchIdentity(): RuntimeCausalCoroutineLaunch? = causalLaunch

    private fun invokeDelegate(p1: Any?, p2: Any?, launch: RuntimeCausalCoroutineLaunch?): Any? {
        if (!callbacks.isActive()) return delegate.invoke(p1, p2)
        val start = RuntimeHookGuard.value(0L, RuntimeHookFailureReason.ASYNC_WRAPPER) { SystemClock.elapsedRealtime() }
        var completedByContinuation = false
        var failed = false
        var cancelled = false
        var wrappedContinuation: JankHunterContinuation<Any?>? = null
        val continuation = if (p2 is Continuation<*>) {
            val wrapped = RuntimeHookGuard.value<JankHunterContinuation<Any?>?>(
                null,
                RuntimeHookFailureReason.ASYNC_WRAPPER,
            ) {
                JankHunterContinuation(p2 as Continuation<Any?>, ownerName, capturedContext, start, callbacks, causal, launch)
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
                causal?.expectRoot(launch, delegate, continuation)
                delegate.invoke(p1, continuation)
            }
            if (result !== COROUTINE_SUSPENDED) {
                completedByContinuation = false
            }
            return result
        } catch (throwable: Throwable) {
            failed = true
            cancelled = throwable is java.util.concurrent.CancellationException
            completedByContinuation = false
            throw throwable
        } finally {
            if (!completedByContinuation) {
                wrappedContinuation?.recordCompletion(failed, cancelled) ?: recordCompletion(start, failed, cancelled, launch)
            }
        }
    }

    private fun recordCompletion(startedAtMs: Long, failed: Boolean, cancelled: Boolean, launch: RuntimeCausalCoroutineLaunch?) {
        RuntimeHookGuard.run(RuntimeHookFailureReason.ASYNC_WRAPPER) {
            val durationMs = if (startedAtMs > 0L) {
                (SystemClock.elapsedRealtime() - startedAtMs).coerceAtLeast(0L)
            } else {
                0L
            }
            callbacks.recordWrappedWork(ownerName, "coroutine", durationMs, failed)
            causal?.complete(launch, coroutineCompletionOutcome(failed, cancelled))
        }
    }
}

private class JankHunterContinuation<T>(
    private val delegate: Continuation<T>,
    private val ownerName: String?,
    private val capturedContext: JankHunterContext,
    private val startedAtMs: Long,
    private val callbacks: RuntimeAsyncCallbacks,
    private val causal: RuntimeCausalCoroutineTelemetry?,
    private val causalLaunch: RuntimeCausalCoroutineLaunch?,
) : Continuation<T>, CoroutineStackFrame {
    @JvmSynthetic
    @JvmField
    @Volatile
    internal var completionState = COMPLETION_PENDING

    override val context: CoroutineContext
        get() = delegate.context

    override val callerFrame: CoroutineStackFrame?
        get() = delegate as? CoroutineStackFrame

    override fun getStackTraceElement(): StackTraceElement? = null

    override fun resumeWith(result: Result<T>) {
        if (!callbacks.isActive()) {
            delegate.resumeWith(result)
            return
        }
        var failed = result.exceptionOrNull() != null
        var cancelled = result.exceptionOrNull() is java.util.concurrent.CancellationException
        try {
            callbacks.callWithContext(capturedContext, ownerName) {
                delegate.resumeWith(result)
            }
        } catch (throwable: Throwable) {
            failed = true
            cancelled = throwable is java.util.concurrent.CancellationException
            throw throwable
        } finally {
            recordCompletion(failed, cancelled)
        }
    }

    fun recordCompletion(failed: Boolean, cancelled: Boolean = false) {
        if (!COMPLETION.compareAndSet(this, COMPLETION_PENDING, COMPLETION_RECORDED)) return
        RuntimeHookGuard.run(RuntimeHookFailureReason.ASYNC_WRAPPER) {
            callbacks.recordWrappedWork(ownerName, "coroutine", elapsedRealtimeSince(startedAtMs), failed)
            causal?.complete(causalLaunch, coroutineCompletionOutcome(failed, cancelled))
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

private fun coroutineCompletionOutcome(failed: Boolean, cancelled: Boolean): CoroutineExecutionOutcome = when {
    cancelled -> CoroutineExecutionOutcome.CANCELLED
    failed -> CoroutineExecutionOutcome.FAILURE
    else -> CoroutineExecutionOutcome.SUCCESS
}
