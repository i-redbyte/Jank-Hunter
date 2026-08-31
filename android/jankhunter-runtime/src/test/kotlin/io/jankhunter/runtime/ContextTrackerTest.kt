package io.jankhunter.runtime

import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import kotlin.concurrent.thread
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class ContextTrackerTest {
    @Test
    fun propagatedOperationIdDoesNotUseBoxedThreadLocal() {
        val field = ContextTracker::class.java.getDeclaredField("propagatedOperationId")

        assertTrue(field.type != ThreadLocal::class.java)
    }

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

    @Test
    fun scopedAnnotationContextRestoresPreviousThreadLocalValues() {
        val tracker = ContextTracker()
        tracker.setScreen("Home")
        val outer = tracker.enterScopedContext("FeedScreen", "FeedOwner")

        assertEquals("FeedScreen", tracker.currentScreen())
        assertEquals("FeedOwner", tracker.currentOwner())

        val inner = tracker.enterScopedContext("DetailsScreen", null)

        assertEquals("DetailsScreen", tracker.currentScreen())
        assertEquals("FeedOwner", tracker.currentOwner())

        tracker.exitScopedContext(inner)
        assertEquals("FeedScreen", tracker.currentScreen())
        assertEquals("FeedOwner", tracker.currentOwner())

        tracker.exitScopedContext(outer)
        assertEquals("Home", tracker.currentScreen())
        assertEquals("unknown", tracker.currentOwner())
    }

    @Test
    fun propagatedOperationIsScopedAndRestored() {
        val tracker = ContextTracker()
        val captured = JankHunterContext(null, null, operationId = 42L)

        tracker.callWithContext(captured, ownerName = null, onContextChanged = {}) {
            assertEquals(42L, tracker.currentOperationId())
            assertEquals(42L, tracker.capture().operationId)
        }

        assertEquals(0L, tracker.currentOperationId())
    }

    @Test
    fun finishedOperationChainIsDetachedByItsOwningContextTracker() {
        val tracker = ContextTracker()
        val active = operation(1L, null)
        val finishedParent = operation(2L, active).also { it.abandon() }
        val finishedHead = operation(3L, finishedParent).also { it.abandon() }
        tracker.activateOperation(finishedHead)

        assertSame(active, tracker.currentOperationOrNull())
        assertNull(finishedHead.previousOperation)
        assertNull(finishedParent.previousOperation)
    }

    private fun operation(id: Long, previous: JankHunterOperation?): JankHunterOperation {
        return JankHunterOperation(
            id = id,
            parentId = previous?.id ?: 0L,
            name = "operation-$id",
            kind = JankHunterOperationKind.SYSTEM,
            startedAtUs = 0L,
            budgetUs = 0L,
            screen = null,
            owner = null,
            attributes = JankHunterOperationAttributes.EMPTY,
            previousOperation = previous,
            sink = null,
            controller = null,
        )
    }
}
