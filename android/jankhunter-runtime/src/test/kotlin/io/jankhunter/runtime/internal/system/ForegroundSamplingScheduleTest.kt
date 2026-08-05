package io.jankhunter.runtime.internal.system

import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
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
}
