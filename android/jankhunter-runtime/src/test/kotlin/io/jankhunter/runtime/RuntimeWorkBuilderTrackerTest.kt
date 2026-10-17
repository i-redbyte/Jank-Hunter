package io.jankhunter.runtime

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeWorkBuilderTrackerTest {
    @Test fun unsupportedReplacementDelayCannotReusePreviousValue() {
        val tracker = RuntimeWorkBuilderTracker()
        val builder = Any()
        assertTrue(tracker.periodic(builder, 1_800_000, 300_000))
        assertTrue(tracker.initialDelay(builder, 100))
        assertTrue(tracker.built(builder, 1, 1))
        assertFalse(tracker.initialDelay(builder, -1))
        assertTrue(tracker.built(builder, 2, 2))
        assertEquals(100L, tracker.find(1, 1)?.initialDelayMS)
        assertNull(tracker.find(2, 2)?.initialDelayMS)
        assertEquals(1_800_000L, tracker.find(2, 2)?.periodMS)
    }

    @Test fun unsupportedReplacementBackoffCannotReusePreviousValue() {
        val tracker = RuntimeWorkBuilderTracker()
        val builder = Any()
        assertTrue(tracker.backoff(builder, 1, 100))
        assertTrue(tracker.initialDelay(builder, 0))
        assertTrue(tracker.built(builder, 1, 1))
        assertFalse(tracker.backoff(builder, 1, -1))
        assertTrue(tracker.built(builder, 2, 2))
        assertEquals(100L, tracker.find(1, 1)?.backoffDelayMS)
        assertNull(tracker.find(2, 2)?.backoffPolicy)
        assertNull(tracker.find(2, 2)?.backoffDelayMS)
        assertEquals(0L, tracker.find(2, 2)?.initialDelayMS)
    }

    @Test fun contentionDoesNotWaitOrReuseStaleHints() {
        val tracker = RuntimeWorkBuilderTracker()
        val builder = Any()
        assertTrue(tracker.periodic(builder, 1_000, null))
        assertTrue(tracker.built(builder, 1, 2))
        val field = tracker.javaClass.getDeclaredField("lock").apply { isAccessible = true }
        val lock = field.get(tracker) as java.util.concurrent.locks.ReentrantLock
        val held = java.util.concurrent.CountDownLatch(1)
        val release = java.util.concurrent.CountDownLatch(1)
        val owner = Thread {
            lock.lock()
            try { held.countDown(); release.await() } finally { lock.unlock() }
        }
        owner.start()
        try {
            assertTrue(held.await(5, java.util.concurrent.TimeUnit.SECONDS))
            assertFalse(tracker.initialDelay(builder, 50))
        } finally { release.countDown(); owner.join(5_000) }
        assertNull(tracker.find(1, 2))
        assertTrue(tracker.built(builder, 3, 4))
        assertNull(tracker.find(3, 4))
    }

    @Test fun schedulesPreserveExplicitZeroDelayAndBuilderReuse() {
        val tracker = RuntimeWorkBuilderTracker()
        val builder = Any()
        assertTrue(tracker.periodic(builder, 1_800_000, null))
        assertTrue(tracker.initialDelay(builder, 0))
        assertTrue(tracker.built(builder, 1, 2))
        assertEquals(1_800_000L, tracker.find(1, 2)?.periodMS)
        assertNull(tracker.find(1, 2)?.flexMS)
        assertEquals(0L, tracker.find(1, 2)?.initialDelayMS)
        assertTrue(tracker.initialDelay(builder, 600_000))
        assertTrue(tracker.built(builder, 3, 4))
        assertEquals(0L, tracker.find(1, 2)?.initialDelayMS)
        assertEquals(600_000L, tracker.find(3, 4)?.initialDelayMS)
        assertFalse(tracker.periodic(Any(), -1, null))
    }

    @Test fun explicitBuilderCallsJoinOnlyTheBuiltUuid() {
        val tracker = RuntimeWorkBuilderTracker()
        val builder = Any()
        assertTrue(tracker.backoff(builder, 1, 12_000))
        assertTrue(tracker.expedited(builder, 2))
        assertTrue(tracker.built(builder, 10, 11))
        val hints = tracker.find(10, 11)
        assertEquals(1, hints?.backoffPolicy)
        assertEquals(12_000L, hints?.backoffDelayMS)
        assertEquals(2, hints?.quotaPolicy)
        assertNull(tracker.find(10, 12))
    }

    @Test fun differentBuilderIdentityAndBoundedCapacityCannotInventHints() {
        val tracker = RuntimeWorkBuilderTracker(capacity = 1)
        val first = Any()
        val other = Any()
        assertTrue(tracker.backoff(first, 2, 10_000))
        assertFalse(tracker.expedited(other, 1))
        assertTrue(tracker.built(first, 2, 2))
        assertTrue(tracker.built(other, 1, 1))
        assertNull(tracker.find(1, 1))
        assertEquals(2, tracker.find(2, 2)?.backoffPolicy)
    }

    @Test fun uuidCapacityRetainsFreshHintsAndReportsEviction() {
        val tracker = RuntimeWorkBuilderTracker(capacity = 1)
        val builder = Any()
        assertTrue(tracker.backoff(builder, 1, 30_000))
        assertTrue(tracker.built(builder, 1, 1))
        assertFalse(tracker.built(builder, 2, 2))
        assertEquals(30_000L, tracker.find(2, 2)?.backoffDelayMS)
        assertNull(tracker.find(1, 1))
    }

    @Test fun conflictingBuilderHintsForOneUuidRemainUnknown() {
        val tracker = RuntimeWorkBuilderTracker()
        val first = Any()
        val second = Any()
        assertTrue(tracker.backoff(first, 1, 10_000))
        assertTrue(tracker.backoff(second, 2, 20_000))
        assertTrue(tracker.built(first, 5, 6))
        assertFalse(tracker.built(second, 5, 6))
        assertNull(tracker.find(5, 6))
    }

    @Test fun plainAndExplicitBuildersSharingUuidCannotBorrowHints() {
        for (plainFirst in listOf(true, false)) {
            val tracker = RuntimeWorkBuilderTracker()
            val plain = Any()
            val explicit = Any()
            assertTrue(tracker.backoff(explicit, 1, 10_000))
            if (plainFirst) assertTrue(tracker.built(plain, 7, 8))
            assertEquals(!plainFirst, tracker.built(explicit, 7, 8))
            if (!plainFirst) assertFalse(tracker.built(plain, 7, 8))
            assertNull(tracker.find(7, 8))
        }
    }
}
