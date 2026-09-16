package io.jankhunter.runtime

import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import java.util.concurrent.locks.Lock
import org.junit.Assert.*
import org.junit.Test

class RuntimeGraphStorageBudgetTest {
    @Test
    fun aBusyRecycleCacheNeverBlocksProducerAcquisitionOrRelease() {
        val budget = RuntimeGraphStorageBudget()
        val cacheLock = checkNotNull(budget.javaClass.getDeclaredField("recycledLock")
            .apply { isAccessible = true }.get(budget))
        val held = CountDownLatch(1)
        val releaseHolder = CountDownLatch(1)
        val finished = CountDownLatch(1)
        val failure = AtomicReference<Throwable?>()
        val holder = Thread {
            val hold = {
                held.countDown()
                check(releaseHolder.await(5L, TimeUnit.SECONDS))
            }
            if (cacheLock is Lock) {
                cacheLock.lock()
                try { hold() } finally { cacheLock.unlock() }
            } else {
                synchronized(cacheLock) { hold() }
            }
        }
        val producer = Thread {
            try {
                val page = checkNotNull(budget.acquirePage())
                budget.recyclePage(page)
            } catch (error: Throwable) { failure.set(error) }
            finally { finished.countDown() }
        }
        try {
            holder.start()
            assertTrue(held.await(5L, TimeUnit.SECONDS))
            producer.start()
            assertTrue("producer waited for the recycle-cache owner", finished.await(1L, TimeUnit.SECONDS))
            failure.get()?.let { throw AssertionError(it) }
            assertEquals(0L, budget.usedBytes())
        } finally {
            releaseHolder.countDown()
            holder.join(5_000L)
            producer.join(5_000L)
            budget.close()
        }
        assertEquals(0L, budget.usedBytes())
    }

    @Test
    fun reservationAndRecyclingPreserveHardLimitAndExistingOwnership() {
        val base = RuntimeGraphStorageBudget.INITIAL_PRODUCER_BYTES
        val pageBytes = RuntimeGraphStorageBudget.PAGE_BYTES
        val budget = RuntimeGraphStorageBudget(base + pageBytes * 2)
        assertTrue(budget.tryReserve(base))
        val first = checkNotNull(budget.acquirePage())
        val second = checkNotNull(budget.acquirePage())
        assertNull(budget.acquirePage())
        assertTrue(budget.consumePressure())
        assertEquals(budget.limitBytes, budget.usedBytes())
        budget.recyclePage(first)
        assertSame(first, budget.acquirePage())
        assertEquals(budget.limitBytes, budget.peakBytes())
        budget.recyclePage(first)
        budget.close()
        assertNull(budget.acquirePage())
        assertFalse(budget.tryReserve(1L))
        assertEquals(base + pageBytes, budget.usedBytes())
        budget.recyclePage(second)
        budget.release(base)
        assertEquals(0L, budget.usedBytes())
    }

    @Test
    fun stackReservationCanReclaimUnusedCachedPages() {
        val budget = RuntimeGraphStorageBudget(RuntimeGraphStorageBudget.INITIAL_PRODUCER_BYTES +
            RuntimeGraphStorageBudget.PAGE_BYTES)
        val page = checkNotNull(budget.acquirePage())
        budget.recyclePage(page)
        assertTrue(budget.tryReserve(budget.limitBytes))
        assertEquals(budget.limitBytes, budget.usedBytes())
        budget.release(budget.limitBytes)
        budget.close()
        assertEquals(0L, budget.usedBytes())
    }

    @Test
    fun competingReservationsNeverExceedQuotaAndAllLeasesAreReleased() {
        val budget = RuntimeGraphStorageBudget(128L * 1024L)
        val start = CountDownLatch(1)
        val failure = AtomicReference<Throwable?>()
        val threads = List(16) {
            Thread {
                try {
                    check(start.await(5L, TimeUnit.SECONDS))
                    repeat(2_000) {
                        if (budget.tryReserve(4_096L)) {
                            assertTrue(budget.usedBytes() <= budget.limitBytes)
                            budget.release(4_096L)
                        }
                    }
                } catch (error: Throwable) { failure.compareAndSet(null, error) }
            }
        }
        threads.forEach(Thread::start)
        start.countDown()
        threads.forEach { it.join(5_000L); assertFalse(it.isAlive) }
        failure.get()?.let { throw AssertionError(it) }
        assertEquals(0L, budget.usedBytes())
        assertTrue(budget.peakBytes() <= budget.limitBytes)
        budget.close()
    }
}
