package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.Jhlog
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.zip.GZIPInputStream
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
            transport.emittedForTest(),
        )
        assertEquals(0L, transport.acceptedLossForTest())
        assertEquals(10_000L, transport.attemptedForTest())
    }

    @Test
    fun cardinalityBoundariesFlushPartialAggregatesWithoutLoss() = withTransport(
        maxCounterKeys = 1,
        maxLogSpamKeys = 1,
    ) { transport ->
        assertTrue(transport.recordMethod(1L, "one"))
        assertTrue(transport.recordMethod(2L, "two"))
        assertTrue(transport.recordLogSpam("screen", "owner", "source-a", 3))
        assertTrue(transport.recordLogSpam("screen", "owner", "source-b", 3))

        assertTrue(transport.flushBlocking(2_000L))
        assertEquals(4L, transport.acceptedForTest())
        assertEquals(transport.acceptedForTest(), transport.emittedForTest())
        assertEquals(0L, transport.acceptedLossForTest())
    }

    @Test
    fun methodNameIsEmbeddedWithTheFirstEvent() {
        val directory = Files.createTempDirectory("jankhunter-runtime-hook-symbol").toFile()
        val writer = writer(directory)
        val transport = RuntimeHookEventTransport({ 16 }, { 16 })
        transport.start(writer)
        try {
            assertTrue(transport.recordMethod(42L, "example.Symbol.call"))
            assertTrue(transport.stopAndFlush(5_000L))
            assertTrue(writer.close())

            assertEquals(1L, transport.acceptedForTest())
            assertEquals(1L, transport.emittedForTest())
            val file = directory.listFiles { candidate -> candidate.extension == "jhlog" }.orEmpty().single()
            val decoded = decodedChunks(file.readBytes())
            assertTrue(
                "stable symbol tokens were not embedded",
                decoded.containsSubsequence("example".toByteArray()) &&
                    decoded.containsSubsequence("Symbol".toByteArray()) &&
                    decoded.containsSubsequence("call".toByteArray()),
            )
        } finally {
            transport.stopAndFlush(5_000L)
            transport.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    @Test
    fun oneHundredSixtyProducersAreRegisteredAndFlushedWithoutLoss() = withTransport { transport ->
        val producerCount = 160
        val eventsPerProducer = 400
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
            assertTrue("producer progress timed out", done.await(15, TimeUnit.SECONDS))
            assertEquals((producerCount * eventsPerProducer).toLong(), transport.attemptedForTest())
            assertTrue(transport.flushBlocking(10_000L))
            assertEquals(transport.acceptedForTest(), transport.emittedForTest())
            assertEquals(0L, transport.acceptedLossForTest())
        } finally {
            pool.shutdownNow()
        }
    }

    @Test
    fun exactBufferBackpressurePreservesBurst() {
        val directory = Files.createTempDirectory("jankhunter-runtime-hook-backpressure").toFile()
        val writer = writer(directory)
        val transport = RuntimeHookEventTransport(
            maxCounterKeys = { 16 },
            maxLogSpamKeys = { 16 },
            exactAdmission = { true },
            consumerDelayNanos = TimeUnit.MILLISECONDS.toNanos(10L),
        )
        transport.start(writer)
        try {
            repeat(10_000) { event -> assertTrue(transport.recordMethod(event.toLong() and 7L, "method")) }
            val observedBackpressure = transport.backpressureCountForTest()
            assertTrue(transport.flushBlocking(10_000L))
            assertEquals(10_000L, transport.acceptedForTest())
            assertEquals(10_000L, transport.emittedForTest())
            assertEquals(0L, transport.acceptedLossForTest())
            assertTrue(observedBackpressure > 0L)
        } finally {
            transport.stopAndFlush(5_000L)
            transport.clear()
            writer.close()
            directory.deleteRecursively()
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

    @Test
    fun exactShutdownWaitsPastBestEffortTimeoutForConsumerFrontier() {
        val directory = Files.createTempDirectory("jankhunter-runtime-hook-shutdown-frontier").toFile()
        val writer = writer(directory)
        val transport = RuntimeHookEventTransport(
            maxCounterKeys = { 16 },
            maxLogSpamKeys = { 16 },
            exactAdmission = { true },
            consumerDelayNanos = TimeUnit.MILLISECONDS.toNanos(100L),
        )
        transport.start(writer)
        try {
            assertTrue(transport.recordMethod(1L, "method"))

            assertTrue(transport.stopAndFlush(timeoutMs = 1L))
            assertEquals(transport.acceptedForTest(), transport.emittedForTest())
            assertEquals(0L, transport.acceptedLossForTest())
        } finally {
            transport.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    @Test
    fun exactShutdownDrainsPublisherAdmittedAtTheShutdownBoundary() {
        val directory = Files.createTempDirectory("jankhunter-runtime-hook-admission-frontier").toFile()
        val writer = writer(directory)
        val admitted = CountDownLatch(1)
        val release = CountDownLatch(1)
        val transport = RuntimeHookEventTransport(
            maxCounterKeys = { 16 },
            maxLogSpamKeys = { 16 },
            exactAdmission = { true },
            publisherAdmissionObserver = {
                admitted.countDown()
                assertTrue("publisher release timed out", release.await(5L, TimeUnit.SECONDS))
            },
        )
        val executor = Executors.newFixedThreadPool(2)
        transport.start(writer)
        try {
            val publisher = executor.submit<Boolean> { transport.recordMethod(1L, "method") }
            assertTrue("publisher was not admitted", admitted.await(5L, TimeUnit.SECONDS))
            val shutdown = executor.submit<Boolean> { transport.stopAndFlush(timeoutMs = 1L) }
            awaitPublisherGateClosed(transport)

            release.countDown()

            assertTrue("admitted publisher was rejected", publisher.get(5L, TimeUnit.SECONDS))
            assertTrue("exact shutdown did not finish", shutdown.get(5L, TimeUnit.SECONDS))
            assertEquals(1L, transport.acceptedForTest())
            assertEquals(1L, transport.emittedForTest())
            assertEquals(0L, transport.acceptedLossForTest())
        } finally {
            release.countDown()
            executor.shutdownNow()
            transport.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    @Test
    fun consumerFailureClosesAdmissionAndAccountsForStrandedEvents() {
        val directory = Files.createTempDirectory("jankhunter-runtime-hook-consumer-failure").toFile()
        val writer = writer(directory)
        val consumerEntered = CountDownLatch(1)
        val failConsumer = CountDownLatch(1)
        val transport = RuntimeHookEventTransport(
            maxCounterKeys = { 16 },
            maxLogSpamKeys = { 16 },
            exactAdmission = { true },
            consumerLoopObserver = {
                consumerEntered.countDown()
                assertTrue("consumer failure trigger timed out", failConsumer.await(5L, TimeUnit.SECONDS))
                error("injected consumer failure")
            },
        )
        transport.start(writer)
        try {
            assertTrue("consumer did not start", consumerEntered.await(5L, TimeUnit.SECONDS))
            assertTrue(transport.recordMethod(1L, "method"))

            failConsumer.countDown()
            awaitConsumerStopped(transport)

            assertFalse(transport.acceptingPublishersForTest())
            assertFalse(transport.recordMethod(2L, "after failure"))
            assertEquals(1L, transport.acceptedForTest())
            assertEquals(0L, transport.emittedForTest())
            assertEquals(1L, transport.acceptedLossForTest())
        } finally {
            failConsumer.countDown()
            transport.clear()
            writer.close()
            directory.deleteRecursively()
        }
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
        return AsyncLogWriterFactory().open(
            directory,
            JankHunterConfig.builder()
                .autoStartCollectors(false)
                .flushIntervalMs(60_000)
                .build(),
            "main",
        )
    }

    private fun awaitPublisherGateClosed(transport: RuntimeHookEventTransport) {
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(5L)
        while (transport.acceptingPublishersForTest()) {
            assertTrue("publisher gate did not close", System.nanoTime() < deadline)
            Thread.yield()
        }
    }

    private fun awaitConsumerStopped(transport: RuntimeHookEventTransport) {
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(5L)
        while (transport.consumerForTest()?.isAlive == true) {
            assertTrue("consumer did not stop", System.nanoTime() < deadline)
            Thread.yield()
        }
    }

    private fun decodedChunks(file: ByteArray): ByteArray {
        if (file.size < Jhlog.FILE_MAGIC.size + Int.SIZE_BYTES * 2) return ByteArray(0)
        val headerLength = readUInt32Le(file, Jhlog.FILE_MAGIC.size)
        var offset = Jhlog.FILE_MAGIC.size + Int.SIZE_BYTES * 2 + headerLength
        val decoded = ByteArrayOutputStream()
        while (offset + Jhlog.CHUNK_HEADER_BYTES <= file.size) {
            val flags = readUInt16Le(file, offset + 6)
            val storedLength = readUInt32Le(file, offset + 12)
            val payloadStart = offset + Jhlog.CHUNK_HEADER_BYTES
            val trailerStart = payloadStart + storedLength
            val chunkEnd = trailerStart + Jhlog.COMMIT_TRAILER_BYTES
            if (storedLength < 0 || trailerStart < payloadStart || chunkEnd > file.size) break
            val stored = file.copyOfRange(payloadStart, trailerStart)
            if (flags and Jhlog.CHUNK_FLAG_GZIP != 0) {
                GZIPInputStream(ByteArrayInputStream(stored)).use { input -> input.copyTo(decoded) }
            } else {
                decoded.write(stored)
            }
            offset = chunkEnd
        }
        return decoded.toByteArray()
    }

    private fun readUInt16Le(bytes: ByteArray, offset: Int): Int {
        return (bytes[offset].toInt() and 0xff) or ((bytes[offset + 1].toInt() and 0xff) shl Byte.SIZE_BITS)
    }

    private fun readUInt32Le(bytes: ByteArray, offset: Int): Int {
        var value = 0
        repeat(Int.SIZE_BYTES) { index ->
            value = value or ((bytes[offset + index].toInt() and 0xff) shl (index * Byte.SIZE_BITS))
        }
        return value
    }

    private fun ByteArray.containsSubsequence(expected: ByteArray): Boolean {
        if (expected.isEmpty()) return true
        for (start in 0..size - expected.size) {
            var matches = true
            for (index in expected.indices) {
                if (this[start + index] != expected[index]) {
                    matches = false
                    break
                }
            }
            if (matches) return true
        }
        return false
    }
}
