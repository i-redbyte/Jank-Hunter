package io.jankhunter.runtime.internal.system

import io.jankhunter.runtime.RuntimeHookFailureTracker
import java.util.concurrent.CountDownLatch
import java.util.concurrent.ScheduledThreadPoolExecutor
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class UserRelevantSamplingScheduleTest {
    @Test
    fun cancelledFutureSlotRejectsLateFuturePublication() {
        val executor = ScheduledThreadPoolExecutor(1)
        val slot = CancelableScheduledFutureSlot()
        slot.cancel()

        try {
            val future = executor.schedule({}, 1L, TimeUnit.DAYS)

            slot.publish(future)

            assertTrue("future published after cancellation was retained", future.isCancelled)
        } finally {
            executor.shutdownNow()
        }
    }

    @Test
    fun userRelevantStateReplacesPendingInactiveDelay() {
        val userRelevant = AtomicBoolean(false)
        val sampleCount = AtomicInteger()
        val firstSample = CountDownLatch(1)
        val secondSample = CountDownLatch(1)
        val scheduler = RuntimeMaintenanceScheduler()
        val schedule = UserRelevantSamplingSchedule(
            intervalMs = 20,
            userRelevant = userRelevant::get,
            task = {
                if (sampleCount.incrementAndGet() == 1) {
                    firstSample.countDown()
                } else {
                    secondSample.countDown()
                }
            },
            minActiveIntervalMs = 20,
            inactiveIntervalMultiplier = 10,
            minInactiveIntervalMs = 1_000,
        )

        try {
            schedule.start(scheduler)
            assertTrue(firstSample.await(500, TimeUnit.MILLISECONDS))
            userRelevant.set(true)
            schedule.onUserRelevanceChanged()

            assertTrue(secondSample.await(500, TimeUnit.MILLISECONDS))
        } finally {
            schedule.stop()
            scheduler.shutdown()
        }
    }

    @Test
    fun exactShutdownHonorsTimeoutForRunningCollectorTask() {
        val taskStarted = CountDownLatch(1)
        val taskInterrupted = CountDownLatch(1)
        val queuedTaskExecuted = AtomicBoolean(false)
        val scheduler = RuntimeMaintenanceScheduler(exactShutdown = true)
        assertTrue(
            scheduler.execute {
                taskStarted.countDown()
                try {
                    CountDownLatch(1).await()
                } catch (_: InterruptedException) {
                    taskInterrupted.countDown()
                }
            },
        )
        assertTrue(taskStarted.await(2, TimeUnit.SECONDS))
        assertTrue(scheduler.execute { queuedTaskExecuted.set(true) })

        val shutdown = Thread({ scheduler.shutdown(timeoutMs = 25L) }, "scheduler-shutdown-test").apply { start() }
        shutdown.join(1_000L)

        assertFalse("exact shutdown ignored timeout", shutdown.isAlive)
        assertTrue("running maintenance task was not interrupted", taskInterrupted.await(1L, TimeUnit.SECONDS))
        assertFalse("queued maintenance task escaped shutdown", queuedTaskExecuted.get())
    }

    @Test
    fun exactShutdownFromMaintenanceThreadDiscardsQueuedTasks() {
        val shutdownReturned = CountDownLatch(1)
        val queuedTaskExecuted = CountDownLatch(1)
        val scheduler = RuntimeMaintenanceScheduler(exactShutdown = true)
        assertTrue(
            scheduler.execute {
                scheduler.execute { queuedTaskExecuted.countDown() }
                scheduler.shutdown()
                shutdownReturned.countDown()
            },
        )

        assertTrue("maintenance shutdown did not return", shutdownReturned.await(1L, TimeUnit.SECONDS))
        assertFalse(
            "queued maintenance task escaped self-shutdown",
            queuedTaskExecuted.await(100L, TimeUnit.MILLISECONDS),
        )
    }

    @Test
    fun suppressedCollectorFailureIsCountedAsTrustEvidence() {
        val before = RuntimeHookFailureTracker.total()
        val scheduler = RuntimeMaintenanceScheduler(exactShutdown = true)

        assertTrue(scheduler.executeAndWait(2_000L) { error("collector failed") })
        scheduler.shutdown()

        assertEquals(before + 1L, RuntimeHookFailureTracker.total())
    }

    @Test
    fun immediateMaintenanceBacklogIsBounded() {
        val releaseTasks = CountDownLatch(1)
        val scheduler = RuntimeMaintenanceScheduler()
        var accepted = 0

        try {
            repeat(256) {
                if (scheduler.execute { releaseTasks.await() }) accepted++
            }

            assertEquals(64, accepted)
        } finally {
            releaseTasks.countDown()
            scheduler.shutdown()
        }
    }
}
