package io.jankhunter.runtime

import io.jankhunter.runtime.internal.system.ObjectRetentionWatcher
import java.util.concurrent.CountDownLatch
import java.util.concurrent.locks.ReentrantLock
import kotlin.concurrent.withLock
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import org.junit.Assert.assertNotSame
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.assertSame
import org.junit.Assert.assertThrows
import org.junit.Test

class ObjectRetentionStopRegressionTest {
    @Test
    fun fatalGcFailureIsNotReportedAsAnOrdinaryUnsuccessfulCollection() {
        var now = 0L
        val failure = OutOfMemoryError("synthetic GC failure")
        val watcher = ObjectRetentionWatcher(
            retainedDelayMs = 1_000L, clock = { now }, forceGcBeforeReport = true,
            requestGc = { throw failure },
        )
        val retained = Any()
        enable(watcher)
        watcher.watch(retained, "example.Owner", null, null)
        now = 2_000L
        try {
            assertSame(failure, assertThrows(OutOfMemoryError::class.java) { watcher.checkRetained() })
        } finally {
            watcher.stop()
            java.lang.ref.Reference.reachabilityFence(retained)
        }
    }

    @Test
    fun stopDoesNotRunHeapDumpOnItsCallingThread() {
        var now = 0L
        var dumpThread: Thread? = null
        val watcher = ObjectRetentionWatcher(
            retainedDelayMs = 1_000L,
            clock = { now },
            exactAdmission = true,
            heapDumpReporter = { _, _, _, _, _ -> dumpThread = Thread.currentThread() },
        )
        enable(watcher)
        val retained = Any()
        watcher.watch(retained, "example.Owner", null, null)
        now = 2_000L
        watcher.stop()
        assertNotNull(dumpThread)
        assertNotSame("stop called heap reporter synchronously", Thread.currentThread(), dumpThread)
        java.lang.ref.Reference.reachabilityFence(retained)
    }

    @Test
    fun stopDoesNotWaitForDiagnosticCheckLock() {
        val watcher = ObjectRetentionWatcher(retainedDelayMs = 1_000L)
        enable(watcher)
        val field = watcher.javaClass.getDeclaredField("checkLock").apply { isAccessible = true }
        val lock = field.get(watcher) as ReentrantLock
        val executor = Executors.newSingleThreadExecutor()
        try {
            lock.withLock {
                val result = executor.submit<ObjectRetentionWatcher.StopResult> { watcher.stop(50L) }
                    .get(500L, TimeUnit.MILLISECONDS)
                assertEquals(ObjectRetentionWatcher.StopResult.TIMED_OUT, result)
            }
        } finally {
            executor.shutdown()
            check(executor.awaitTermination(2L, TimeUnit.SECONDS))
            watcher.stop()
        }
    }

    @Test
    fun inFlightDumpOutlivesDeadlineWithoutRunningAnotherDump() {
        var now = 0L
        var dumps = 0
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val watcher = ObjectRetentionWatcher(
            retainedDelayMs = 1_000L,
            clock = { now },
            exactAdmission = true,
            heapDumpReporter = { _, _, _, _, _ ->
                dumps++
                entered.countDown()
                check(release.await(2L, TimeUnit.SECONDS))
            },
        )
        val executor = Executors.newSingleThreadExecutor()
        val retained = Any()
        enable(watcher)
        watcher.watch(retained, "example.Owner", null, null)
        now = 2_000L
        val checking = executor.submit { watcher.checkRetained() }
        try {
            assertTrue(entered.await(1L, TimeUnit.SECONDS))
            assertEquals(ObjectRetentionWatcher.StopResult.TIMED_OUT, watcher.stop(50L))
            assertEquals(1, dumps)
            watcher.watch(Any(), "must-not-be-admitted", null, null)
        } finally {
            release.countDown()
            checking.get(1L, TimeUnit.SECONDS)
            executor.shutdown()
            assertTrue(executor.awaitTermination(1L, TimeUnit.SECONDS))
        }
        watcher.checkRetained()
        assertEquals(1, dumps)
        java.lang.ref.Reference.reachabilityFence(retained)
    }

    @Test
    fun finalDumpStopsWaitingAtDeadlineAndSkipsSubsequentGroups() {
        var now = 0L
        var dumps = 0
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val watcher = ObjectRetentionWatcher(
            retainedDelayMs = 1_000L,
            clock = { now },
            exactAdmission = true,
            heapDumpReporter = { _, _, _, _, _ ->
                dumps++
                entered.countDown()
                check(release.await(2L, TimeUnit.SECONDS))
            },
        )
        val retained = listOf(Any(), Any())
        enable(watcher)
        retained.forEachIndexed { index, value -> watcher.watch(value, "group-$index", null, null) }
        now = 2_000L
        try {
            assertEquals(ObjectRetentionWatcher.StopResult.TIMED_OUT, watcher.stop(100L))
            assertTrue(entered.await(1L, TimeUnit.SECONDS))
        } finally {
            release.countDown()
        }
        assertEquals(ObjectRetentionWatcher.StopResult.TIMED_OUT, watcher.stop(1_000L))
        assertEquals(1, dumps)
        java.lang.ref.Reference.reachabilityFence(retained)
    }

    @Test
    fun interruptedStopPreservesInterruptAndDiagnosticOwnership() {
        val watcher = ObjectRetentionWatcher(retainedDelayMs = 1_000L)
        enable(watcher)
        Thread.currentThread().interrupt()
        try {
            assertEquals(ObjectRetentionWatcher.StopResult.INTERRUPTED, watcher.stop(50L))
            assertTrue(Thread.currentThread().isInterrupted)
        } finally {
            Thread.interrupted()
            watcher.stop(1_000L)
        }
    }

    private fun enable(watcher: ObjectRetentionWatcher) {
        val field = watcher.javaClass.getDeclaredField("running").apply { isAccessible = true }
        (field.get(watcher) as AtomicBoolean).set(true)
    }
}
