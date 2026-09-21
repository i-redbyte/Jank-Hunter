package io.jankhunter.runtime

import java.lang.ref.WeakReference
import java.util.concurrent.Callable
import java.util.concurrent.CountDownLatch
import java.util.concurrent.ExecutionException
import java.util.concurrent.Executors
import java.util.concurrent.ScheduledFuture
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class ScheduledFutureRetentionTest {
    @Test
    fun completedRunnableReleasesTaskWhileFutureIsRetained() = withExecutor { executor ->
        val retained = completePayload(executor)
        assertNoTaskState(retained.first)
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(2L)
        while (retained.second.get() != null && System.nanoTime() < deadline) {
            System.gc()
            Thread.yield()
        }
        assertNull("completed future retained runnable payload", retained.second.get())
        assertTrue(retained.first.isDone)
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

    private fun completePayload(
        executor: JankHunterScheduledExecutorService,
    ): Pair<ScheduledFuture<*>, WeakReference<ByteArray>> {
        val payload = ByteArray(1024 * 1024)
        val reference = WeakReference(payload)
        val future = executor.schedule(Runnable { check(payload.size == 1024 * 1024) }, 0L, TimeUnit.MILLISECONDS)
        future.get(2L, TimeUnit.SECONDS)
        return Pair(future, reference)
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
