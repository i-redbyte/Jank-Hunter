package io.jankhunter.runtime

import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeGenerationWireArtTest {
    @Test
    fun pendingGenerationQualityAndResumedEventsReachTheirOwnWriter() {
        val context = InstrumentationRegistry.getInstrumentation().context
        val directory = File(context.cacheDir, "generation-wire")
        directory.deleteRecursively()
        val writers = List(3) { index ->
            AsyncLogWriterFactory().open(File(directory, "$index"),
                JankHunterConfig.builder().autoStartCollectors(false).build(), "main")
        }
        val entered = List(4) { CountDownLatch(1) }
        val release = List(4) { CountDownLatch(1) }
        val nextHook = AtomicInteger()
        val nextGraph = AtomicInteger()
        val observer = {
            val slot = Thread.currentThread().name.substringAfterLast('-').toInt()
            if (slot < 4) {
                entered[slot].countDown()
                check(release[slot].await(10L, TimeUnit.SECONDS))
            }
        }
        val hooks = RuntimeHookEventTransport({ 16 }, { 16 }, consumerLoopObserver = observer,
            consumerThreadFactory = { task, name -> Thread(task, "$name-${nextHook.getAndIncrement() * 2}") })
        val graph = RuntimeCallGraph({ 1L }, { "NewScreen" }, { 0L }, { 16 }, consumerLoopObserver = observer,
            consumerThreadFactory = { task, name -> Thread(task, "$name-${nextGraph.getAndIncrement() * 2 + 1}") })
        fun stop(index: Int) {
            hooks.stopAndFlush(1L)
            graph.flushForShutdown(1L)
            val drain = RuntimeWriterDrain(writers[index], 2_000L)
            hooks.whenWriterDrained(writers[index], drain::complete)
            graph.whenWriterDrained(writers[index], drain::complete)
            hooks.clear()
            graph.clear()
        }
        try {
            repeat(2) { index ->
                hooks.start(writers[index])
                graph.resetFlushState(writers[index])
                assertTrue(entered[index * 2].await(2L, TimeUnit.SECONDS))
                assertTrue(entered[index * 2 + 1].await(2L, TimeUnit.SECONDS))
                assertTrue(hooks.recordMethod(100L + index, "old-generation-$index"))
                graph.recordSemantic(101L, "old-root", 102L, "old-child", 1L, true)
                stop(index)
            }
            hooks.start(writers[2])
            graph.resetFlushState(writers[2])
            assertFalse(hooks.recordMethod(303L, "waiting-method"))
            assertFalse(hooks.recordLogSpam("WaitingScreen", "Owner", "source", 3))
            assertEquals(0L, graph.enter(304L, "waiting-entry", true))
            graph.recordSemantic(301L, "new-root", 302L, "new-child", 1L, true)
            assertTrue(hooks.flushBlocking(1L))
            assertTrue(graph.flushBlocking(1L))
            release[0].countDown()
            release[1].countDown()
            val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(3L)
            while ((!hooks.acceptingPublishersForTest() || !graph.acceptingPublishersForTest()) && System.nanoTime() < deadline) Thread.yield()
            assertTrue(hooks.recordMethod(303L, "new-generation-only"))
            assertTrue(graph.acceptingPublishersForTest())
            graph.recordSemantic(301L, "new-root", 302L, "new-child", 7L, true)
            assertTrue(hooks.flushBlocking(2_000L))
            assertTrue(graph.flushBlocking(2_000L))
            assertEquals(2L, generationQuality(writers[2], 0x203a))
            assertEquals(1L, generationQuality(writers[2], 0x203b))
            assertEquals(1L, generationQuality(writers[2], 0x203c))
            assertEquals(2L, generationQuality(writers[2], 0x201b))
            stop(2)
            release.forEach(CountDownLatch::countDown)
            val closedBy = System.nanoTime() + TimeUnit.SECONDS.toNanos(3L)
            while (writers.any { it.isAcceptingEvents() } && System.nanoTime() < closedBy) Thread.yield()
            assertTrue(writers.none { it.isAcceptingEvents() })
            writers.forEach { assertTrue(it.close(2_000L)) }
            File(directory, "2").walkTopDown().single { it.extension == "jhlog" }
                .copyTo(File(context.filesDir, "runtime-generation-5.1.0.jhlog"), overwrite = true)
        } finally {
            release.forEach(CountDownLatch::countDown)
            hooks.stopAndFlush(2_000L)
            graph.flushForShutdown(2_000L)
            hooks.clear()
            graph.clear()
            writers.forEach { it.close() }
            directory.deleteRecursively()
        }
    }
}
