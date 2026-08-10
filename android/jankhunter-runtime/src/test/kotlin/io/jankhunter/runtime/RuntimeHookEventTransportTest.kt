package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeHookEventTransportTest {
    @Test
    fun acceptedEventsAreEitherEmittedOrReportedLost() = withTransport { transport ->
        repeat(10_000) { transport.recordMethod(it.toLong() and 7L, "method") }

        assertTrue(transport.flushBlocking(5_000L))
        assertEquals(
            transport.acceptedForTest(),
            transport.emittedForTest() + transport.acceptedLossForTest(),
        )
        assertEquals(10_000L, transport.attemptedForTest())
    }

    @Test
    fun counterAndLogCardinalityLossRemainComplete() = withTransport(
        maxCounterKeys = 1,
        maxLogSpamKeys = 1,
    ) { transport ->
        assertTrue(transport.recordMethod(1L, "one"))
        assertTrue(transport.recordMethod(2L, "two"))
        assertTrue(transport.recordLogSpam("screen", "owner", "flow", "step", "source-a", 3))
        assertTrue(transport.recordLogSpam("screen", "owner", "flow", "step", "source-b", 3))

        assertTrue(transport.flushBlocking(2_000L))
        assertEquals(4L, transport.acceptedForTest())
        assertEquals(transport.acceptedForTest(), transport.emittedForTest() + transport.acceptedLossForTest())
        assertTrue(transport.acceptedLossForTest() >= 2L)
    }

    @Test
    fun thirtyTwoProducersNeverWaitForConsumer() = withTransport { transport ->
        val producerCount = 32
        val eventsPerProducer = 2_000
        val pool = Executors.newFixedThreadPool(producerCount)
        val start = CountDownLatch(1)
        val done = CountDownLatch(producerCount)
        try {
            repeat(producerCount) { producer ->
                pool.execute {
                    start.await()
                    repeat(eventsPerProducer) { event ->
                        transport.recordMethod((producer * 8L) + (event and 7), "method")
                    }
                    done.countDown()
                }
            }
            start.countDown()
            assertTrue("producer progress timed out", done.await(10, TimeUnit.SECONDS))
            assertEquals((producerCount * eventsPerProducer).toLong(), transport.attemptedForTest())
        } finally {
            pool.shutdownNow()
        }
    }

    @Test
    fun shutdownTerminatesDedicatedDaemonConsumer() {
        val directory = Files.createTempDirectory("jankhunter-runtime-hook-events").toFile()
        val writer = writer(directory)
        val transport = RuntimeHookEventTransport({ 16 }, { 16 })
        transport.start(writer)
        val consumer = transport.consumerForTest()
        assertEquals("JankHunterEvents", consumer?.name)
        assertTrue(consumer?.isDaemon == true)

        assertTrue(transport.stopAndFlush(2_000L))

        assertFalse(consumer?.isAlive == true)
        transport.clear()
        writer.close()
        directory.deleteRecursively()
    }

    private fun withTransport(
        maxCounterKeys: Int = 128,
        maxLogSpamKeys: Int = 128,
        block: (RuntimeHookEventTransport) -> Unit,
    ) {
        val directory = Files.createTempDirectory("jankhunter-runtime-hook-events").toFile()
        val writer = writer(directory)
        val transport = RuntimeHookEventTransport({ maxCounterKeys }, { maxLogSpamKeys })
        transport.start(writer)
        try {
            block(transport)
        } finally {
            transport.stopAndFlush(5_000L)
            transport.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    private fun writer(directory: java.io.File): AsyncLogWriter {
        return AsyncLogWriter.open(
            directory,
            JankHunterConfig.builder()
                .autoStartCollectors(false)
                .flushIntervalMs(60_000)
                .build(),
            "main",
        )
    }
}
