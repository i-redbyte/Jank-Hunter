package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeHookAdmissionTest {
    @Test
    fun zeroBudgetNeverEntersBackpressureWait() = withBlockedConsumer(waitNanos = { 0L }) { transport ->
        repeat(256) { assertTrue(transport.recordMethod(1L, "method")) }
        assertFalse(transport.recordMethod(2L, "rejected"))
        assertEquals(0L, transport.backpressureCountForTest())
        assertEquals(257L, transport.attemptedForTest())
    }

    @Test
    fun shutdownReleasesSaturatedPublisherBeforeConsumerCanDrain() = withBlockedConsumer(
        waitNanos = { TimeUnit.SECONDS.toNanos(5L) },
        beforeAwait = {
            awaitBackpressure(it)
            assertFalse(it.stopAndFlush(1L))
        },
    ) { transport ->
        repeat(256) { assertTrue(transport.recordMethod(1L, "method")) }
        assertFalse(transport.recordMethod(2L, "rejected"))
        assertEquals(257L, transport.attemptedForTest())
        assertEquals(256L, transport.acceptedForTest())
    }

    @Test
    fun disablingExactAdmissionReleasesSaturatedPublisher() {
        val exact = AtomicBoolean(true)
        withBlockedConsumer(
            waitNanos = { TimeUnit.SECONDS.toNanos(5L) },
            exact = { exact.get() },
            beforeAwait = {
                awaitBackpressure(it)
                exact.set(false)
            },
        ) { transport ->
            repeat(256) { assertTrue(transport.recordMethod(1L, "method")) }
            assertFalse(transport.recordMethod(2L, "rejected"))
            assertEquals(257L, transport.attemptedForTest())
        }
    }

    @Test
    fun saturatedExactAdmissionReturnsWithinBound() = withBlockedConsumer { transport ->
        repeat(256) { assertTrue(transport.recordMethod(1L, "method")) }
        assertFalse(transport.recordMethod(2L, "rejected"))
        assertEquals(256L, transport.acceptedForTest())
        assertEquals(257L, transport.attemptedForTest())
    }

    @Test
    fun interruptedExactAdmissionReturnsWithoutClearingInterrupt() = withBlockedConsumer { transport ->
        repeat(256) { assertTrue(transport.recordMethod(1L, "method")) }
        Thread.currentThread().interrupt()
        try {
            assertFalse(transport.recordMethod(2L, "rejected"))
            assertTrue(Thread.currentThread().isInterrupted)
            assertEquals(257L, transport.attemptedForTest())
        } finally {
            Thread.interrupted()
        }
    }

    private fun withBlockedConsumer(
        waitNanos: RuntimeLongSource = RuntimeLongSource { 5_000_000L },
        exact: RuntimeBooleanSource = RuntimeBooleanSource { true },
        beforeAwait: (RuntimeHookEventTransport) -> Unit = {},
        block: (RuntimeHookEventTransport) -> Unit,
    ) {
        val directory = Files.createTempDirectory("jankhunter-hook-admission").toFile()
        val release = CountDownLatch(1)
        val entered = CountDownLatch(1)
        val writer = AsyncLogWriterFactory().open(
            directory, JankHunterConfig.builder().autoStartCollectors(false).build(), "main",
        )
        val transport = RuntimeHookEventTransport(
            { 16 }, { 16 },
            exactAdmission = exact,
            admissionWaitNanos = waitNanos,
            consumerLoopObserver = {
                entered.countDown()
                check(release.await(5L, TimeUnit.SECONDS))
            },
        )
        val executor = Executors.newSingleThreadExecutor()
        transport.start(writer)
        try {
            assertTrue(entered.await(5L, TimeUnit.SECONDS))
            // A stopped consumer deterministically keeps the 256-slot ring full. The timeout
            // is only a test guard, and cannot unblock production or satisfy the assertion.
            val task = executor.submit { block(transport) }
            beforeAwait(transport)
            task.get(500L, TimeUnit.MILLISECONDS)
        } finally {
            release.countDown()
            executor.shutdown()
            assertTrue(executor.awaitTermination(5L, TimeUnit.SECONDS))
            assertTrue(transport.stopAndFlush(5_000L))
            transport.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    private fun awaitBackpressure(transport: RuntimeHookEventTransport) {
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(2L)
        while (transport.backpressureCountForTest() == 0L) {
            assertTrue("producer did not reach backpressure", System.nanoTime() < deadline)
            Thread.yield()
        }
    }
}
