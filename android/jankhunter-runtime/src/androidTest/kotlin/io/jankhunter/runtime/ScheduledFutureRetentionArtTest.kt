package io.jankhunter.runtime

import java.lang.ref.WeakReference
import java.lang.ref.ReferenceQueue
import java.util.concurrent.Callable
import java.util.concurrent.CountDownLatch
import java.util.concurrent.ExecutionException
import java.util.concurrent.Executors
import java.util.concurrent.Executor
import java.util.concurrent.ScheduledExecutorService
import java.util.concurrent.atomic.AtomicReference
import java.util.concurrent.ScheduledFuture
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import androidx.test.ext.junit.runners.AndroidJUnit4

@RunWith(AndroidJUnit4::class)
class ScheduledFutureRetentionArtTest {
    @Test
    fun ordinaryExecutorKeepsTheDelegatesRetrySemantics() {
        val delegate = Executor { command ->
            try {
                command.run()
            } catch (_: IllegalStateException) {
                command.run()
            }
        }
        var attempts = 0
        JankHunterExecutor(delegate, "retry", null, { 1L }, activeExecutorTestCallbacks()).execute {
            attempts++
            if (attempts == 1) error("retry this command")
        }
        assertEquals(2, attempts)
    }

    @Test
    fun completedRunnableReleasesTaskWhileFutureIsRetained() = withExecutor { executor ->
        val retained = completePayload(executor)
        assertNoTaskState(retained.future)
        assertPayloadReleased(retained)
        assertTrue(retained.future.isDone)
    }

    @Test
    fun unwrappedDelegateIsTheRetentionProbeControl() {
        val executor = Executors.newSingleThreadScheduledExecutor()
        try {
            assertPayloadReleased(completePayload(executor))
        } finally {
            executor.shutdownNow()
            assertTrue(executor.awaitTermination(2L, TimeUnit.SECONDS))
        }
    }

    @Test
    fun callableReleasesTaskButKeepsItsRequiredResult() = withExecutor { executor ->
        val result = Any()
        val future = executor.schedule(Callable { result }, 0L, TimeUnit.MILLISECONDS)
        assertSame(result, future.get(2L, TimeUnit.SECONDS))
        assertNoTaskState(future)
        assertSame(result, future.get())
    }

    @Test
    fun failedRunnableReleasesTask() = withExecutor { executor ->
        val failure = IllegalStateException("expected")
        val future = executor.schedule(Runnable { throw failure }, 0L, TimeUnit.MILLISECONDS)
        val thrown = assertThrows(ExecutionException::class.java) { future.get(2L, TimeUnit.SECONDS) }
        assertSame(failure, thrown.cause)
        assertNoTaskState(future)
    }

    @Test
    fun failedCallableReleasesTask() = withExecutor { executor ->
        val failure = IllegalStateException("expected")
        val future = executor.schedule(Callable<Any> { throw failure }, 0L, TimeUnit.MILLISECONDS)
        val thrown = assertThrows(ExecutionException::class.java) { future.get(2L, TimeUnit.SECONDS) }
        assertSame(failure, thrown.cause)
        assertNoTaskState(future)
    }

    @Test
    fun cancelledBeforeStartReleasesRunnableAndCallable() = withExecutor { executor ->
        val runnable = executor.schedule(Runnable {}, 1L, TimeUnit.DAYS)
        val callable = executor.schedule(Callable { Any() }, 1L, TimeUnit.DAYS)
        assertTrue(runnable.cancel(false))
        assertTrue(callable.cancel(false))
        assertNoTaskState(runnable)
        assertNoTaskState(callable)
    }

    @Test
    fun cancellationDuringExecutionDetachesFutureWithoutBreakingRunningTask() = withExecutor { executor ->
        val started = CountDownLatch(1)
        val release = CountDownLatch(1)
        val finished = CountDownLatch(1)
        val future = executor.schedule(Runnable {
            started.countDown()
            check(release.await(2L, TimeUnit.SECONDS))
            finished.countDown()
        }, 0L, TimeUnit.MILLISECONDS)
        try {
            assertTrue(started.await(2L, TimeUnit.SECONDS))
            assertTrue(future.cancel(false))
            assertNoTaskState(future)
        } finally {
            release.countDown()
            assertTrue(finished.await(2L, TimeUnit.SECONDS))
        }
    }

    @Test
    fun periodicTaskIsRetainedUntilCancellation() = withExecutor { executor ->
        val ranTwice = CountDownLatch(2)
        val future = executor.scheduleWithFixedDelay({ ranTwice.countDown() }, 0L, 1L, TimeUnit.MILLISECONDS)
        assertTrue(ranTwice.await(2L, TimeUnit.SECONDS))
        assertNotNull(taskState(future))
        assertTrue(future.cancel(false))
        assertNoTaskState(future)
    }

    @Test
    fun periodicFailureReleasesTask() = withExecutor { executor ->
        val future = executor.scheduleAtFixedRate(
            { throw IllegalStateException("expected") }, 0L, 1L, TimeUnit.MILLISECONDS,
        )
        assertThrows(ExecutionException::class.java) { future.get(2L, TimeUnit.SECONDS) }
        assertNoTaskState(future)
    }

    private data class PayloadProbe(
        val future: ScheduledFuture<*>,
        val reference: WeakReference<ByteArray>,
        val collected: ReferenceQueue<ByteArray>,
    )

    private fun completePayload(executor: ScheduledExecutorService): PayloadProbe {
        val result = AtomicReference<PayloadProbe>()
        val failure = AtomicReference<Throwable>()
        // End the allocation frame before requesting GC: ART may retain interpreter/JIT locals.
        val producer = Thread {
            try {
                val payload = ByteArray(1024 * 1024)
                val queue = ReferenceQueue<ByteArray>()
                val reference = WeakReference(payload, queue)
                val future = executor.schedule(Runnable { check(payload.size == 1024 * 1024) }, 0L, TimeUnit.MILLISECONDS)
                future.get(2L, TimeUnit.SECONDS)
                result.set(PayloadProbe(future, reference, queue))
            } catch (throwable: Throwable) {
                failure.set(throwable)
            }
        }
        producer.start()
        producer.join(3_000L)
        assertTrue("payload producer did not finish", !producer.isAlive)
        failure.get()?.let { throw AssertionError("payload producer failed", it) }
        return checkNotNull(result.get())
    }

    private fun assertPayloadReleased(probe: PayloadProbe) {
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(2L)
        var collected = false
        // Never call WeakReference.get before GC: the probe must not make its referent a live local.
        while (System.nanoTime() < deadline) {
            Runtime.getRuntime().gc()
            if (probe.collected.remove(50L) != null) {
                collected = true
                break
            }
        }
        assertTrue("completed future retained runnable payload", collected)
        assertNull(probe.reference.get())
        assertTrue(probe.future.isDone)
    }

    private fun assertNoTaskState(future: ScheduledFuture<*>) {
        assertNull("terminal future retains SDK task state", taskState(future))
    }

    private fun taskState(future: ScheduledFuture<*>): Any? {
        val field = future.javaClass.getDeclaredField("state")
        field.isAccessible = true
        return field.get(future)
    }

    private fun withExecutor(block: (JankHunterScheduledExecutorService) -> Unit) {
        val delegate = Executors.newSingleThreadScheduledExecutor()
        val executor = JankHunterScheduledExecutorService(
            delegate, "retention", "retention", { 1L }, activeExecutorTestCallbacks(),
        )
        try {
            block(executor)
        } finally {
            executor.shutdownNow()
            assertTrue(executor.awaitTermination(2L, TimeUnit.SECONDS))
        }
    }
}
