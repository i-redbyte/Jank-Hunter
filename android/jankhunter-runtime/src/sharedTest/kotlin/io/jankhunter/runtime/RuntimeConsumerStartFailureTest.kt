package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeConsumerStartFailureTest {
    @Test fun hookStartFailureDoesNotWaitForPausedPublisher() = pausedPublisher(false)
    @Test fun graphStartFailureDoesNotWaitForPausedPublisher() = pausedPublisher(true)

    @Test fun constructionFailureDoesNotConsumeGenerationSlot() {
        val directory = Files.createTempDirectory("jankhunter-consumer-construction").toFile()
        val writer = AsyncLogWriterFactory().open(directory, JankHunterConfig.builder().autoStartCollectors(false).build(), "main")
        val failure = IllegalStateException("injected thread construction failure")
        val factory: (Runnable, String) -> Thread = { _, _ -> throw failure }
        val hooks = RuntimeHookEventTransport({ 16 }, { 16 }, consumerThreadFactory = factory)
        val graph = RuntimeCallGraph({ 1L }, { null }, { 0L }, { 16 }, consumerThreadFactory = factory)
        try {
            repeat(4) {
                assertSame(failure, assertThrows(IllegalStateException::class.java) { hooks.start(writer) })
                hooks.clear()
                assertSame(failure, assertThrows(IllegalStateException::class.java) { graph.resetFlushState(writer) })
                graph.clear()
            }
        } finally {
            hooks.clear()
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    private fun pausedPublisher(graphMode: Boolean) {
        val directory = Files.createTempDirectory("jankhunter-start-failure").toFile()
        val writer = AsyncLogWriterFactory().open(directory, JankHunterConfig.builder().autoStartCollectors(false).build(), "main")
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val failure = IllegalStateException("injected thread start failure")
        val executor = Executors.newSingleThreadExecutor()
        lateinit var publish: () -> Unit
        lateinit var publisher: Thread
        val factory: (Runnable, String) -> Thread = { task, name ->
            object : Thread(task, name) {
                override fun start() {
                    publisher = Thread { publish() }.also { it.start() }
                    check(entered.await(2L, TimeUnit.SECONDS))
                    throw failure
                }
            }
        }
        val observer = { entered.countDown(); check(release.await(5L, TimeUnit.SECONDS)) }
        val hooks = RuntimeHookEventTransport({ 16 }, { 16 }, publisherAdmissionObserver = observer, consumerThreadFactory = factory)
        val graph = RuntimeCallGraph({ 1L }, { null }, { 0L }, { 16 }, publisherAdmissionObserver = observer, consumerThreadFactory = factory)
        val drained = CountDownLatch(1)
        publish = if (graphMode) ({ graph.recordSemantic(1L, "root", 2L, "child", 1L, true) })
            else ({ hooks.recordMethod(1L, "method"); Unit })
        try {
            val start = executor.submit {
                assertSame(failure, assertThrows(IllegalStateException::class.java) {
                    if (graphMode) graph.resetFlushState(writer) else hooks.start(writer)
                })
            }
            assertTrue(entered.await(2L, TimeUnit.SECONDS))
            start.get(300L, TimeUnit.MILLISECONDS)
            if (graphMode) graph.whenWriterDrained(writer, drained::countDown)
            else hooks.whenWriterDrained(writer, drained::countDown)
            release.countDown()
            publisher.join(2_000L)
            assertTrue(drained.await(2L, TimeUnit.SECONDS))
            if (graphMode) assertEquals(graph.acceptedForTest(), graph.emittedForTest() + graph.acceptedEventLossForTest())
            else assertEquals(hooks.acceptedForTest(), hooks.emittedForTest() + hooks.acceptedLossForTest())
        } finally {
            release.countDown()
            executor.shutdown()
            assertTrue(executor.awaitTermination(3L, TimeUnit.SECONDS))
            hooks.clear()
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }
}
