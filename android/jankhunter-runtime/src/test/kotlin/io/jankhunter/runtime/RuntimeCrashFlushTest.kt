package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.nio.file.Files
import io.jankhunter.runtime.internal.system.RuntimeMaintenanceScheduler
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.locks.ReentrantLock
import kotlin.concurrent.withLock
import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeCrashFlushTest {
    @Test
    fun blockedMetricLockCannotDelayTheOriginalHandlerPastTheCrashDeadline() {
        val directory = Files.createTempDirectory("jankhunter-crash-metric-lock").toFile()
        val config = JankHunterConfig.builder().metricAggregationEnabled(true).exactEventCollectionEnabled(true)
            .metricAggregationWindowMs(60_000L).build()
        val writer = AsyncLogWriterFactory().open(directory, config, "metric-lock")
        val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1L })
        graph.state.config = config
        graph.state.writer = writer
        val maintenance = RuntimeMaintenanceScheduler()
        graph.state.maintenanceScheduler = maintenance
        graph.metrics.recordCounter("pending.before.crash", 1L)
        val workers = Executors.newFixedThreadPool(2)
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val lockField = RuntimeMetricsService::class.java.getDeclaredField("flushLock").apply { isAccessible = true }
        val lock = lockField.get(graph.metrics) as ReentrantLock
        val original = IllegalStateException("original")
        var forwarded = 0
        val handler = graph.session.createCrashFlushHandler { _, failure -> assertSame(original, failure); forwarded++ }
        try {
            val owner = workers.submit {
                lock.withLock {
                    entered.countDown()
                    assertTrue(release.await(2L, TimeUnit.SECONDS))
                }
            }
            assertTrue(entered.await(1L, TimeUnit.SECONDS))
            val crash = workers.submit { handler.uncaughtException(Thread.currentThread(), original) }
            try {
                crash.get(300L, TimeUnit.MILLISECONDS)
                assertEquals(1, forwarded)
            } finally {
                release.countDown()
                owner.get(1L, TimeUnit.SECONDS)
            }
        } finally {
            release.countDown()
            workers.shutdown()
            assertTrue(workers.awaitTermination(2L, TimeUnit.SECONDS))
            maintenance.shutdown(1_000L)
            writer.close(1_000L)
            directory.deleteRecursively()
        }
    }

    @Test
    fun crashDrainsAnUnpublishedCallGraphPageBeforeForwardingTheOriginalFailure() {
        val directory = Files.createTempDirectory("jankhunter-crash-graph").toFile()
        val config = JankHunterConfig.builder().flushIntervalMs(60_000L).build()
        val writer = AsyncLogWriterFactory().open(directory, config, "crash")
        val graph = RuntimeComponentGraph(nowMs = { 10L }, nowUs = { 10_000L })
        graph.state.config = config
        graph.state.writer = writer
        graph.runtimeCallGraph.resetFlushState(writer)
        val original = IllegalStateException("application failure")
        val caller = Thread.currentThread()
        var forwarded = 0
        var emittedBeforeForwarding = -1L
        val handler = graph.session.createCrashFlushHandler { thread, failure ->
            assertSame(caller, thread)
            assertSame(original, failure)
            emittedBeforeForwarding = graph.runtimeCallGraph.emittedForTest()
            forwarded++
        }
        try {
            // The constant graph clock prevents a periodic flush; this short page remains unpublished.
            val parent = graph.runtimeCallGraph.enter(41L, "crash.Parent", true)
            val child = graph.runtimeCallGraph.enter(42L, "crash.Child", true)
            graph.runtimeCallGraph.exit(child, 42L)
            graph.runtimeCallGraph.exit(parent, 41L)
            assertEquals(1L, graph.runtimeCallGraph.acceptedForTest())
            assertEquals(0L, graph.runtimeCallGraph.emittedForTest())
            handler.uncaughtException(caller, original)
            assertEquals(1, forwarded)
            assertEquals("previous handler received the crash before the accepted graph page was drained", 1L, emittedBeforeForwarding)
        } finally {
            assertTrue(graph.runtimeCallGraph.flushForShutdown(1_000L))
            graph.runtimeCallGraph.clear()
            writer.close(1_000L)
            directory.deleteRecursively()
        }
    }
}
