package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeHookRestartTest {
    @Test
    fun replacementCollectsWhileOldConsumerIsStillStopping() {
        val directory = Files.createTempDirectory("jankhunter-hook-restart").toFile()
        val config = JankHunterConfig.builder().autoStartCollectors(false).build()
        val writerA = AsyncLogWriterFactory().open(directory.resolve("a"), config, "main")
        val writerB = AsyncLogWriterFactory().open(directory.resolve("b"), config, "main")
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val blockOnce = AtomicBoolean(true)
        val hooks = RuntimeHookEventTransport({ 16 }, { 16 }, consumerLoopObserver = {
            if (blockOnce.compareAndSet(true, false)) {
                entered.countDown()
                check(release.await(5L, TimeUnit.SECONDS))
            }
        })
        try {
            hooks.start(writerA)
            assertTrue(entered.await(2L, TimeUnit.SECONDS))
            assertTrue(hooks.recordMethod(1L, "old-session"))
            val oldConsumer = checkNotNull(hooks.consumerForTest())
            assertFalse(hooks.stopAndFlush(1L))
            hooks.clear()
            hooks.start(writerB)
            assertTrue("replacement must collect before the previous consumer leaves", hooks.acceptingPublishersForTest())
            assertTrue(hooks.recordMethod(2L, "new-session"))
            assertTrue(hooks.flushBlocking(2_000L))
            assertEquals(1L, hooks.acceptedForTest())
            assertEquals(1L, hooks.emittedForTest())
            assertEquals(0L, hooks.acceptedLossForTest())
            release.countDown()
            oldConsumer.join(2_000L)
            assertFalse(oldConsumer.isAlive)
            assertTrue(hooks.recordMethod(3L, "new-session-after-old-cleanup"))
            assertTrue(hooks.flushBlocking(2_000L))
            assertEquals(2L, hooks.acceptedForTest())
            assertEquals(2L, hooks.emittedForTest())
        } finally {
            release.countDown()
            hooks.stopAndFlush(2_000L)
            hooks.clear()
            writerA.close()
            writerB.close()
            directory.deleteRecursively()
        }
    }

    @Test
    fun thirdGenerationWaitsForOneRetiredOwnerAndThenStartsAutomatically() = withBlockedPair { fixture ->
        fixture.hooks.start(fixture.writers[2])
        assertEquals("do not create a third blocked consumer", 2, fixture.created.get())
        assertFalse(fixture.hooks.recordMethod(3L, "waiting-generation"))
        assertTrue(fixture.hooks.flushBlocking(1L))
        assertEquals(1L, generationQuality(fixture.writers[2], 0x203a))
        fixture.release[0].countDown()
        fixture.awaitReplacement()
        assertTrue(fixture.hooks.recordMethod(4L, "resumed-generation"))
        assertTrue(fixture.hooks.flushBlocking(2_000L))
        assertEquals(3, fixture.created.get())
        assertEquals(1L, generationQuality(fixture.writers[2], 0x203a))
        assertEquals(1L, fixture.hooks.acceptedForTest())
        assertEquals(1L, fixture.hooks.emittedForTest())
    }

    @Test
    fun replacingPendingStartActivatesOnlyTheLatestWriter() = withBlockedPair { fixture ->
        fixture.hooks.start(fixture.writers[2])
        fixture.hooks.clear()
        fixture.writers[2].close()
        fixture.hooks.start(fixture.writers[3])
        assertEquals(2, fixture.created.get())
        fixture.release[0].countDown()
        fixture.awaitReplacement()
        assertTrue(fixture.hooks.recordMethod(4L, "latest-generation"))
        assertTrue(fixture.hooks.flushBlocking(2_000L))
        assertEquals(1L, fixture.hooks.emittedForTest())
        assertEquals(0L, fixture.hooks.acceptedLossForTest())
        assertEquals(3, fixture.created.get())
    }

    @Test
    fun clearingPendingStartDoesNotResurrectCollection() = withBlockedPair { fixture ->
        fixture.hooks.start(fixture.writers[2])
        fixture.hooks.clear()
        fixture.release.forEach(CountDownLatch::countDown)
        fixture.threads.forEach { it.join(2_000L); assertFalse(it.isAlive) }
        assertEquals(2, fixture.created.get())
        assertFalse(fixture.hooks.acceptingPublishersForTest())
        assertFalse(fixture.hooks.recordMethod(4L, "must-not-resurrect"))
    }

    private fun withBlockedPair(block: (BlockedPair) -> Unit) {
        val fixture = BlockedPair()
        try {
            repeat(2) { index ->
                fixture.hooks.start(fixture.writers[index])
                assertTrue(fixture.entered[index].await(2L, TimeUnit.SECONDS))
                assertFalse(fixture.hooks.stopAndFlush(1L))
                fixture.hooks.clear()
            }
            block(fixture)
        } finally {
            fixture.release.forEach(CountDownLatch::countDown)
            fixture.hooks.stopAndFlush(2_000L)
            fixture.hooks.clear()
            fixture.threads.forEach { it.join(2_000L) }
            fixture.writers.forEach { it.close() }
            fixture.directory.deleteRecursively()
        }
    }

    private class BlockedPair {
        val directory = Files.createTempDirectory("jankhunter-hook-generations").toFile()
        val writers = List(4) { index ->
            AsyncLogWriterFactory().open(
                directory.resolve(index.toString()), JankHunterConfig.builder().autoStartCollectors(false).build(), "main",
            )
        }
        val created = AtomicInteger()
        val entered = List(2) { CountDownLatch(1) }
        val release = List(2) { CountDownLatch(1) }
        val threads = java.util.concurrent.CopyOnWriteArrayList<Thread>()
        val hooks = RuntimeHookEventTransport({ 16 }, { 16 },
            consumerThreadFactory = { task, name ->
                Thread(task, "$name-${created.getAndIncrement()}").also(threads::add)
            },
            consumerLoopObserver = {
                val index = Thread.currentThread().name.substringAfterLast('-').toInt()
                if (index < 2) {
                    entered[index].countDown()
                    check(release[index].await(5L, TimeUnit.SECONDS))
                }
            },
        )

        fun awaitReplacement() {
            val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(2L)
            while (!hooks.acceptingPublishersForTest() && System.nanoTime() < deadline) Thread.yield()
            assertTrue("deferred replacement did not start automatically", hooks.acceptingPublishersForTest())
        }
    }
}
