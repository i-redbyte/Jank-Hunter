package io.jankhunter.runtime

import android.view.View
import java.util.concurrent.Callable
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executor
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterExecutorTest {
    @Test
    fun wrapExecutorRunsDelegateTask() {
        val latch = CountDownLatch(1)
        var now = 100L
        val executor = JankHunterExecutor(
            delegate = Executor { command ->
                now += 12
                command.run()
            },
            name = "image decode pool",
            ownerName = "image decode pool",
            clock = { now++ },
        )

        executor.execute {
            assertEquals("image decode pool", JankHunter.currentOwner())
            latch.countDown()
        }

        assertTrue(latch.await(1, TimeUnit.SECONDS))
    }

    @Test
    fun wrapExecutorServiceKeepsExecutorServiceContract() {
        val delegate = Executors.newSingleThreadExecutor()
        try {
            var now = 200L
            val wrapped = JankHunterExecutorService(
                delegate = delegate,
                name = "api-pool",
                ownerName = "api-pool",
                clock = { now++ },
            )
            val result = wrapped.submit<String> {
                JankHunter.currentOwner()
            }

            assertEquals("api-pool", result.get(1, TimeUnit.SECONDS))
        } finally {
            delegate.shutdownNow()
        }
    }

    @Test
    fun executorServiceShutdownNowReturnsOriginalPendingRunnable() {
        val delegate = Executors.newSingleThreadExecutor()
        try {
            val wrapped = JankHunterExecutorService(
                delegate = delegate,
                name = "api-pool",
                ownerName = "api-pool",
                clock = { 1L },
            )
            val started = CountDownLatch(1)
            val release = CountDownLatch(1)
            val blocker = Runnable {
                started.countDown()
                try {
                    release.await(5, TimeUnit.SECONDS)
                } catch (_: InterruptedException) {
                    Thread.currentThread().interrupt()
                }
            }
            val pending = Runnable {}

            wrapped.execute(blocker)
            assertTrue(started.await(1, TimeUnit.SECONDS))
            wrapped.execute(pending)

            val returned = wrapped.shutdownNow()
            release.countDown()

            assertTrue(returned.any { it === pending })
        } finally {
            delegate.shutdownNow()
        }
    }

    @Test
    fun wrapExecutorServiceKeepsScheduledExecutorServiceContract() {
        val delegate = Executors.newSingleThreadScheduledExecutor()
        try {
            var now = 300L
            val wrapped = JankHunterScheduledExecutorService(
                delegate = delegate,
                name = "scheduler",
                ownerName = "scheduler",
                clock = { now++ },
            )

            val result = wrapped.schedule(
                Callable { JankHunter.currentOwner() },
                0L,
                TimeUnit.MILLISECONDS,
            )

            assertEquals("scheduler", result.get(1, TimeUnit.SECONDS))
        } finally {
            delegate.shutdownNow()
        }
    }

    @Test
    fun scheduledExecutorWrapsPeriodicTasks() {
        val delegate = Executors.newSingleThreadScheduledExecutor()
        try {
            var now = 400L
            val wrapped = JankHunterScheduledExecutorService(
                delegate = delegate,
                name = "ticker",
                ownerName = "ticker",
                clock = { now++ },
            )
            val latch = CountDownLatch(2)
            val owners = mutableListOf<String>()

            val future = wrapped.scheduleAtFixedRate(
                {
                    owners += JankHunter.currentOwner()
                    latch.countDown()
                },
                0L,
                10L,
                TimeUnit.MILLISECONDS,
            )

            assertTrue(latch.await(1, TimeUnit.SECONDS))
            future.cancel(false)
            assertTrue(owners.all { it == "ticker" })
        } finally {
            delegate.shutdownNow()
        }
    }

    @Test
    fun scheduledExecutorClearsPeriodicTrackingWhenTaskFails() {
        val delegate = Executors.newSingleThreadScheduledExecutor()
        try {
            var now = 500L
            val wrapped = JankHunterScheduledExecutorService(
                delegate = delegate,
                name = "ticker",
                ownerName = "ticker",
                clock = { now++ },
            )
            val ran = CountDownLatch(1)

            val future = wrapped.scheduleAtFixedRate(
                {
                    ran.countDown()
                    throw IllegalStateException("boom")
                },
                0L,
                10L,
                TimeUnit.MILLISECONDS,
            )

            assertTrue(ran.await(1, TimeUnit.SECONDS))
            waitUntilDone(future)
            assertEquals(0, trackedRunnableCount(wrapped))
        } finally {
            delegate.shutdownNow()
        }
    }

    @Test
    fun publicWrappersAreNoopsWhenRuntimeInactive() {
        JankHunter.shutdown()

        val delegate = Executor { command -> command.run() }
        assertSame(delegate, JankHunter.wrapExecutor(delegate, "db"))
        val scheduled = Executors.newSingleThreadScheduledExecutor()
        try {
            assertSame(scheduled, JankHunter.wrapExecutor(scheduled, "scheduled"))
            assertSame(scheduled, JankHunter.wrapExecutorService(scheduled, "scheduled"))
            assertSame(scheduled, JankHunter.wrapScheduledExecutorService(scheduled, "scheduled"))
        } finally {
            scheduled.shutdownNow()
        }

        val runnable = Runnable {}
        assertSame(runnable, JankHunter.wrapRunnable(runnable, "plain"))
        val callable = Callable { "ok" }
        assertSame(callable, JankHunter.wrapCallable(callable, "plain"))
        val coroutineBlock: Function2<Any?, Any?, Any?> = { _, _ -> "ok" }
        assertSame(coroutineBlock, JankHunter.wrapCoroutineBlock(coroutineBlock, "plain"))
        val listener = View.OnClickListener {}
        assertSame(listener, JankHunter.wrapClickListener(listener, "plain"))
    }

    @Test
    fun wrapRunnableKeepsSpecializedRunnableType() {
        var ran = false
        val priorityRunnable = object : PriorityRunnable {
            override fun run() {
                ran = true
            }
        }

        val wrapped = JankHunter.wrapRunnable(priorityRunnable, "priority")

        assertSame(priorityRunnable, wrapped)
        PriorityRunnableQueue().offer(wrapped!!)
        assertTrue(ran)
    }

    @Test
    fun wrapRunnableReturnsOriginalWhenRuntimeInactive() {
        JankHunter.shutdown()
        val runnable = Runnable {}

        val wrapped = JankHunter.wrapRunnable(runnable, "plain")

        assertSame(runnable, wrapped)
    }

    @Test
    fun wrapCallableKeepsSpecializedCallableType() {
        val priorityCallable = object : PriorityCallable<String> {
            override fun call(): String = "ok"
        }

        val wrapped = JankHunter.wrapCallable(priorityCallable, "priority")

        assertSame(priorityCallable, wrapped)
    }

    @Test
    fun recordLogSpamIsNoopWhenRuntimeInactive() {
        JankHunter.shutdown()

        repeat(3) {
            JankHunter.recordLogSpam("BenchmarkOwner", "android.util.Log.d", 3)
        }
    }

    @Test
    fun executorMetricNameIsStable() {
        assertEquals("image_decode_pool-1", metricExecutorName("image decode/pool-1"))
        assertEquals("unknown", metricExecutorName(null))
    }

    private interface PriorityRunnable : Runnable

    private interface PriorityCallable<T> : Callable<T>

    private class PriorityRunnableQueue {
        fun offer(command: Runnable) {
            (command as PriorityRunnable).run()
        }
    }

    private fun waitUntilDone(future: java.util.concurrent.Future<*>) {
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(1)
        while (!future.isDone && System.nanoTime() < deadline) {
            Thread.sleep(10)
        }
        assertTrue("future did not complete", future.isDone)
    }

    @Suppress("UNCHECKED_CAST")
    private fun trackedRunnableCount(executor: Any): Int {
        val trackerField = executor.javaClass.getDeclaredField("tracker").apply {
            isAccessible = true
        }
        val tracker = trackerField.get(executor)
        val trackedField = tracker.javaClass.getDeclaredField("trackedRunnables").apply {
            isAccessible = true
        }
        return (trackedField.get(tracker) as Map<Any, Any>).size
    }

}
