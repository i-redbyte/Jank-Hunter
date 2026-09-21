package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeGraphGlobalBudgetTest {
    @Test
    fun concurrentProducerBurstsRespectGlobalStorageBudgetAndAccountForEveryAttempt() {
        val directory = File.createTempFile("jh-graph-budget", "").apply { check(delete()); check(mkdirs()) }
        val writer = AsyncLogWriterFactory().open(directory,
            JankHunterConfig.builder().autoStartCollectors(false).build(), "main")
        val consumerEntered = CountDownLatch(1)
        val releaseConsumer = CountDownLatch(1)
        val producersReady = CountDownLatch(PRODUCERS)
        val releaseProducers = CountDownLatch(1)
        val failure = AtomicReference<Throwable?>()
        val graph = RuntimeCallGraph({ 1L }, { null }, { 0L }, { 128 }, exactAdmission = { false },
            consumerLoopObserver = {
                consumerEntered.countDown()
                check(releaseConsumer.await(20L, TimeUnit.SECONDS))
            })
        val threads = List(PRODUCERS) { index ->
            Thread({
                try {
                    repeat(EVENTS) { edge ->
                        graph.recordSemantic(1L, "caller", edge + 2L, "callee", 1L, true)
                    }
                } catch (error: Throwable) {
                    failure.compareAndSet(null, error)
                } finally {
                    producersReady.countDown()
                    check(releaseProducers.await(20L, TimeUnit.SECONDS))
                }
            }, "graph-budget-$index")
        }
        try {
            graph.resetFlushState(writer)
            assertTrue(consumerEntered.await(5L, TimeUnit.SECONDS))
            threads.forEach(Thread::start)
            assertTrue(producersReady.await(10L, TimeUnit.SECONDS))
            failure.get()?.let { throw AssertionError("producer failed", it) }
            val payloadBytes = retainedPagePayloadBytes(graph)
            assertTrue("aggregate arrays alone exceed the shared 8 MiB generation budget: $payloadBytes",
                payloadBytes <= GENERATION_BUDGET_BYTES)
            assertTrue(graph.storageUsedForTest() >= payloadBytes)
            assertTrue(graph.storagePeakForTest() <= GENERATION_BUDGET_BYTES)
            assertEquals(PRODUCERS.toLong() * EVENTS, graph.attemptedForTest())
            assertEquals(graph.attemptedForTest(), graph.acceptedForTest() + graph.producerCapacityLossForTest())
            releaseConsumer.countDown()
            releaseProducers.countDown()
            threads.forEach { it.join(5_000L); assertFalse(it.isAlive) }
            assertTrue(graph.flushBlocking(5_000L))
            assertEquals(graph.acceptedForTest(), graph.emittedForTest())
            assertEquals(0L, graph.acceptedEventLossForTest())
            assertTrue(graph.flushForShutdown(5_000L))
            assertEquals(0L, graph.storageUsedForTest())
        } finally {
            releaseConsumer.countDown()
            releaseProducers.countDown()
            threads.forEach { it.join(5_000L) }
            graph.flushForShutdown(5_000L)
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    private fun retainedPagePayloadBytes(graph: RuntimeCallGraph): Long {
        val session = RuntimeCallGraph::class.java.getDeclaredField("primary")
            .apply { isAccessible = true }.get(graph)
        val producer = RuntimeCallGraphSession::class.java.getDeclaredField("producer")
            .apply { isAccessible = true }.get(session) as RuntimeGraphProducer
        val pagesField = RuntimeGraphAggregateBuffer::class.java.getDeclaredField("pages").apply { isAccessible = true }
        var bytes = 0L
        for (state in producer.registry) {
            for (page in (pagesField.get(state.buffer) as Array<*>).filterNotNull()) {
                for (field in RuntimeGraphAggregatePage::class.java.declaredFields) {
                    if (!field.type.isArray) continue
                    val width = when (field.type.componentType) {
                        Byte::class.javaPrimitiveType -> 1
                        Int::class.javaPrimitiveType -> 4
                        else -> 8 // Conservative reference width; excludes object/array headers.
                    }
                    field.isAccessible = true
                    bytes += java.lang.reflect.Array.getLength(checkNotNull(field.get(page))).toLong() * width
                }
            }
        }
        return bytes
    }

    private companion object {
        const val PRODUCERS = 128
        const val EVENTS = RUNTIME_GRAPH_PAGE_QUEUE_CAPACITY * RUNTIME_GRAPH_PAGE_MAX_KEYS
        const val GENERATION_BUDGET_BYTES = 8L * 1024L * 1024L
    }
}
