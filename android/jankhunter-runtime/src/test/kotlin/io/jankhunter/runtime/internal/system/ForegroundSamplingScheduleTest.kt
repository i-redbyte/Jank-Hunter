package io.jankhunter.runtime.internal.system

import io.jankhunter.runtime.RuntimeHookFailureTracker
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ForegroundSamplingScheduleTest {
    @Test
    fun foregroundTransitionReplacesPendingBackgroundDelay() {
        val foreground = AtomicBoolean(false)
        val sampleCount = AtomicInteger()
        val firstSample = CountDownLatch(1)
        val secondSample = CountDownLatch(1)
        val scheduler = RuntimeMaintenanceScheduler()
        val schedule = ForegroundSamplingSchedule(
            intervalMs = 20,
            foreground = foreground::get,
            task = {
                if (sampleCount.incrementAndGet() == 1) {
                    firstSample.countDown()
                } else {
                    secondSample.countDown()
                }
            },
            minForegroundIntervalMs = 20,
            backgroundIntervalMultiplier = 10,
            minBackgroundIntervalMs = 1_000,
        )

        try {
            schedule.start(scheduler)
            assertTrue(firstSample.await(500, TimeUnit.MILLISECONDS))
            foreground.set(true)
            schedule.onForegroundChanged()

            assertTrue(secondSample.await(500, TimeUnit.MILLISECONDS))
        } finally {
            schedule.stop()
            scheduler.shutdown()
        }
    }

    @Test
    fun exactShutdownWaitsForRunningCollectorTask() {
        val taskStarted = CountDownLatch(1)
        val releaseTask = CountDownLatch(1)
        val taskCompleted = CountDownLatch(1)
        val scheduler = RuntimeMaintenanceScheduler(exactShutdown = true)
        assertTrue(
            scheduler.execute {
                taskStarted.countDown()
                releaseTask.await()
                taskCompleted.countDown()
            },
        )
        assertTrue(taskStarted.await(2, TimeUnit.SECONDS))

        val shutdown = Thread(scheduler::shutdown, "scheduler-shutdown-test").apply { start() }
        try {
            shutdown.join(50L)
            assertTrue("exact shutdown returned before the collector task", shutdown.isAlive)
        } finally {
            releaseTask.countDown()
            shutdown.join(2_000L)
        }
        assertFalse("exact shutdown did not finish after the collector task", shutdown.isAlive)
        assertEquals(0L, taskCompleted.count)
    }

    @Test
    fun suppressedCollectorFailureIsCountedAsTrustEvidence() {
        val before = RuntimeHookFailureTracker.total()
        val scheduler = RuntimeMaintenanceScheduler(exactShutdown = true)

        assertTrue(scheduler.executeAndWait(2_000L) { error("collector failed") })
        scheduler.shutdown()

        assertEquals(before + 1L, RuntimeHookFailureTracker.total())
    }
}
