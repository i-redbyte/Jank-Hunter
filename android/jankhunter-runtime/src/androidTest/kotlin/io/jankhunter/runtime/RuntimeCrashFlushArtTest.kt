package io.jankhunter.runtime

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.system.RuntimeMaintenanceScheduler
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.locks.ReentrantLock
import kotlin.concurrent.withLock
import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class RuntimeCrashFlushArtTest {
    @Test
    fun mainCrashPersistsPendingMetricsHooksAndPartialGraphBeforeForwarding() = withRuntime("complete") { graph, directory ->
        val writer = checkNotNull(graph.state.writer)
        graph.runtimeHookEvents.start(writer)
        graph.runtimeCallGraph.resetFlushState(writer)
        assertTrue(graph.metrics.flushBlocking(1_000L))
        graph.metrics.recordCounter("app.crash.pending.metric", 7L)
        assertTrue(graph.runtimeHookEvents.recordMethod(51L, "app.crash.PendingMethod"))
        val parent = graph.runtimeCallGraph.enter(41L, "app.crash.Parent", true)
        val child = graph.runtimeCallGraph.enter(42L, "app.crash.Child", true)
        graph.runtimeCallGraph.exit(child, 42L)
        graph.runtimeCallGraph.exit(parent, 41L)
        assertEquals(1L, graph.runtimeCallGraph.acceptedForTest())
        assertEquals(0L, graph.runtimeCallGraph.emittedForTest())
        val original = IllegalStateException("application crash")
        var forwarded = 0
        val handler = graph.session.createCrashFlushHandler { thread, failure ->
            assertSame(Thread.currentThread(), thread)
            assertSame(original, failure)
            assertEquals(1L, graph.runtimeCallGraph.emittedForTest())
            copyLog(directory, "complete-before-forwarding")
            forwarded++
        }
        InstrumentationRegistry.getInstrumentation().runOnMainSync { handler.uncaughtException(Thread.currentThread(), original) }
        assertEquals(1, forwarded)
        assertTrue(graph.runtimeHookEvents.stopAndFlush(1_000L))
        assertTrue(graph.runtimeCallGraph.flushForShutdown(1_000L))
        assertTrue(writer.close(1_000L))
        copyLog(directory, "complete-closed")
    }

    @Test
    fun contendedCrashDrainReturnsOnDeadlineAndPersistsExplicitIncompleteStages() = withRuntime("incomplete") { graph, directory ->
        val writer = checkNotNull(graph.state.writer)
        graph.metrics.recordCounter("app.crash.unsaved.metric", 7L)
        val field = RuntimeMetricsService::class.java.getDeclaredField("flushLock").apply { isAccessible = true }
        val lock = field.get(graph.metrics) as ReentrantLock
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val workers = Executors.newFixedThreadPool(2)
        val original = IllegalStateException("application crash")
        var forwarded = 0
        val handler = graph.session.createCrashFlushHandler { thread, failure ->
            assertSame(Thread.currentThread(), thread)
            assertSame(original, failure)
            forwarded++
        }
        try {
            val holder = workers.submit {
                lock.withLock { entered.countDown(); assertTrue(release.await(2L, TimeUnit.SECONDS)) }
            }
            assertTrue(entered.await(1L, TimeUnit.SECONDS))
            val started = System.nanoTime()
            workers.submit { handler.uncaughtException(Thread.currentThread(), original) }.get(300L, TimeUnit.MILLISECONDS)
            assertEquals(1, forwarded)
            File(InstrumentationRegistry.getInstrumentation().context.filesDir, "crash-incomplete-duration-ns.txt")
                .writeText((System.nanoTime() - started).toString())
            release.countDown()
            holder.get(1L, TimeUnit.SECONDS)
            assertTrue(writer.flushBlocking(1_000L))
            assertTrue(writer.close(1_000L))
            copyLog(directory, "incomplete-closed")
        } finally {
            release.countDown()
            workers.shutdown()
            assertTrue(workers.awaitTermination(2L, TimeUnit.SECONDS))
        }
    }

    private fun withRuntime(name: String, action: (RuntimeComponentGraph, File) -> Unit) {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val directory = File(instrumentation.targetContext.cacheDir, "crash-$name-${System.nanoTime()}")
        val config = JankHunterConfig.builder().autoStartCollectors(false).metricAggregationEnabled(true)
            .metricAggregationWindowMs(60_000L).flushIntervalMs(60_000L).build()
        val writer = AsyncLogWriterFactory().open(directory, config, "crash")
        val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1_000L })
        val scheduler = RuntimeMaintenanceScheduler()
        graph.state.config = config
        graph.state.writer = writer
        graph.state.maintenanceScheduler = scheduler
        graph.metrics.configure(16, true)
        try {
            writer.counter("app.bootstrap", 1L)
            assertTrue(writer.flushBlocking(1_000L))
            action(graph, directory)
        } finally {
            graph.runtimeHookEvents.stopAndFlush(1_000L)
            graph.runtimeHookEvents.clear()
            graph.runtimeCallGraph.flushForShutdown(1_000L)
            graph.runtimeCallGraph.clear()
            graph.metrics.reset()
            scheduler.shutdown(1_000L)
            writer.close(1_000L)
            directory.deleteRecursively()
        }
    }

    private fun copyLog(directory: File, name: String) {
        val log = directory.walkTopDown().single { it.isFile && it.extension == "jhlog" }
        log.copyTo(File(InstrumentationRegistry.getInstrumentation().context.filesDir, "crash-$name.jhlog"), overwrite = true)
    }
}
