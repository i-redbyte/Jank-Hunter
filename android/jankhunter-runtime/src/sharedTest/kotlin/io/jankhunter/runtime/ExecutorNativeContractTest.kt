package io.jankhunter.runtime

import java.util.concurrent.BlockingQueue
import java.util.concurrent.Callable
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Delayed
import java.util.concurrent.FutureTask
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.ScheduledFuture
import java.util.concurrent.ScheduledThreadPoolExecutor
import java.util.concurrent.ThreadPoolExecutor
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotSame
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class ExecutorNativeContractTest {
    @Test
    fun aCustomQueueGetterIsNeverCalledThroughThePublicAdapterFactory() {
        var reads = 0
        val pool = object : ThreadPoolExecutor(0, 1, 1, TimeUnit.SECONDS, LinkedBlockingQueue()) {
            override fun getQueue(): BlockingQueue<Runnable> {
                reads++
                error("The host's queue getter must not be invoked by JH")
            }
        }
        try {
            val completed = CountDownLatch(2)
            pool.execute { completed.countDown() }
            val observed = checkNotNull(activeGraph().asyncTelemetry.wrapExecutorService(pool, "custom-queue", null))
            assertSame(pool, observed)
            observed.execute { completed.countDown() }
            assertTrue(completed.await(2, TimeUnit.SECONDS))
            assertEquals(0, reads)
        } finally { pool.shutdownNow() }
    }

    @Test
    fun scheduledFuturePreservesNativeSelfComparison() {
        val pool = ComparingScheduler()
        try {
            val observed = checkNotNull(activeGraph().asyncTelemetry.wrapScheduledExecutorService(pool, "self", null))
            assertNotSame(pool, observed)
            val future = observed.schedule({}, 1, TimeUnit.DAYS)
            try {
                assertEquals(0, pool.futures.single().compareTo(pool.futures.single()))
                assertEquals(0, future.compareTo(future))
            } finally { future.cancel(false) }
        } finally { pool.shutdownNow() }
    }

    @Test
    fun scheduledFutureDelegatesComparisonWithTheOriginalNativeCounterpart() {
        val pool = ComparingScheduler()
        try {
            val observed = checkNotNull(activeGraph().asyncTelemetry.wrapScheduledExecutorService(pool, "pair", null))
            val first = observed.schedule({}, 1, TimeUnit.DAYS)
            val second = observed.schedule({}, 1, TimeUnit.DAYS)
            try {
                assertEquals(-1, pool.futures[0].compareTo(pool.futures[1]))
                assertEquals(-1, first.compareTo(second))
                assertEquals(1, second.compareTo(first))
                assertEquals(-1, first.compareTo(pool.futures[1]))
            } finally { first.cancel(false); second.cancel(false) }
        } finally { pool.shutdownNow() }
    }

    @Test
    fun aStandardScheduledThreadPoolFutureStillComparesEqualToItself() {
        val pool = ScheduledThreadPoolExecutor(1)
        try {
            val observed = checkNotNull(activeGraph().asyncTelemetry.wrapScheduledExecutorService(pool, "standard", null))
            val future = observed.schedule({}, 1, TimeUnit.DAYS)
            try { assertEquals(0, future.compareTo(future)) } finally { future.cancel(false) }
        } finally { pool.shutdownNow() }
    }

    private fun activeGraph() = RuntimeComponentGraph({ 1L }, { 1L }).also {
        it.state.featureGate.activate(JankHunterConfig.builder().build())
    }

    private class ComparingScheduler : ScheduledThreadPoolExecutor(1) {
        val futures = ArrayList<NativeFuture>()
        override fun schedule(command: Runnable, delay: Long, unit: TimeUnit): ScheduledFuture<*> =
            NativeFuture(futures.size).also(futures::add)
    }

    private class NativeFuture(private val order: Int) : FutureTask<Int>(Callable { order }), ScheduledFuture<Int> {
        override fun getDelay(unit: TimeUnit): Long = unit.convert(1, TimeUnit.DAYS)
        override fun compareTo(other: Delayed): Int = order.compareTo((other as NativeFuture).order)
    }
}
