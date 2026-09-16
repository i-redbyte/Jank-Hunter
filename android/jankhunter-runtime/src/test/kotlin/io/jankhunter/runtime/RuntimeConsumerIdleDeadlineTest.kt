package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.locks.LockSupport
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeConsumerIdleDeadlineTest {
    @Test
    fun advancingEventTimeDoesNotSpendTheActiveFlushInterval() {
        val eventTime = AtomicLong(0L)
        val observed = CountDownLatch(1)
        val emitted = CountDownLatch(1)
        withGraphClock(eventTime, 60_000L, emitted, observed) { graph ->
            graph.recordEdge(1L, 2L)
            eventTime.set(120_000L)
            LockSupport.unpark(graph.consumerForTest())
            assertTrue(observed.await(1L, TimeUnit.SECONDS))
            assertFalse("elapsed event time incorrectly spent the active flush interval", emitted.await(200L, TimeUnit.MILLISECONDS))
            assertTrue(graph.flushBlocking(1_000L))
            assertEquals(1L, graph.emittedForTest())
        }
    }

    @Test
    fun activeDeadlineRotatesAPartialPageWithoutEventTimeProgressOrSignals() {
        val emitted = CountDownLatch(1)
        withGraphClock(AtomicLong(0L), 100L, emitted, CountDownLatch(1)) { graph ->
            graph.recordEdge(1L, 2L)
            assertTrue("active flush deadline did not rotate the idle partial page", emitted.await(2L, TimeUnit.SECONDS))
            assertTrue(graph.flushBlocking(1_000L))
            assertEquals(1L, graph.emittedForTest())
            assertEquals(0L, graph.acceptedEventLossForTest())
        }
    }

    private fun withGraphClock(
        eventTime: AtomicLong,
        intervalMs: Long,
        emitted: CountDownLatch,
        observed: CountDownLatch,
        action: (RuntimeCallGraph) -> Unit,
    ) {
        val directory = Files.createTempDirectory("jankhunter-graph-active-clock").toFile()
        val writer = AsyncLogWriterFactory().open(directory, JankHunterConfig.builder().autoStartCollectors(false).build(), "clock")
        val started = CountDownLatch(1)
        val graph = RuntimeCallGraph(
            nowMs = eventTime::get, captureScreen = { null }, captureOperationId = { 0L }, maxKeys = { 16 },
            periodicFlushIntervalMs = intervalMs, batchObserver = { emitted.countDown() },
            consumerLoopObserver = {
                started.countDown()
                if (eventTime.get() > 0L) observed.countDown()
            },
        )
        graph.resetFlushState(writer)
        try {
            assertTrue(started.await(1L, TimeUnit.SECONDS))
            action(graph)
        } finally {
            assertTrue(graph.flushForShutdown(1_000L))
            graph.clear()
            assertTrue(writer.close(1_000L))
            directory.deleteRecursively()
        }
    }

    @Test
    fun hookConsumerDoesNotPollBeforeItsFlushDeadline() = withConsumers { hooks, _, loops ->
        hooks.start(loops.writer)
        assertTrue(loops.started.await(1L, TimeUnit.SECONDS))
        loops.release.countDown()
        // Permit a few spurious wakes, but reject a repeating 50ms poll before the 5s deadline.
        assertFalse("idle hook consumer kept polling", loops.repeated.await(300L, TimeUnit.MILLISECONDS))
    }

    @Test
    fun graphConsumerDoesNotPollBeforeItsFlushDeadline() = withConsumers { _, graph, loops ->
        graph.resetFlushState(loops.writer)
        assertTrue(loops.started.await(1L, TimeUnit.SECONDS))
        loops.release.countDown()
        assertFalse("idle graph consumer kept polling", loops.repeated.await(300L, TimeUnit.MILLISECONDS))
    }

    @Test
    fun hookPublicationBeforeWakeResetIsDrainedByExplicitFlush() = withConsumers { hooks, _, loops ->
        hooks.start(loops.writer)
        assertTrue(loops.started.await(1L, TimeUnit.SECONDS))
        assertTrue(hooks.recordMethod(7L, "before.wait"))
        loops.release.countDown()
        assertTrue(hooks.flushBlocking(1_000L))
        assertEquals(1L, hooks.acceptedForTest())
        assertEquals(1L, hooks.emittedForTest())
        assertEquals(0L, hooks.acceptedLossForTest())
    }

    @Test
    fun partialGraphPageBeforeWakeResetIsDrainedByExplicitFlush() = withConsumers { _, graph, loops ->
        graph.resetFlushState(loops.writer)
        assertTrue(loops.started.await(1L, TimeUnit.SECONDS))
        graph.recordEdge(1L, 2L)
        loops.release.countDown()
        assertTrue(graph.flushBlocking(1_000L))
        assertEquals(1L, graph.acceptedForTest())
        assertEquals(1L, graph.emittedForTest())
        assertEquals(0L, graph.acceptedEventLossForTest())
    }

    @Test
    fun hookPeriodicDeadlineEmitsAnIdleAggregateWithoutAnotherSignal() = withConsumers { hooks, _, loops ->
        hooks.start(loops.writer)
        assertTrue(loops.started.await(1L, TimeUnit.SECONDS))
        assertTrue(hooks.recordMethod(7L, "periodic.flush"))
        loops.release.countDown()
        awaitCondition("periodic hook flush did not emit its pending aggregate", 7_000L) {
            hooks.emittedForTest() == 1L
        }
        assertEquals(0L, hooks.acceptedLossForTest())
    }

    @Test
    fun parkedHookConsumerWakesForFirstPublicationAndStop() = withConsumers { hooks, _, loops ->
        hooks.start(loops.writer)
        assertTrue(loops.started.await(1L, TimeUnit.SECONDS))
        loops.release.countDown()
        val worker = checkNotNull(hooks.consumerForTest())
        awaitCondition("hook consumer did not park", 1_000L) { worker.state == Thread.State.TIMED_WAITING }
        assertTrue(hooks.recordMethod(7L, "after.wait"))
        assertTrue(hooks.flushBlocking(1_000L))
        assertEquals(1L, hooks.emittedForTest())
        assertTrue(hooks.stopAndFlush(1_000L))
        assertFalse(worker.isAlive)
    }

    private fun awaitCondition(message: String, timeoutMs: Long, condition: () -> Boolean) {
        val deadline = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(timeoutMs)
        while (!condition() && System.nanoTime() < deadline) LockSupport.parkNanos(1_000_000L)
        assertTrue(message, condition())
    }

    private fun withConsumers(action: (RuntimeHookEventTransport, RuntimeCallGraph, Loops) -> Unit) {
        val directory = Files.createTempDirectory("jankhunter-consumer-idle").toFile()
        val writer = AsyncLogWriterFactory().open(
            directory, JankHunterConfig.builder().autoStartCollectors(false).flushIntervalMs(60_000L).build(), "idle",
        )
        val loops = Loops(writer)
        val hooks = RuntimeHookEventTransport({ 16 }, { 16 }, consumerLoopObserver = loops::observe)
        val graph = RuntimeCallGraph(
            nowMs = { TimeUnit.NANOSECONDS.toMillis(System.nanoTime()) }, captureScreen = { null },
            captureOperationId = { 0L }, maxKeys = { 16 }, consumerLoopObserver = loops::observe,
        )
        try {
            action(hooks, graph, loops)
        } finally {
            loops.release.countDown()
            assertTrue(hooks.stopAndFlush(1_000L))
            hooks.clear()
            assertTrue(graph.flushForShutdown(1_000L))
            graph.clear()
            assertTrue(writer.close(1_000L))
            directory.deleteRecursively()
        }
    }

    private class Loops(val writer: io.jankhunter.runtime.internal.io.AsyncLogWriter) {
        val started = CountDownLatch(1)
        val release = CountDownLatch(1)
        val repeated = CountDownLatch(5)

        fun observe() {
            if (started.count > 0L) {
                started.countDown()
                check(release.await(2L, TimeUnit.SECONDS))
            } else {
                repeated.countDown()
            }
        }
    }
}
