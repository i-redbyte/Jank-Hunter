package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.nio.file.Files
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeConsumerInitializationFailureTest {
    @Test fun methodAccumulatorFailureClosesPublisherGate() = verifyFailure(false, false)
    @Test fun logAccumulatorFailureClosesPublisherGate() = verifyFailure(true, false)
    @Test fun fatalAccumulatorFailureReleasesOwnerBeforeRethrowing() = verifyFailure(false, true)

    private fun verifyFailure(logFailure: Boolean, fatal: Boolean) {
        val directory = Files.createTempDirectory("jankhunter-consumer-init-failure").toFile()
        val writer = AsyncLogWriterFactory().open(directory, JankHunterConfig.builder().autoStartCollectors(false).build(), "main")
        val fail = AtomicBoolean(true)
        val error: Throwable = if (fatal) OutOfMemoryError("injected allocation failure") else IllegalStateException("injected init failure")
        val source = RuntimeIntSource { if (fail.get()) throw error else 16 }
        val uncaught = AtomicReference<Throwable>()
        lateinit var hooks: RuntimeHookEventTransport
        hooks = RuntimeHookEventTransport(
            maxCounterKeys = if (logFailure) RuntimeIntSource { 16 } else source,
            maxLogSpamKeys = if (logFailure) source else RuntimeIntSource { 16 },
            consumerThreadFactory = { task, name ->
                object : Thread(task, name) {
                    override fun start() {
                        if (fail.get()) {
                            assertTrue(hooks.recordMethod(1L, "admitted-before-init"))
                            uncaughtExceptionHandler = UncaughtExceptionHandler { _, throwable -> uncaught.set(throwable) }
                            super.start()
                            join(2_000L)
                        } else super.start()
                    }
                }
            },
        )
        try {
            hooks.start(writer)
            assertFalse(hooks.acceptingPublishersForTest())
            assertFalse(hooks.flushBlocking(1L))
            assertEquals(1L, hooks.acceptedForTest())
            assertEquals(1L, hooks.acceptedLossForTest())
            assertEquals(0, hooks.registeredProducerCountForTest())
            if (fatal) assertSame(error, uncaught.get()) else assertEquals(null, uncaught.get())
            hooks.clear()
            fail.set(false)
            hooks.start(writer)
            assertTrue(hooks.recordMethod(2L, "recovered"))
            assertTrue(hooks.flushBlocking(2_000L))
        } finally {
            hooks.stopAndFlush(2_000L)
            hooks.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }
}
