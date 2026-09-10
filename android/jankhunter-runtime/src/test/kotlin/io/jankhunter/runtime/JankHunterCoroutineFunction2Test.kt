package io.jankhunter.runtime

import java.lang.reflect.InvocationTargetException
import java.util.concurrent.Callable
import java.util.concurrent.Executor
import kotlin.coroutines.Continuation
import kotlin.coroutines.CoroutineContext
import kotlin.coroutines.EmptyCoroutineContext
import kotlin.coroutines.intrinsics.COROUTINE_SUSPENDED
import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Assert.assertThrows
import org.junit.Test

class JankHunterCoroutineFunction2Test {
    @Test
    fun downstreamContinuationExceptionMarksSuccessfulResultAsFailed() {
        val callbacks = RecordingAsyncCallbacks()
        val downstreamFailure = IllegalStateException("downstream")
        var wrappedContinuation: Any? = null
        val delegate: Function2<Any?, Any?, Any?> = { _, continuation ->
            wrappedContinuation = continuation
            COROUTINE_SUSPENDED
        }
        val wrapped = JankHunterCoroutineFunction2(delegate, "owner", callbacks)
        val downstream = object : Continuation<Any?> {
            override val context: CoroutineContext = EmptyCoroutineContext

            override fun resumeWith(result: Result<Any?>) {
                throw downstreamFailure
            }
        }

        assertSame(COROUTINE_SUSPENDED, wrapped.invoke(Unit, downstream))
        val actual = assertThrows(InvocationTargetException::class.java) {
            RESUME_WITH.invoke(wrappedContinuation, Unit)
        }

        assertSame(downstreamFailure, actual.cause)
        assertEquals(listOf(true), callbacks.recordedFailures)
    }

    @Test
    fun synchronousContinuationFailureRecordsOneFailedCompletion() {
        val callbacks = RecordingAsyncCallbacks()
        val coroutineFailure = IllegalArgumentException("coroutine")
        val downstreamFailure = IllegalStateException("downstream")
        val delegate: Function2<Any?, Any?, Any?> = { _, continuation ->
            check(continuation is Continuation<*>)
            continuation.resumeWith(Result.failure(coroutineFailure))
            COROUTINE_SUSPENDED
        }
        val wrapped = JankHunterCoroutineFunction2(delegate, "owner", callbacks)
        val downstream = object : Continuation<Any?> {
            override val context: CoroutineContext = EmptyCoroutineContext

            override fun resumeWith(result: Result<Any?>) {
                throw downstreamFailure
            }
        }

        val actual = assertThrows(IllegalStateException::class.java) {
            wrapped.invoke(Unit, downstream)
        }

        assertSame(downstreamFailure, actual)
        assertEquals(listOf(true), callbacks.recordedFailures)
    }

    private class RecordingAsyncCallbacks : RuntimeAsyncCallbacks {
        val recordedFailures = mutableListOf<Boolean>()

        override fun captureContext(ownerName: String?): JankHunterContext {
            return JankHunterContext(screen = null, owner = ownerName)
        }

        override fun isActive(): Boolean = true

        override fun <T> callWithContext(
            context: JankHunterContext,
            ownerName: String?,
            block: () -> T,
        ): T = block()

        override fun startOperation(name: String, kind: JankHunterOperationKind): JankHunterOperation {
            return JankHunterOperation.NONE
        }

        override fun recordWrappedWork(ownerName: String?, kind: String, durationMs: Long, failed: Boolean) {
            recordedFailures += failed
        }

        override fun recordClick(ownerName: String?, durationMs: Long, failed: Boolean) = Unit

        override fun recordExecutorWait(executorName: String, ownerName: String?, waitMs: Long) = Unit

        override fun recordExecutorSnapshot(executorName: String, executor: Executor, queued: Int) = Unit

        override fun runExecutorTask(
            executorName: String,
            ownerName: String?,
            command: Runnable,
            clock: RuntimeLongSource,
        ) = command.run()

        override fun <T> callExecutorTask(
            executorName: String,
            ownerName: String?,
            callable: Callable<T>,
            clock: RuntimeLongSource,
        ): T = callable.call()
    }

    private companion object {
        val RESUME_WITH = Continuation::class.java.getMethod("resumeWith", Any::class.java)
    }
}
