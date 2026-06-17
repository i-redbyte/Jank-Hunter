package io.jankhunter.runtime

import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import kotlin.concurrent.thread
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ContextTrackerTest {
    @Test
    fun propagatedScreenDoesNotOverwriteGlobalScreenUpdatesFromOtherThreads() {
        val tracker = ContextTracker()
        tracker.setScreen("Home")
        val captured = tracker.capture()
        tracker.setScreen("Checkout")

        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val propagatedScreen = AtomicReference<String>()
        val worker = thread {
            tracker.callWithContext(captured, ownerName = null, onContextChanged = {}) {
                propagatedScreen.set(tracker.currentScreen())
                entered.countDown()
                assertTrue(release.await(2, TimeUnit.SECONDS))
            }
        }

        assertTrue(entered.await(2, TimeUnit.SECONDS))
        assertEquals("Checkout", tracker.currentScreen())

        tracker.setScreen("Payment")
        release.countDown()
        worker.join(2_000)

        assertEquals("Home", propagatedScreen.get())
        assertEquals("Payment", tracker.currentScreen())
    }
}
