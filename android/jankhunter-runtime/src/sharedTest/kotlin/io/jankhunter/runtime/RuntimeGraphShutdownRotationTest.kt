package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import java.util.concurrent.locks.LockSupport
import org.junit.Assert.*
import org.junit.Test

class RuntimeGraphShutdownRotationTest {
    @Test
    fun admittedExitCanFinishAnInProgressRotationAfterStopClosesAdmission() {
        val root = Files.createTempDirectory("jh-graph-stop-rotation").toFile()
        val writer = AsyncLogWriterFactory().open(root, JankHunterConfig.builder().autoStartCollectors(false).build(), "main")
        val consumerEntered = CountDownLatch(1)
        val consumerRelease = CountDownLatch(1)
        val admitted = CountDownLatch(1)
        val publisherRelease = CountDownLatch(1)
        val failure = AtomicReference<Throwable?>()
        val graph = RuntimeCallGraph({ 0L }, { null }, { 0L }, { 16 },
            admissionWaitNanos = { TimeUnit.SECONDS.toNanos(2L) },
            publisherAdmissionObserver = {
                admitted.countDown()
                check(publisherRelease.await(5L, TimeUnit.SECONDS))
            }, consumerLoopObserver = {
                consumerEntered.countDown()
                check(consumerRelease.await(5L, TimeUnit.SECONDS))
            })
        val publisher = Thread {
            try {
                graph.enter(1L, "parent", true)
                val child = graph.enter(2L, "child", true)
                graph.exit(child, 2L)
            } catch (error: Throwable) { failure.set(error) }
        }
        try {
            graph.resetFlushState(writer)
            val consumer = checkNotNull(graph.consumerForTest())
            assertTrue(consumerEntered.await(5L, TimeUnit.SECONDS))
            publisher.start()
            assertTrue(admitted.await(5L, TimeUnit.SECONDS))
            val session = RuntimeCallGraph::class.java.getDeclaredField("primary").apply { isAccessible = true }.get(graph)
            val producer = RuntimeCallGraphSession::class.java.getDeclaredField("producer")
                .apply { isAccessible = true }.get(session) as RuntimeGraphProducer
            val buffer = producer.registry.single().buffer
            assertFalse(graph.flushForShutdown(1L))
            // The consumer is barrier-paused; explicitly schedule its rotation handshake.
            buffer.rotationRequested = true
            publisherRelease.countDown()
            val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(2L)
            while (!buffer.producerWaiting && publisher.isAlive && System.nanoTime() < deadline) Thread.yield()
            assertTrue("an admitted exit was rejected instead of waiting for the bounded rotation", buffer.producerWaiting)
            buffer.rotationRequested = false
            LockSupport.unpark(publisher)
            consumerRelease.countDown()
            publisher.join(5_000L)
            consumer.join(5_000L)
            failure.get()?.let { throw AssertionError(it) }
            assertFalse(publisher.isAlive)
            assertFalse(consumer.isAlive)
            assertEquals(1L, graph.attemptedForTest())
            assertEquals(1L, graph.acceptedForTest())
            assertEquals(1L, graph.emittedForTest())
        } finally {
            publisherRelease.countDown()
            consumerRelease.countDown()
            publisher.join(5_000L)
            graph.flushForShutdown(5_000L)
            graph.clear()
            writer.close()
            root.deleteRecursively()
        }
    }
}
