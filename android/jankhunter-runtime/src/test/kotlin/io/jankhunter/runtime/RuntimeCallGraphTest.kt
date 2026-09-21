package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.locks.LockSupport
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeCallGraphTest {
    @Test
    fun currentCallSiteTracksTopFrameWithoutMutatingStack() = withGraph { graph ->
        assertEquals(0L, graph.currentMethodId())
        assertEquals(null, graph.currentMethodName())
        assertFalse(graph.hasCurrentMethod())

        val parent = graph.enter(11L, "FeedPresenter.refresh", enabled = true)
        assertEquals(11L, graph.currentMethodId())
        assertEquals("FeedPresenter.refresh", graph.currentMethodName())
        assertTrue(graph.hasCurrentMethod())

        val child = graph.enter(-22L, "FeedRepository.load", enabled = true)
        assertEquals(-22L, graph.currentMethodId())
        assertEquals("FeedRepository.load", graph.currentMethodName())
        assertEquals(2, graph.currentThreadDepthForTest())

        graph.exit(child, -22L)
        assertEquals(11L, graph.currentMethodId())
        assertEquals("FeedPresenter.refresh", graph.currentMethodName())
        graph.exit(parent, 11L)
        assertEquals(0L, graph.currentMethodId())
        assertEquals(null, graph.currentMethodName())
    }

    @Test
    fun exitPopsPrimitiveStack() = withGraph { graph ->
        val parent = graph.enter(0L, enabled = true)
        val child = graph.enter(-1L, enabled = true)

        graph.exit(child, -1L)
        assertEquals(1, graph.currentThreadDepthForTest())

        graph.exit(parent, 0L)
        assertEquals(0, graph.currentThreadDepthForTest())
    }

    @Test
    fun stoppedEpochInvalidatesStaleTokenAndStack() = withGraph { graph ->
        val stale = graph.enter(42L, enabled = true)

        graph.flushForShutdown()
        graph.clear()

        assertEquals(0, graph.currentThreadDepthForTest())
        assertEquals(0L, graph.enter(42L, enabled = true))
        graph.exit(stale, 42L)
    }

    @Test
    fun stoppedGraphDoesNotExposeStaleCallSite() {
        val directory = Files.createTempDirectory("jankhunter-runtime-call-graph-stale-context").toFile()
        val writer = writer(directory)
        val graph = graph()
        graph.resetFlushState(writer)
        try {
            graph.enter(42L, enabled = true)
            assertTrue(graph.hasCurrentMethod())

            graph.flushForShutdown()

            assertFalse(graph.hasCurrentMethod())
            assertEquals(0L, graph.currentMethodId())
            assertEquals(null, graph.currentMethodName())
        } finally {
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    @Test
    fun zeroAndNegativeStableIdsArePublished() = withGraph { graph ->
        val parent = graph.enter(0L, enabled = true)
        val child = graph.enter(Long.MIN_VALUE, enabled = true)

        graph.exit(child, Long.MIN_VALUE)
        graph.exit(parent, 0L)

        assertEquals(1L, graph.acceptedForTest())
        assertTrue(graph.flushBlocking(2_000L))
        assertEquals(1L, graph.emittedForTest())
        assertEquals(0L, graph.acceptedEventLossForTest())
    }

    @Test
    fun semanticRootIsPublishedWithoutAnOrdinaryCallStack() = withGraph { graph ->
        repeat(250) {
            graph.recordSemantic(
                callerId = 11L,
                callerName = "jankhunter.semantic.v1.compose.composition.main",
                calleeId = 12L,
                calleeName = "example.FeedScreen",
                durationMs = 3L,
                enabled = true,
            )
        }

        assertEquals(250L, graph.acceptedForTest())
        assertTrue(graph.flushBlocking(2_000L))
        assertEquals(250L, graph.emittedForTest())
        assertTrue(graph.aggregatedEdgeKeysForTest() < 4L)
        assertEquals(0L, graph.acceptedEventLossForTest())
    }

    @Test
    fun disabledSemanticRootDoesNotPublish() = withGraph { graph ->
        graph.recordSemantic(1L, "root", 2L, "callee", 1L, enabled = false)

        assertEquals(0L, graph.acceptedForTest())
    }

    @Test
    fun nonLifoExitDoesNotPublishFalseEdge() = withGraph { graph ->
        val parent = graph.enter(1L, enabled = true)
        graph.enter(2L, enabled = true)

        graph.exit(parent, 1L)

        assertEquals(0L, graph.acceptedForTest())
        assertEquals(0, graph.currentThreadDepthForTest())
    }

    @Test
    fun contextIsCapturedAtCalleeEntry() = withGraph(
        initialScreen = "screen-a",
    ) { graph, screen ->
        val parent = graph.enter(1L, enabled = true)
        val childA = graph.enter(2L, enabled = true)
        screen.set("screen-b")
        graph.exit(childA, 2L)
        val childB = graph.enter(2L, enabled = true)
        graph.exit(childB, 2L)
        graph.exit(parent, 1L)

        assertEquals(2L, graph.acceptedForTest())
        assertTrue(graph.flushBlocking(2_000L))
        assertEquals(2L, graph.aggregatedEdgeKeysForTest())
        assertEquals(2L, graph.emittedForTest())
        assertEquals(0L, graph.acceptedEventLossForTest())
    }

    @Test
    fun producersMakeProgressWithExactAccounting() = withGraph { graph ->
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
                        val parentId = producer.toLong() shl 32
                        val childId = event.toLong() and 7L
                        val parent = graph.enter(parentId, enabled = true)
                        val child = graph.enter(childId, enabled = true)
                        graph.exit(child, childId)
                        graph.exit(parent, parentId)
                    }
                    done.countDown()
                }
            }
            start.countDown()
            assertTrue("producer progress timed out", done.await(10, TimeUnit.SECONDS))
            assertTrue(graph.flushBlocking(5_000L))
            val expected = producerCount.toLong() * eventsPerProducer
            assertEquals(expected, graph.attemptedForTest())
            assertEquals(expected, graph.emittedForTest())
            assertEquals(0L, graph.acceptedEventLossForTest())
        } finally {
            pool.shutdownNow()
        }
    }

    @Test
    fun repeatedCallsHaveExactLogicalCount() = withGraph { graph ->
        repeat(10_000) {
            val parent = graph.enter(1L, enabled = true)
            val child = graph.enter(2L, enabled = true)
            graph.exit(child, 2L)
            graph.exit(parent, 1L)
        }
        assertTrue(graph.flushBlocking(5_000L))
        assertEquals(10_000L, graph.acceptedForTest())
        assertEquals(10_000L, graph.emittedForTest())
        assertTrue(graph.aggregatedEdgeKeysForTest() < 16L)
        assertEquals(0L, graph.backpressureCountForTest())
        assertEquals(0L, graph.acceptedEventLossForTest())
    }

    @Test
    fun periodicFlushKeepsEdgesAggregatedAcrossFormerFiveSecondBoundary() {
        val directory = Files.createTempDirectory("jankhunter-runtime-call-graph-window").toFile()
        val writer = writer(directory)
        val now = AtomicLong(0L)
        val consumerStarted = CountDownLatch(1)
        val observedAfterFormerBoundary = CountDownLatch(1)
        val emitted = CountDownLatch(1)
        val graph = RuntimeCallGraph(
            nowMs = now::get,
            captureScreen = { "screen" },
            captureOperationId = { 41L },
            maxKeys = { 128 },
            periodicFlushIntervalMs = 30_000L,
            uptimeNanos = { TimeUnit.MILLISECONDS.toNanos(now.get()) },
            batchObserver = { emitted.countDown() },
            consumerLoopObserver = {
                if (now.get() == 0L) consumerStarted.countDown()
                if (now.get() == 6_000L) observedAfterFormerBoundary.countDown()
            },
        )
        graph.resetFlushState(writer)
        try {
            assertTrue("runtime graph consumer did not start", consumerStarted.await(2L, TimeUnit.SECONDS))
            graph.recordEdge(1L, 2L)
            now.set(6_000L)
            LockSupport.unpark(graph.consumerForTest())
            assertTrue(
                "consumer did not observe the former five-second boundary",
                observedAfterFormerBoundary.await(2L, TimeUnit.SECONDS),
            )
            assertFalse("runtime edge flushed at the former boundary", emitted.await(100L, TimeUnit.MILLISECONDS))

            now.set(30_000L)
            LockSupport.unpark(graph.consumerForTest())
            assertTrue("runtime edge did not flush at the bounded window", emitted.await(2L, TimeUnit.SECONDS))
            val admissionDeadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(2L)
            while (graph.emittedForTest() < 1L && System.nanoTime() < admissionDeadline) Thread.yield()
            assertEquals(1L, graph.emittedForTest())
            assertEquals(0L, graph.acceptedEventLossForTest())
        } finally {
            graph.flushForShutdown()
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    @Test
    fun aggregateCapacityIsAFlushThresholdInsteadOfALossLimit() = withGraph(maxKeys = 1) { graph, _ ->
        repeat(512) { graph.recordEdge(1L, it.toLong() + 2L) }
        assertTrue(graph.flushBlocking(5_000L))
        assertEquals(512L, graph.attemptedForTest())
        assertEquals(512L, graph.emittedForTest())
        assertEquals(0L, graph.acceptedEventLossForTest())
        assertEquals(512L, graph.aggregatedEdgeKeysForTest())
    }

    @Test
    fun producerPageDeadlineLossIsCountedExactly() {
        val directory = Files.createTempDirectory("jankhunter-runtime-call-graph-capacity").toFile()
        val writer = writer(directory)
        val consumerEntered = CountDownLatch(1)
        val releaseConsumer = CountDownLatch(1)
        val graph = RuntimeCallGraph(
            nowMs = { 1L },
            captureScreen = { "screen" },
            captureOperationId = { 41L },
            maxKeys = { 4_096 },
            admissionWaitNanos = { 0L },
            consumerLoopObserver = {
                consumerEntered.countDown()
                assertTrue("consumer release timed out", releaseConsumer.await(5L, TimeUnit.SECONDS))
            },
        )
        graph.resetFlushState(writer)
        try {
            assertTrue("runtime graph consumer did not start", consumerEntered.await(5L, TimeUnit.SECONDS))
            val capacity = RUNTIME_GRAPH_PAGE_QUEUE_CAPACITY * RUNTIME_GRAPH_PAGE_MAX_KEYS
            repeat(capacity + 1) { index ->
                graph.recordSemantic(
                    callerId = 1L,
                    callerName = "caller",
                    calleeId = index.toLong() + 2L,
                    calleeName = "callee",
                    durationMs = 1L,
                    enabled = true,
                )
            }
            assertEquals(capacity.toLong() + 1L, graph.attemptedForTest())
            assertEquals(capacity.toLong(), graph.acceptedForTest())
            assertEquals(1L, graph.producerCapacityLossForTest())
        } finally {
            releaseConsumer.countDown()
            graph.flushForShutdown()
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    @Test
    fun shutdownTerminatesDedicatedDaemonConsumer() {
        val directory = Files.createTempDirectory("jankhunter-runtime-call-graph").toFile()
        val writer = writer(directory)
        val graph = graph()
        graph.resetFlushState(writer)
        val consumer = graph.consumerForTest()
        assertEquals("JankHunterGraph", consumer?.name)
        assertTrue(consumer?.isDaemon == true)

        graph.flushForShutdown()

        assertFalse(consumer?.isAlive == true)
        writer.close()
        directory.deleteRecursively()
    }

    @Test
    fun exactShutdownDrainsExitAdmittedAtTheShutdownBoundary() {
        val directory = Files.createTempDirectory("jankhunter-runtime-call-graph-frontier").toFile()
        val writer = writer(directory)
        val admitted = CountDownLatch(1)
        val release = CountDownLatch(1)
        val admissions = AtomicInteger()
        val graph = graph(
            publisherAdmissionObserver = {
                if (admissions.incrementAndGet() == 1) {
                    admitted.countDown()
                    assertTrue("publisher release timed out", release.await(5L, TimeUnit.SECONDS))
                }
            },
        )
        val executor = Executors.newFixedThreadPool(2)
        graph.resetFlushState(writer)
        try {
            val publisher = executor.submit {
                val parent = graph.enter(1L, enabled = true)
                val child = graph.enter(2L, enabled = true)
                graph.exit(child, 2L)
                graph.exit(parent, 1L)
            }
            assertTrue("publisher was not admitted", admitted.await(5L, TimeUnit.SECONDS))
            val shutdown = executor.submit { graph.flushForShutdown() }
            awaitPublisherGateClosed(graph)

            release.countDown()

            publisher.get(5L, TimeUnit.SECONDS)
            shutdown.get(5L, TimeUnit.SECONDS)
            assertEquals(1L, graph.attemptedForTest())
            assertEquals(1L, graph.acceptedForTest())
            assertEquals(1L, graph.emittedForTest())
            assertEquals(0L, graph.acceptedEventLossForTest())
        } finally {
            release.countDown()
            executor.shutdownNow()
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    @Test
    fun consumerFailureClosesAdmissionAndAccountsForActivePage() {
        val directory = Files.createTempDirectory("jankhunter-runtime-call-graph-consumer-failure").toFile()
        val writer = writer(directory)
        val consumerEntered = CountDownLatch(1)
        val failConsumer = CountDownLatch(1)
        val graph = graph(
            consumerLoopObserver = {
                consumerEntered.countDown()
                assertTrue("consumer failure trigger timed out", failConsumer.await(5L, TimeUnit.SECONDS))
                error("injected consumer failure")
            },
        )
        graph.resetFlushState(writer)
        try {
            assertTrue("consumer did not start", consumerEntered.await(5L, TimeUnit.SECONDS))
            val parent = graph.enter(1L, enabled = true)
            val child = graph.enter(2L, enabled = true)
            graph.exit(child, 2L)
            graph.exit(parent, 1L)

            failConsumer.countDown()
            awaitConsumerStopped(graph)

            assertFalse(graph.acceptingPublishersForTest())
            assertEquals(0L, graph.enter(3L, enabled = true))
            assertEquals(1L, graph.acceptedForTest())
            assertEquals(0L, graph.emittedForTest())
            assertEquals(1L, graph.acceptedEventLossForTest())
            assertEquals(0, graph.registeredProducerCountForTest())
        } finally {
            failConsumer.countDown()
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    @Test
    fun consumerFailureWithoutAcceptedEventsDoesNotInventLoss() {
        val directory = Files.createTempDirectory("jankhunter-runtime-call-graph-empty-failure").toFile()
        val writer = writer(directory)
        val graph = graph(consumerLoopObserver = { error("injected consumer failure") })
        graph.resetFlushState(writer)
        try {
            awaitConsumerStopped(graph)

            assertEquals(0L, graph.acceptedForTest())
            assertEquals(0L, graph.emittedForTest())
            assertEquals(0L, graph.acceptedEventLossForTest())
            assertEquals(0, graph.registeredProducerCountForTest())
            assertFalse(graph.flushBlocking(10L))
        } finally {
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    @Test
    fun consumerStartFailureClosesAdmissionAndReleasesLifecycleState() {
        val directory = Files.createTempDirectory("jankhunter-runtime-call-graph-start-failure").toFile()
        val writer = writer(directory)
        val failure = IllegalStateException("injected thread start failure")
        lateinit var graph: RuntimeCallGraph
        graph = RuntimeCallGraph(
            nowMs = { 1L },
            captureScreen = { "screen" },
            captureOperationId = { 41L },
            maxKeys = { 128 },
            consumerThreadFactory = { runnable, name ->
                object : Thread(runnable, name) {
                    override fun start() {
                        graph.recordSemantic(1L, "root", 2L, "callee", 1L, enabled = true)
                        throw failure
                    }
                }
            },
        )
        try {
            assertSame(failure, assertThrows(IllegalStateException::class.java) { graph.resetFlushState(writer) })

            assertEquals(null, graph.consumerForTest())
            assertFalse(graph.acceptingPublishersForTest())
            assertFalse(graph.flushBlocking(1L))
            assertEquals(1L, graph.acceptedForTest())
            assertEquals(1L, graph.acceptedEventLossForTest())
            assertEquals(0, graph.registeredProducerCountForTest())
        } finally {
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    @Test
    fun methodEntryCannotRegisterProducerAfterConcurrentClear() {
        val directory = Files.createTempDirectory("jankhunter-runtime-call-graph-late-registration").toFile()
        val writer = writer(directory)
        val registrationEntered = CountDownLatch(1)
        val releaseRegistration = CountDownLatch(1)
        val graph = RuntimeCallGraph(
            nowMs = { 1L },
            captureScreen = { "screen" },
            captureOperationId = { 41L },
            maxKeys = { 128 },
            producerRegistrationObserver = {
                registrationEntered.countDown()
                assertTrue("producer registration release timed out", releaseRegistration.await(5L, TimeUnit.SECONDS))
            },
        )
        val executor = Executors.newSingleThreadExecutor()
        graph.resetFlushState(writer)
        try {
            val publisher = executor.submit<Long> { graph.enter(1L, "method", enabled = true) }
            assertTrue("producer did not reach registration", registrationEntered.await(5L, TimeUnit.SECONDS))

            graph.clear()
            awaitConsumerStopped(graph)
            releaseRegistration.countDown()

            assertEquals(0L, publisher.get(5L, TimeUnit.SECONDS))
            assertEquals(0, graph.registeredProducerCountForTest())
        } finally {
            releaseRegistration.countDown()
            executor.shutdownNow()
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    @Test
    fun consumerDoesNotSwallowFatalVmFailure() {
        val directory = Files.createTempDirectory("jankhunter-runtime-call-graph-fatal-consumer").toFile()
        val writer = writer(directory)
        val consumerEntered = CountDownLatch(1)
        val releaseConsumer = CountDownLatch(1)
        val uncaught = CountDownLatch(1)
        val failure = AtomicReference<Throwable>()
        val fatal = FatalConsumerError()
        val graph = graph(
            consumerLoopObserver = {
                consumerEntered.countDown()
                releaseConsumer.await(5L, TimeUnit.SECONDS)
                throw fatal
            },
        )
        graph.resetFlushState(writer)
        try {
            assertTrue(consumerEntered.await(5L, TimeUnit.SECONDS))
            checkNotNull(graph.consumerForTest()).uncaughtExceptionHandler =
                Thread.UncaughtExceptionHandler { _, throwable ->
                    failure.set(throwable)
                    uncaught.countDown()
                }
            releaseConsumer.countDown()

            assertTrue("fatal consumer failure was swallowed", uncaught.await(5L, TimeUnit.SECONDS))
            assertSame(fatal, failure.get())
        } finally {
            releaseConsumer.countDown()
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    @Test
    fun consumerFailureAccountsForBatchAlreadyRemovedFromAggregateTable() {
        val directory = Files.createTempDirectory("jankhunter-runtime-call-graph-batch-failure").toFile()
        val writer = writer(directory)
        val graph = RuntimeCallGraph(
            nowMs = { 1L },
            captureScreen = { "screen" },
            captureOperationId = { 41L },
            maxKeys = { 128 },
            batchObserver = { error("injected batch hand-off failure") },
        )
        graph.resetFlushState(writer)
        try {
            repeat(7) {
                graph.recordSemantic(1L, "root", 2L, "callee", 1L, enabled = true)
            }

            graph.flushForShutdown()

            assertEquals(7L, graph.acceptedForTest())
            assertEquals(0L, graph.emittedForTest())
            assertEquals(7L, graph.acceptedEventLossForTest())
            assertFalse(graph.flushBlocking(10L))
        } finally {
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    @Test
    fun interruptedNonExactShutdownDoesNotDiscardConsumerStateDuringClear() {
        val directory = Files.createTempDirectory("jankhunter-runtime-call-graph-deferred-clear").toFile()
        val writer = writer(directory)
        val consumerEntered = CountDownLatch(1)
        val releaseConsumer = CountDownLatch(1)
        val observedEvents = AtomicLong()
        val graph = RuntimeCallGraph(
            nowMs = { 1L },
            captureScreen = { "screen" },
            captureOperationId = { 41L },
            maxKeys = { 128 },
            exactAdmission = { false },
            batchObserver = { batch -> observedEvents.addAndGet(batch.logicalEventCount()) },
            consumerLoopObserver = {
                consumerEntered.countDown()
                assertTrue("consumer release timed out", releaseConsumer.await(5L, TimeUnit.SECONDS))
            },
        )
        graph.resetFlushState(writer)
        val consumer = graph.consumerForTest()
        val shutdown = Thread(graph::flushForShutdown, "JankHunterInterruptedShutdown")
        try {
            assertTrue("consumer did not start", consumerEntered.await(5L, TimeUnit.SECONDS))
            repeat(7) {
                graph.recordSemantic(1L, "root", 2L, "callee", 1L, enabled = true)
            }

            shutdown.start()
            awaitPublisherGateClosed(graph)
            shutdown.interrupt()
            shutdown.join(2_000L)

            assertFalse("non-exact shutdown did not return after interruption", shutdown.isAlive)
            assertTrue("consumer unexpectedly stopped before release", consumer?.isAlive == true)
            assertEquals(0L, graph.acceptedEventLossForTest())

            graph.clear()
            assertTrue("clear hid the still-running consumer", graph.consumerForTest()?.isAlive == true)

            releaseConsumer.countDown()
            consumer?.join(5_000L)
            assertFalse("consumer did not finish deferred clear", consumer?.isAlive == true)
            assertEquals(7L, observedEvents.get())
            assertEquals(0, graph.registeredProducerCountForTest())
        } finally {
            releaseConsumer.countDown()
            shutdown.interrupt()
            shutdown.join(2_000L)
            consumer?.join(5_000L)
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    @Test
    fun exactShutdownHonorsTimeoutWithoutDiscardingConsumerStateDuringClear() {
        val directory = Files.createTempDirectory("jankhunter-runtime-call-graph-exact-timeout").toFile()
        val writer = writer(directory)
        val consumerEntered = CountDownLatch(1)
        val releaseConsumer = CountDownLatch(1)
        val observedEvents = AtomicLong()
        val graph = RuntimeCallGraph(
            nowMs = { 1L },
            captureScreen = { "screen" },
            captureOperationId = { 41L },
            maxKeys = { 128 },
            exactAdmission = { true },
            batchObserver = { batch -> observedEvents.addAndGet(batch.logicalEventCount()) },
            consumerLoopObserver = {
                consumerEntered.countDown()
                releaseConsumer.await()
            },
        )
        graph.resetFlushState(writer)
        val consumer = graph.consumerForTest()
        try {
            assertTrue("consumer did not start", consumerEntered.await(5L, TimeUnit.SECONDS))
            graph.recordSemantic(1L, "root", 2L, "callee", 1L, enabled = true)

            val startedAtNs = System.nanoTime()
            assertFalse(graph.flushForShutdown(timeoutMs = 1L))
            assertTrue(TimeUnit.NANOSECONDS.toMillis(System.nanoTime() - startedAtNs) < 1_000L)

            graph.clear()
            assertTrue("clear hid the still-running consumer", graph.consumerForTest()?.isAlive == true)

            releaseConsumer.countDown()
            consumer?.join(5_000L)
            assertFalse("consumer did not finish deferred clear", consumer?.isAlive == true)
            assertEquals(1L, observedEvents.get())
            assertEquals(0, graph.registeredProducerCountForTest())
        } finally {
            releaseConsumer.countDown()
            consumer?.join(5_000L)
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    @Test
    fun deferredShutdownClosesWriterOnlyAfterAcceptedGraphEventsAreWritten() {
        val directory = Files.createTempDirectory("jankhunter-runtime-call-graph-deferred-writer-close").toFile()
        val writer = writer(directory)
        val consumerEntered = CountDownLatch(1)
        val releaseConsumer = CountDownLatch(1)
        val graph = RuntimeCallGraph(
            nowMs = { 1L },
            captureScreen = { "screen" },
            captureOperationId = { 41L },
            maxKeys = { 128 },
            exactAdmission = { true },
            consumerLoopObserver = {
                consumerEntered.countDown()
                releaseConsumer.await()
            },
        )
        graph.resetFlushState(writer)
        val consumer = graph.consumerForTest()
        try {
            assertTrue("consumer did not start", consumerEntered.await(5L, TimeUnit.SECONDS))
            graph.recordSemantic(1L, "root", 2L, "callee", 1L, enabled = true)

            assertFalse(graph.flushForShutdown(timeoutMs = 1L))
            assertFalse(closeWhenGraphDrained(graph, writer, timeoutMs = 5_000L))
            assertTrue("writer closed before the graph drained", writer.isAcceptingEvents())

            releaseConsumer.countDown()
            consumer?.join(5_000L)

            assertFalse("graph consumer did not finish", consumer?.isAlive == true)
            assertEquals(1L, graph.emittedForTest())
            assertEquals(0L, graph.acceptedEventLossForTest())
            assertFalse("writer remained open after deferred graph shutdown", writer.isAcceptingEvents())
        } finally {
            releaseConsumer.countDown()
            consumer?.join(5_000L)
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    @Test
    fun deferredShutdownDoesNotTakeOwnershipOfUnrelatedWriter() {
        val directory = Files.createTempDirectory("jankhunter-runtime-call-graph-unrelated-writer").toFile()
        val graphWriter = writer(directory)
        val unrelatedWriter = writer(directory)
        val consumerEntered = CountDownLatch(1)
        val releaseConsumer = CountDownLatch(1)
        val graph = RuntimeCallGraph(
            nowMs = { 1L },
            captureScreen = { "screen" },
            captureOperationId = { 41L },
            maxKeys = { 128 },
            consumerLoopObserver = {
                consumerEntered.countDown()
                releaseConsumer.await()
            },
        )
        graph.resetFlushState(graphWriter)
        val consumer = graph.consumerForTest()
        try {
            assertTrue("consumer did not start", consumerEntered.await(5L, TimeUnit.SECONDS))
            assertFalse(graph.flushForShutdown(timeoutMs = 1L))
            assertFalse(closeWhenGraphDrained(graph, graphWriter, timeoutMs = 5_000L))

            assertTrue(closeWhenGraphDrained(graph, unrelatedWriter, timeoutMs = 5_000L))
            assertFalse("unrelated writer remained open", unrelatedWriter.isAcceptingEvents())

            releaseConsumer.countDown()
            consumer?.join(5_000L)
            assertFalse("graph writer remained open", graphWriter.isAcceptingEvents())
        } finally {
            releaseConsumer.countDown()
            consumer?.join(5_000L)
            graph.clear()
            graphWriter.close()
            unrelatedWriter.close()
            directory.deleteRecursively()
        }
    }

    @Test
    fun restartWaitsForDeferredConsumerShutdown() {
        val directory = Files.createTempDirectory("jankhunter-runtime-call-graph-deferred-restart").toFile()
        val writer = writer(directory)
        val consumerEntered = CountDownLatch(1)
        val releaseConsumer = CountDownLatch(1)
        val restartEntered = CountDownLatch(1)
        val graph = graph(
            consumerLoopObserver = {
                consumerEntered.countDown()
                releaseConsumer.await()
            },
        )
        val executor = Executors.newSingleThreadExecutor()
        graph.resetFlushState(writer)
        val firstConsumer = graph.consumerForTest()
        try {
            assertTrue("consumer did not start", consumerEntered.await(5L, TimeUnit.SECONDS))
            assertFalse(graph.flushForShutdown(timeoutMs = 1L))
            graph.clear()

            val restart = executor.submit {
                restartEntered.countDown()
                graph.resetFlushState(writer)
            }
            assertTrue("restart did not begin", restartEntered.await(5L, TimeUnit.SECONDS))
            releaseConsumer.countDown()

            restart.get(5L, TimeUnit.SECONDS)
            assertTrue("replacement consumer did not start", graph.consumerForTest()?.isAlive == true)
        } finally {
            releaseConsumer.countDown()
            firstConsumer?.join(5_000L)
            graph.flushForShutdown()
            graph.clear()
            executor.shutdownNow()
            writer.close()
            directory.deleteRecursively()
        }
    }

    private fun closeWhenGraphDrained(graph: RuntimeCallGraph, writer: AsyncLogWriter, timeoutMs: Long): Boolean {
        val closed = java.util.concurrent.atomic.AtomicBoolean()
        graph.whenWriterDrained(writer) { closed.set(writer.close(timeoutMs)) }
        return closed.get()
    }

    private fun withGraph(
        initialScreen: String = "screen",
        maxKeys: Int = 128,
        block: (RuntimeCallGraph, AtomicText) -> Unit,
    ) {
        val directory = Files.createTempDirectory("jankhunter-runtime-call-graph").toFile()
        val writer = writer(directory)
        val screen = AtomicText(initialScreen)
        val graph = graph(screen, maxKeys)
        graph.resetFlushState(writer)
        try {
            block(graph, screen)
        } finally {
            graph.flushForShutdown()
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    private fun withGraph(block: (RuntimeCallGraph) -> Unit) {
        withGraph { graph, _ -> block(graph) }
    }

    private fun graph(
        screen: AtomicText = AtomicText("screen"),
        maxKeys: Int = 128,
        publisherAdmissionObserver: (() -> Unit)? = null,
        consumerLoopObserver: (() -> Unit)? = null,
    ): RuntimeCallGraph {
        val now = AtomicLong(1L)
        return RuntimeCallGraph(
            nowMs = { now.getAndIncrement() },
            captureScreen = screen::get,
            captureOperationId = { 41L },
            maxKeys = { maxKeys },
            publisherAdmissionObserver = publisherAdmissionObserver,
            consumerLoopObserver = consumerLoopObserver,
        )
    }

    private fun awaitPublisherGateClosed(graph: RuntimeCallGraph) {
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(5L)
        while (graph.acceptingPublishersForTest()) {
            assertTrue("publisher gate did not close", System.nanoTime() < deadline)
            Thread.yield()
        }
    }

    private fun awaitConsumerStopped(graph: RuntimeCallGraph) {
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(5L)
        while (graph.consumerForTest()?.isAlive == true) {
            assertTrue("consumer did not stop", System.nanoTime() < deadline)
            Thread.yield()
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

    private class AtomicText(initial: String) {
        @Volatile
        private var value = initial

        fun get(): String = value

        fun set(updated: String) {
            value = updated
        }
    }

    private class FatalConsumerError : VirtualMachineError()
}
