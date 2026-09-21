package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.system.RuntimeMaintenanceScheduler
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.locks.ReentrantLock
import kotlin.concurrent.withLock
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeMetricsDeadlineTest {
    @Test
    fun exactFlushCannotWaitIndefinitelyForAnOlderDrain() = withBlockedDrain { metrics ->
        assertFalse(metrics.flushBlocking(25L))
    }

    @Test
    fun resettingTheSessionDoesNotWaitForAnOlderDrain() = withBlockedDrain { metrics ->
        metrics.reset()
    }

    @Test
    fun flushRequestedFromMaintenanceStillDrainsWithoutWaitingForItself() = withMetrics { metrics, scheduler, _ ->
        metrics.recordCounter("before.snapshot", 1L)
        val completed = CountDownLatch(1)
        var flushed = false
        assertTrue(scheduler.execute {
            try { flushed = metrics.flushBlocking(500L) } finally { completed.countDown() }
        })
        assertTrue(completed.await(1L, TimeUnit.SECONDS))
        assertTrue("snapshot caller on maintenance could not drain metrics", flushed)
    }

    private fun withBlockedDrain(action: (RuntimeMetricsService) -> Unit) = withMetrics { metrics, _, _ ->
        val workers = Executors.newFixedThreadPool(2)
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val field = RuntimeMetricsService::class.java.getDeclaredField("flushLock").apply { isAccessible = true }
        val lock = field.get(metrics) as ReentrantLock
        try {
            val holder = workers.submit {
                lock.withLock {
                    entered.countDown()
                    assertTrue(release.await(2L, TimeUnit.SECONDS))
                }
            }
            assertTrue(entered.await(1L, TimeUnit.SECONDS))
            try {
                workers.submit { action(metrics) }.get(250L, TimeUnit.MILLISECONDS)
            } finally {
                release.countDown()
                holder.get(1L, TimeUnit.SECONDS)
            }
        } finally {
            release.countDown()
            workers.shutdown()
            assertTrue(workers.awaitTermination(2L, TimeUnit.SECONDS))
        }
    }

    private fun withMetrics(action: (RuntimeMetricsService, RuntimeMaintenanceScheduler, AsyncLogWriter) -> Unit) {
        val directory = Files.createTempDirectory("jankhunter-metrics-deadline").toFile()
        val config = JankHunterConfig.builder().metricAggregationEnabled(true).exactEventCollectionEnabled(true)
            .metricAggregationWindowMs(60_000L).build()
        val writer = AsyncLogWriterFactory().open(directory, config, "metrics")
        val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1L })
        val scheduler = RuntimeMaintenanceScheduler()
        graph.state.config = config
        graph.state.writer = writer
        graph.state.maintenanceScheduler = scheduler
        graph.metrics.recordCounter("pending.before.drain", 1L)
        try { action(graph.metrics, scheduler, writer) } finally {
            scheduler.shutdown(1_000L)
            writer.close(1_000L)
            directory.deleteRecursively()
        }
    }
}
