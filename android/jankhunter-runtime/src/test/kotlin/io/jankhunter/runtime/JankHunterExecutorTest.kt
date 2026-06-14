package io.jankhunter.runtime

import java.util.concurrent.Callable
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executor
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotSame
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
    fun wrappersAreIdempotent() {
        val delegate = Executor { command -> command.run() }
        val wrapped = JankHunter.wrapExecutor(delegate, "db")

        assertSame(wrapped, JankHunter.wrapExecutor(wrapped, "db"))
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
    fun wrapRunnableStillWrapsPlainRunnable() {
        val runnable = Runnable {}

        val wrapped = JankHunter.wrapRunnable(runnable, "plain")

        assertNotSame(runnable, wrapped)
        assertTrue(wrapped is JankHunterRunnable)
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
}
