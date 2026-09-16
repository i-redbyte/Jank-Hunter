package io.jankhunter.runtime

import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeMetricDrainExecutorTest {
    @Test
    fun boundsPendingWorkAndTerminatesItsOnlyWorkerAfterIdle() {
        val executor = RuntimeMetricDrainExecutor()
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val completed = CountDownLatch(1)
        val worker = AtomicReference<Thread>()
        val calls = AtomicInteger()
        try {
            assertTrue(executor.execute {
                worker.set(Thread.currentThread())
                entered.countDown()
                assertTrue(release.await(2L, TimeUnit.SECONDS))
                calls.incrementAndGet()
            })
            assertTrue(entered.await(1L, TimeUnit.SECONDS))
            assertTrue(executor.execute { calls.incrementAndGet(); completed.countDown() })
            assertFalse(executor.execute { calls.incrementAndGet() })
            release.countDown()
            assertTrue(completed.await(1L, TimeUnit.SECONDS))
            assertEquals(2, calls.get())
            val onlyWorker = checkNotNull(worker.get())
            onlyWorker.join(2_000L)
            assertFalse("idle drain thread retained executor resources", onlyWorker.isAlive)
            val restarted = CountDownLatch(1)
            assertTrue(executor.execute { restarted.countDown() })
            assertTrue(restarted.await(1L, TimeUnit.SECONDS))
        } finally { release.countDown() }
    }
}
