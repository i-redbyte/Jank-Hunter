package io.jankhunter.runtime

import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeCrashFlushHandlerTest {
    @Test
    fun everyStageReceivesOnlyTheRemainingSharedBudget() {
        var now = 0L
        val calls = mutableListOf<Pair<String, Long>>()
        val diagnostics = mutableListOf<String>()
        val failure = IllegalStateException("original")
        var forwarded = 0
        fun stage(name: String): (Long) -> Boolean = { timeout ->
            calls.add(name to timeout)
            now += 25_000_000L
            true
        }
        val handler = RuntimeCrashFlushHandler(
            { thread, original -> assertSame(Thread.currentThread(), thread); assertSame(failure, original); forwarded++ },
            { diagnostics.add(it) }, stage("metrics"), stage("hooks"), stage("graph"), stage("writer"), { now },
        )
        handler.uncaughtException(Thread.currentThread(), failure)
        assertEquals(listOf("metrics" to 100L, "hooks" to 75L, "graph" to 50L, "writer" to 25L), calls)
        assertEquals(listOf("jankhunter.runtime.crash.count"), diagnostics)
        assertEquals(1, forwarded)
    }

    @Test
    fun exhaustedDeadlineSkipsFurtherBlockingStagesAndNamesEachIncompleteDrain() {
        var now = 0L
        val calls = mutableListOf<String>()
        val diagnostics = mutableListOf<String>()
        val handler = RuntimeCrashFlushHandler(
            { _, _ -> }, { diagnostics.add(it) },
            { calls.add("metrics"); now = 100_000_000L; false },
            { calls.add("hooks"); true }, { calls.add("graph"); true }, { calls.add("writer"); true }, { now },
        )
        handler.uncaughtException(Thread.currentThread(), IllegalStateException())
        assertEquals(listOf("metrics"), calls)
        assertEquals(listOf("jankhunter.runtime.crash.count") +
            listOf("metrics", "hooks", "graph", "writer").map { "jankhunter.runtime.crash_flush.incomplete.$it.count" }, diagnostics)
    }

    @Test
    fun oneFailedStageDoesNotPreventOtherAvailableBuffersFromDraining() {
        val diagnostics = mutableListOf<String>()
        val calls = mutableListOf<String>()
        val handler = RuntimeCrashFlushHandler(
            { _, _ -> }, { diagnostics.add(it) },
            { calls.add("metrics"); error("failed stage") },
            { calls.add("hooks"); true }, { calls.add("graph"); true }, { calls.add("writer"); true }, { 0L },
        )
        handler.uncaughtException(Thread.currentThread(), IllegalStateException())
        assertEquals(listOf("metrics", "hooks", "graph", "writer"), calls)
        assertEquals(listOf("jankhunter.runtime.crash.count", "jankhunter.runtime.crash_flush.incomplete.metrics.count"), diagnostics)
    }

    @Test
    fun concurrentCrashDoesNotStartAnotherDrainButBothOriginalFailuresAreForwarded() {
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val drains = AtomicInteger()
        val forwarded = AtomicInteger()
        val executor = Executors.newSingleThreadExecutor()
        val handler = RuntimeCrashFlushHandler(
            { _, _ -> forwarded.incrementAndGet() }, {}, { true }, { true }, { true },
            {
                if (drains.incrementAndGet() == 1) {
                    entered.countDown()
                    assertTrue(release.await(2L, TimeUnit.SECONDS))
                }
                true
            },
        )
        try {
            val first = executor.submit { handler.uncaughtException(Thread.currentThread(), IllegalStateException("first")) }
            assertTrue(entered.await(1L, TimeUnit.SECONDS))
            handler.uncaughtException(Thread.currentThread(), IllegalStateException("second"))
            release.countDown()
            first.get(1L, TimeUnit.SECONDS)
            assertEquals(1, drains.get())
            assertEquals(2, forwarded.get())
        } finally {
            release.countDown()
            executor.shutdown()
            assertTrue(executor.awaitTermination(2L, TimeUnit.SECONDS))
        }
    }

    @Test
    fun expiredBlockingBudgetStillRequestsANonblockingWriterFlush() {
        var now = 0L
        var requests = 0
        val handler = RuntimeCrashFlushHandler(
            { _, _ -> }, {}, { now = 100_000_000L; false }, { true }, { true }, { true }, { now },
            requestWriterFlush = { requests++ },
        )
        handler.uncaughtException(Thread.currentThread(), IllegalStateException())
        assertEquals(1, requests)
    }

    @Test
    fun missingPreviousHandlerRethrowsTheOriginalFailure() {
        val original = IllegalStateException("original")
        val handler = RuntimeCrashFlushHandler(null, {}, { true }, { true }, { true }, { true })
        assertSame(original, assertThrows(IllegalStateException::class.java) {
            handler.uncaughtException(Thread.currentThread(), original)
        })
    }
}
