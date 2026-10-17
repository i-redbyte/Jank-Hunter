package io.jankhunter.runtime.internal.system

import io.jankhunter.runtime.JankHunterWorkRegistryResult
import io.jankhunter.runtime.RuntimeWorkCleanup
import io.jankhunter.runtime.JankHunterWorkRegistrySource
import io.jankhunter.runtime.JankHunterWorkRegistryUnavailable
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executor
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class WorkRegistryStopTest {
    @Test
    fun stoppingDoesNotWaitForTheExternalRegistryHandle() = checkCleanup(lateHandle = false)

    @Test
    fun aHandleReturnedAfterStopCannotBlockTheSharedSampler() = checkCleanup(lateHandle = true)

    private fun checkCleanup(lateHandle: Boolean) {
        val requested = CountDownLatch(1)
        val returnHandle = CountDownLatch(if (lateHandle) 1 else 0)
        val closeEntered = CountDownLatch(1)
        val releaseClose = CountDownLatch(1)
        val closed = CountDownLatch(1)
        val sampled = CountDownLatch(1)
        val stopped = CountDownLatch(1)
        val receive = AtomicReference<(JankHunterWorkRegistryResult) -> Unit>()
        val reports = AtomicInteger()
        val source = JankHunterWorkRegistrySource { _, callback ->
            receive.set(callback)
            requested.countDown()
            check(returnHandle.await(5, TimeUnit.SECONDS))
            AutoCloseable {
                closeEntered.countDown()
                try { check(releaseClose.await(5, TimeUnit.SECONDS)) } finally { closed.countDown() }
            }
        }
        val collector = WorkRegistryCollector(source, Executor { it.run() }, { 1L }, RuntimeWorkCleanup()) { _, _, _ -> reports.incrementAndGet() }
        collector.activate()
        val sampling = Thread { collector.sampleOnce(); sampled.countDown() }
        val stopping = Thread { collector.stop(); stopped.countDown() }
        try {
            sampling.start()
            assertTrue(requested.await(2, TimeUnit.SECONDS))
            if (!lateHandle) assertTrue(sampled.await(2, TimeUnit.SECONDS))
            stopping.start()
            if (lateHandle) {
                assertTrue(stopped.await(1, TimeUnit.SECONDS))
                returnHandle.countDown()
            }
            assertTrue(closeEntered.await(2, TimeUnit.SECONDS))
            assertTrue("Stop waited for an adapter's close()", stopped.await(1, TimeUnit.SECONDS))
            assertTrue("A late handle blocked the sampler in attach()", sampled.await(1, TimeUnit.SECONDS))
            receive.get().invoke(JankHunterWorkRegistryResult.Unavailable(JankHunterWorkRegistryUnavailable.QUERY_FAILED))
            assertEquals(0, reports.get())
        } finally {
            returnHandle.countDown()
            releaseClose.countDown()
            sampling.join(2_000)
            stopping.join(2_000)
            collector.stop()
            assertTrue(closed.await(2, TimeUnit.SECONDS))
        }
        assertFalse(sampling.isAlive)
        assertFalse(stopping.isAlive)
    }
}
