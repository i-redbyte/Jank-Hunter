package io.jankhunter.runtime

import android.os.Build
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.concurrent.SpscSlotSequencer
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class RuntimeGraphArtTest {
    @Test
    fun releaseAcquirePublicationSurvivesWraparoundOnArt() {
        val sequencer = SpscSlotSequencer(64)
        val payload = LongArray(64)
        val start = CountDownLatch(1)
        val done = CountDownLatch(2)
        val failure = AtomicReference<Throwable?>()
        val producer = Thread({
            try {
                start.await()
                for (value in 1L..PUBLICATION_EVENTS) {
                    var position: Long
                    do {
                        position = sequencer.tryClaimProducer()
                        if (position == SpscSlotSequencer.NO_POSITION) Thread.yield()
                    } while (position == SpscSlotSequencer.NO_POSITION)
                    payload[sequencer.slotIndex(position)] = value
                    sequencer.publish(position)
                }
            } catch (throwable: Throwable) {
                failure.compareAndSet(null, throwable)
            } finally {
                done.countDown()
            }
        }, "JankHunterArtProducer")
        val consumer = Thread({
            try {
                start.await()
                var expected = 1L
                while (expected <= PUBLICATION_EVENTS) {
                    val position = sequencer.tryClaimConsumer()
                    if (position == SpscSlotSequencer.NO_POSITION) {
                        Thread.yield()
                        continue
                    }
                    assertEquals(expected, payload[sequencer.slotIndex(position)])
                    sequencer.release(position)
                    expected++
                }
            } catch (throwable: Throwable) {
                failure.compareAndSet(null, throwable)
            } finally {
                done.countDown()
            }
        }, "JankHunterArtConsumer")
        producer.start()
        consumer.start()
        start.countDown()

        assertTrue("ART publication test timed out", done.await(30, TimeUnit.SECONDS))
        producer.join(1_000L)
        consumer.join(1_000L)
        assertFalse(producer.isAlive)
        assertFalse(consumer.isAlive)
        failure.get()?.let { throw AssertionError("ART publication failure", it) }
    }

    @Test
    fun producerMatrixHasExactAccountingAndBoundedLifecycleOnArt() {
        assertTrue(Build.VERSION.SDK_INT >= 23)
        assertNotNull(Build.SUPPORTED_ABIS)
        val soakIterations = InstrumentationRegistry.getArguments()
            .getString("jankhunterSoakIterations")?.toIntOrNull()?.coerceIn(1, 100) ?: 1
        repeat(soakIterations) { iteration ->
            listOf(1, 8, 32).forEach { producers ->
                runGraphScenario(iteration, producers)
            }
        }
    }

    private fun runGraphScenario(iteration: Int, producerCount: Int) {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val directory = context.cacheDir.resolve("jankhunter-art-$iteration-$producerCount-${System.nanoTime()}")
        val writer = AsyncLogWriterFactory().open(
            directory,
            JankHunterConfig.builder().autoStartCollectors(false).flushIntervalMs(60_000).build(),
            "instrumentation",
        )
        val graph = RuntimeCallGraph(
            nowMs = RuntimeLongSource { System.nanoTime() / 1_000_000L },
            captureScreen = { "ArtScreen" },
            captureOperationId = { producerCount.toLong() + 1L },
            maxKeys = { 4_096 },
            consumerDelayNanos = 50_000L,
        )
        graph.resetFlushState(writer)
        val start = CountDownLatch(1)
        val done = CountDownLatch(producerCount)
        val progress = AtomicLong()
        val failure = AtomicReference<Throwable?>()
        val threads = List(producerCount) { producer ->
            Thread({
                try {
                    start.await()
                    repeat(EVENTS_PER_PRODUCER) { event ->
                        recordEdge(graph, producer.toLong(), (event and 63).toLong())
                        progress.incrementAndGet()
                    }
                } catch (throwable: Throwable) {
                    failure.compareAndSet(null, throwable)
                } finally {
                    done.countDown()
                }
            }, "JankHunterArtGraph-$producer")
        }
        try {
            threads.forEach(Thread::start)
            start.countDown()
            assertTrue("ART graph producers timed out", done.await(30, TimeUnit.SECONDS))
            threads.forEach { it.join(1_000L) }
            failure.get()?.let { throw AssertionError("ART graph producer failure", it) }
            val expected = producerCount.toLong() * EVENTS_PER_PRODUCER
            assertEquals(expected, progress.get())
            assertTrue(graph.flushBlocking(10_000L))
            assertEquals(expected, graph.attemptedForTest())
            assertEquals(expected, graph.emittedForTest())
            assertEquals(0L, graph.acceptedEventLossForTest())
            val consumer = graph.consumerForTest()
            graph.flushForShutdown()
            assertFalse(consumer?.isAlive == true)
        } finally {
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    private fun recordEdge(graph: RuntimeCallGraph, parentId: Long, childId: Long) {
        val parent = graph.enter(parentId, "parent-$parentId", enabled = true)
        val child = graph.enter(childId, "child-$childId", enabled = true)
        graph.exit(child, childId)
        graph.exit(parent, parentId)
    }

    private companion object {
        const val PUBLICATION_EVENTS = 1_000_000L
        const val EVENTS_PER_PRODUCER = 5_000
    }
}
