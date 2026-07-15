package io.jankhunter.runtime.integration

import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterJankStatsTest {
    @Test
    fun uninstallDisablesTrackingOnlyOnce() {
        val fake = FakeJankStats()
        val handle = JankHunterJankStats.Handle(fake)

        handle.uninstall()
        handle.uninstall()

        assertFalse(fake.trackingEnabledState)
        assertEquals(1, fake.setTrackingEnabledCalls)
    }

    @Test
    fun uninstalledHandleCannotBeReenabled() {
        val fake = FakeJankStats()
        val handle = JankHunterJankStats.Handle(fake)

        handle.uninstall()
        handle.setTrackingEnabled(true)

        assertFalse(fake.trackingEnabledState)
        assertEquals(1, fake.setTrackingEnabledCalls)
    }

    @Test
    fun uninstallAndReenableAreSerializedAcrossThreads() {
        val fake = BlockingFakeJankStats()
        val handle = JankHunterJankStats.Handle(fake)
        val reenableStarted = CountDownLatch(1)
        val executor = Executors.newFixedThreadPool(2)
        val uninstall = executor.submit { handle.uninstall() }
        try {
            assertTrue(fake.disableEntered.await(2, TimeUnit.SECONDS))
            val reenable = executor.submit {
                reenableStarted.countDown()
                handle.setTrackingEnabled(true)
            }
            assertTrue(reenableStarted.await(2, TimeUnit.SECONDS))
            fake.allowDisable.countDown()
            uninstall.get(2, TimeUnit.SECONDS)
            reenable.get(2, TimeUnit.SECONDS)
        } finally {
            fake.allowDisable.countDown()
            executor.shutdownNow()
            assertTrue(executor.awaitTermination(2, TimeUnit.SECONDS))
        }

        assertFalse(fake.trackingEnabledState)
        assertEquals(1, fake.setTrackingEnabledCalls.get())
    }

    @Test
    fun fatalErrorsFromReflectedJankStatsAreNotSuppressed() {
        val handle = JankHunterJankStats.Handle(FatalFakeJankStats())

        assertThrows(FatalTestError::class.java) {
            handle.setTrackingEnabled(true)
        }
    }

    class FakeJankStats {
        var trackingEnabledState = true
        var setTrackingEnabledCalls = 0

        fun setTrackingEnabled(enabled: Boolean) {
            trackingEnabledState = enabled
            setTrackingEnabledCalls++
        }
    }

    class BlockingFakeJankStats {
        val disableEntered = CountDownLatch(1)
        val allowDisable = CountDownLatch(1)
        val setTrackingEnabledCalls = AtomicInteger()

        @Volatile
        var trackingEnabledState = true

        fun setTrackingEnabled(enabled: Boolean) {
            setTrackingEnabledCalls.incrementAndGet()
            if (!enabled) {
                disableEntered.countDown()
                assertTrue(allowDisable.await(2, TimeUnit.SECONDS))
            }
            trackingEnabledState = enabled
        }
    }

    class FatalFakeJankStats {
        fun setTrackingEnabled(@Suppress("UNUSED_PARAMETER") enabled: Boolean) {
            throw FatalTestError()
        }
    }

    class FatalTestError : VirtualMachineError()
}
