package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.QualityCounterId
import java.nio.file.Files
import java.lang.ref.WeakReference
import java.util.concurrent.CountDownLatch
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.*
import org.junit.Test

class RuntimeGraphBudgetLifecycleTest {
    @Test
    fun failedThreadLocalPublicationDoesNotLeaveAReleasedStateVisibleToTheNextEntry() {
        val root = Files.createTempDirectory("jh-graph-thread-local-failure").toFile()
        val writer = AsyncLogWriterFactory().open(root, JankHunterConfig.builder().autoStartCollectors(false).build(), "main")
        val graph = RuntimeCallGraph({ 0L }, { null }, { 0L }, { 16 })
        val injected = OutOfMemoryError("injected ThreadLocal rehash failure")
        try {
            graph.resetFlushState(writer)
            val session = RuntimeCallGraph::class.java.getDeclaredField("primary")
                .apply { isAccessible = true }.get(graph)
            val producer = RuntimeCallGraphSession::class.java.getDeclaredField("producer")
                .apply { isAccessible = true }.get(session) as RuntimeGraphProducer
            val local = object : ThreadLocal<WeakReference<RuntimeGraphProducerState>>() {
                override fun set(value: WeakReference<RuntimeGraphProducerState>?) {
                    super.set(value)
                    throw injected
                }
            }
            RuntimeGraphProducer::class.java.getDeclaredField("threadState").apply { isAccessible = true }.set(producer, local)
            try {
                graph.enter(1L, "unpublished", true)
                fail("the original fatal error must propagate")
            } catch (failure: OutOfMemoryError) { assertSame(injected, failure) }
            assertEquals(0, graph.registeredProducerCountForTest())
            assertEquals(0L, graph.storageUsedForTest())
            assertNull("next entry must not reuse a released producer", local.get())
        } finally {
            graph.flushForShutdown(5_000L)
            graph.clear()
            writer.close()
            root.deleteRecursively()
        }
    }

    @Test
    fun failedRegistryPublicationReturnsTheEntireUnpublishedReservation() {
        val root = Files.createTempDirectory("jh-graph-registration-failure").toFile()
        val writer = AsyncLogWriterFactory().open(root, JankHunterConfig.builder().autoStartCollectors(false).build(), "main")
        val graph = RuntimeCallGraph({ 0L }, { null }, { 0L }, { 16 })
        val injected = OutOfMemoryError("injected registry-node allocation failure")
        try {
            graph.resetFlushState(writer)
            val session = RuntimeCallGraph::class.java.getDeclaredField("primary")
                .apply { isAccessible = true }.get(graph)
            val producer = RuntimeCallGraphSession::class.java.getDeclaredField("producer")
                .apply { isAccessible = true }.get(session) as RuntimeGraphProducer
            RuntimeGraphProducer::class.java.getDeclaredField("registry").apply { isAccessible = true }
                .set(producer, object : ConcurrentLinkedQueue<RuntimeGraphProducerState>() {
                    override fun add(element: RuntimeGraphProducerState): Boolean = throw injected
                })
            try {
                graph.enter(1L, "unpublished", true)
                fail("the original fatal error must propagate")
            } catch (failure: OutOfMemoryError) { assertSame(injected, failure) }
            assertEquals(0, graph.registeredProducerCountForTest())
            assertEquals("failed publication retained a quota without an owner", 0L, graph.storageUsedForTest())
        } finally {
            graph.flushForShutdown(5_000L)
            graph.clear()
            writer.close()
            root.deleteRecursively()
        }
    }

