package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.AsyncWriterProducer
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import io.jankhunter.runtime.internal.io.LogQualityCounters
import io.jankhunter.runtime.internal.io.QualityCounterId
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeMetricsEpochFlushTest {
    @Test fun delayedOldWindowCannotFlushTheNextSession() = checkOldTask(immediate = false, blocking = false)
    @Test fun requestedOldFlushCannotFlushTheNextSession() = checkOldTask(immediate = true, blocking = false)
    @Test fun expiredCrashFlushCannotFlushTheNextSession() = checkOldTask(immediate = false, blocking = true)

    @Test
    fun failedOldSessionFlushDoesNotMarkTheNextSessionIncomplete() {
        val directory = Files.createTempDirectory("jankhunter-old-flush-quality").toFile()
        val config = JankHunterConfig.builder().metricAggregationEnabled(true).build()
        val first = AsyncLogWriterFactory().open(directory.resolve("first"), config, "first")
        val second = AsyncLogWriterFactory().open(directory.resolve("second"), config, "second")
        val dispatched = CountDownLatch(1)
        val queued = AtomicReference<() -> Unit>()
        val graph = RuntimeComponentGraph({ 1L }, { 1L }, blockingMetricDrains = {
            queued.set(it)
            dispatched.countDown()
            true
        })
        val caller = Executors.newSingleThreadExecutor()
        graph.state.config = config
        graph.state.writer = first
        graph.metrics.recordCounter("old.pending.metric", 1L)
        try {
            val flush = caller.submit { graph.session.flush() }
            assertTrue(dispatched.await(1L, TimeUnit.SECONDS))
            graph.state.writer = second
            checkNotNull(queued.getAndSet(null)).invoke()
            flush.get(1L, TimeUnit.SECONDS)
            assertEquals("old drain failure contaminated new collection quality", 0L, metricTimeouts(second))
            assertEquals(1L, metricTimeouts(first))
        } finally {
            queued.getAndSet(null)?.invoke()
            caller.shutdown()
            assertTrue(caller.awaitTermination(2L, TimeUnit.SECONDS))
            first.close(1_000L)
            second.close(1_000L)
            directory.deleteRecursively()
        }
    }

    private fun metricTimeouts(writer: AsyncLogWriter): Long {
        val field = AsyncLogWriter::class.java.getDeclaredField("quality").apply { isAccessible = true }
        return (field.get(writer) as LogQualityCounters).snapshot()
            .firstOrNull { it.counterId == QualityCounterId.METRIC_FLUSH_TIMEOUT }?.value ?: 0L
    }

    private fun checkOldTask(immediate: Boolean, blocking: Boolean) {
        val directory = Files.createTempDirectory("jankhunter-metrics-epoch").toFile()
        val config = JankHunterConfig.builder().metricAggregationEnabled(true).metricAggregationWindowMs(60_000L).build()
        val first = AsyncLogWriterFactory().open(directory.resolve("first"), config, "first")
        val second = AsyncLogWriterFactory().open(directory.resolve("second"), config, "second")
        var current = first
        val delayed = mutableListOf<() -> Unit>()
        val immediateTasks = mutableListOf<() -> Unit>()
        val blockingTasks = mutableListOf<() -> Unit>()
        val metrics = RuntimeMetricsService(
            16, { 1L }, { current }, { config }, {},
            { immediateTasks.add(it); true }, { _, task -> delayed.add(task); true },
            executeBlockingDrain = { blockingTasks.add(it); true },
        )
        try {
            metrics.configure(16, true)
            metrics.recordCounter("old.metric", 1L)
            val old = when {
                blocking -> { assertFalse(metrics.flushBlocking(0L, first)); blockingTasks.single() }
                immediate -> { assertTrue(metrics.requestFlush()); immediateTasks.single() }
                else -> delayed.single()
            }
            metrics.reset()
            metrics.configure(16, true)
            current = second
            metrics.recordCounter("new.metric", 1L)
            assertEquals(2, delayed.size)
            old()
            assertEquals("old task wrote into the new writer", 0L, accepted(second))
            metrics.recordCounter("another.new.metric", 1L)
            assertEquals("old task reset the new window's queued flag", 2, delayed.size)
            delayed.last().invoke()
            assertTrue(second.flushBlocking(1_000L))
            assertEquals(2L, accepted(second))
        } finally {
            first.close(1_000L)
            second.close(1_000L)
            directory.deleteRecursively()
        }
    }

    private fun accepted(writer: AsyncLogWriter): Long {
        val field = AsyncLogWriter::class.java.getDeclaredField("producer").apply { isAccessible = true }
        return (field.get(writer) as AsyncWriterProducer).acceptedSequence
    }
}