    @Test
    fun coldProducerDenialCountsKnownSemanticEdgesAndUnknownMethodEntriesSeparately() {
        val root = Files.createTempDirectory("jh-graph-cold-budget").toFile()
        val writer = AsyncLogWriterFactory().open(root, JankHunterConfig.builder().autoStartCollectors(false).build(), "main")
        val graph = RuntimeCallGraph({ 0L }, { null }, { 0L }, { 16 }, exactAdmission = { false },
            producerStorageLimitBytes = 16_384L)
        val ready = List(3) { CountDownLatch(1) }
        val release = CountDownLatch(1)
        val failure = AtomicReference<Throwable?>()
        val threads = List(3) { index -> Thread {
            try {
                assertTrue(graph.enter(1L, "root", true) > 0L)
                ready[index].countDown()
                check(release.await(10L, TimeUnit.SECONDS))
            } catch (error: Throwable) { failure.compareAndSet(null, error); ready[index].countDown() }
        } }
        try {
            graph.resetFlushState(writer)
            threads.forEachIndexed { index, thread ->
                thread.start()
                assertTrue(ready[index].await(5L, TimeUnit.SECONDS))
            }
            failure.get()?.let { throw AssertionError(it) }
            repeat(7) { assertEquals(0L, graph.enter(2L, "omitted", true)) }
            graph.recordSemantic(1L, "caller", 2L, "callee", 0L, true)
            assertTrue(graph.flushBlocking(5_000L))
            assertEquals(3, graph.registeredProducerCountForTest())
            assertEquals(7L, generationQuality(writer, QualityCounterId.RUNTIME_GRAPH_STORAGE_SKIPPED_ENTRY_TOTAL))
            assertEquals(1L, generationQuality(writer, QualityCounterId.RUNTIME_GRAPH_PRODUCER_CAPACITY_LOSS))
            assertEquals(1L, generationQuality(writer, QualityCounterId.RUNTIME_GRAPH_INPUT_TOTAL))
            assertEquals(0L, graph.acceptedForTest())
            assertEquals(0L, graph.acceptedEventLossForTest())
            assertTrue(graph.storagePeakForTest() <= 16_384L)
        } finally {
            release.countDown()
            threads.forEach { it.join(5_000L); assertFalse(it.isAlive) }
            assertTrue(graph.flushForShutdown(5_000L))
            assertEquals(0L, graph.storageUsedForTest())
            graph.clear()
            writer.close()
            root.deleteRecursively()
        }
    }

    @Test
    fun stopWaitsForAnEntryThatOwnsStackStorageAndReleasesItAfterTheDeadline() {
        val root = Files.createTempDirectory("jh-graph-entry-close").toFile()
        val writer = AsyncLogWriterFactory().open(root, JankHunterConfig.builder().autoStartCollectors(false).build(), "main")
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val failure = AtomicReference<Throwable?>()
        val graph = RuntimeCallGraph({
            entered.countDown()
            check(release.await(10L, TimeUnit.SECONDS))
            0L
        }, { null }, { 0L }, { 16 })
        val producer = Thread {
            try { graph.enter(1L, "root", true) } catch (error: Throwable) { failure.set(error) }
        }
        try {
            graph.resetFlushState(writer)
            val consumer = checkNotNull(graph.consumerForTest())
            producer.start()
            assertTrue(entered.await(5L, TimeUnit.SECONDS))
            assertFalse(graph.flushForShutdown(1L))
            graph.clear()
            assertEquals(RuntimeGraphStorageBudget.INITIAL_PRODUCER_BYTES, graph.storageUsedForTest())
            release.countDown()
            producer.join(5_000L)
            consumer.join(5_000L)
            assertFalse(producer.isAlive)
            assertFalse(consumer.isAlive)
            failure.get()?.let { throw AssertionError(it) }
            assertEquals(0L, graph.storageUsedForTest())
        } finally {
            release.countDown()
            producer.join(5_000L)
            graph.flushForShutdown(5_000L)
            graph.clear()
            writer.close()
            root.deleteRecursively()
        }
    }

    @Test
    fun pageDeniedBeforePublicationLeavesNoSequenceHoleAndCanReuseADrainedPage() {
        val budget = RuntimeGraphStorageBudget(RuntimeGraphStorageBudget.INITIAL_PRODUCER_BYTES +
            RuntimeGraphStorageBudget.PAGE_BYTES)
        assertTrue(budget.tryReserve(RuntimeGraphStorageBudget.INITIAL_PRODUCER_BYTES))
        val buffer = RuntimeGraphAggregateBuffer(Thread.currentThread(), budget)
        assertEquals(RUNTIME_GRAPH_ADD_AGGREGATED, buffer.tryAdd(1L, "caller", 2L, "callee", null, 0L, 1L))
        assertTrue(buffer.publishActivePage())
        assertEquals(RUNTIME_GRAPH_ADD_FULL, buffer.tryAdd(1L, "caller", 3L, "callee", null, 0L, 1L))
        val first = buffer.tryClaimConsumer()
        val page = buffer.pageAt(first)
        assertEquals(1L, page.logicalEventCount())
        buffer.release(first)
        buffer.trimEmptyPages()
        assertEquals(RUNTIME_GRAPH_ADD_AGGREGATED, buffer.tryAdd(1L, "caller", 3L, "callee", null, 0L, 2L))
        assertTrue(buffer.publishActivePage())
        val second = buffer.tryClaimConsumer()
        assertEquals(first + 1L, second)
        assertSame(page, buffer.pageAt(second))
        assertEquals(1L, page.logicalEventCount())
        assertEquals(3L, page.callees[page.nextOccupiedIndex(0)])
        buffer.release(second)
        buffer.releaseStorage()
        budget.release(RuntimeGraphStorageBudget.INITIAL_PRODUCER_BYTES)
        budget.close()
        assertEquals(0L, budget.usedBytes())
    }
}
