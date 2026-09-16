package io.jankhunter.runtime.internal.system

import android.app.Activity
import android.app.Application
import java.lang.ref.ReferenceQueue
import java.lang.ref.WeakReference
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class ActivityObservationStoreTest {
    @Test
    fun currentStartedAndResumedStateSurvivesPausesWithoutDuplicateEntries() = withStore { store, app ->
        val first = Activity()
        val second = Activity()
        app.callback.onActivityStarted(first)
        app.callback.onActivityResumed(first)
        app.callback.onActivityStarted(first)
        app.callback.onActivityStarted(second)
        app.callback.onActivityResumed(second)
        app.callback.onActivityPaused(second)
        val snapshot = store.snapshot()
        assertEquals(listOf(first, second), snapshot.map { it.activity })
        assertTrue(snapshot[0].resumed)
        assertFalse(snapshot[1].resumed)
        app.callback.onActivityStopped(first)
        app.callback.onActivityDestroyed(second)
        assertTrue(store.snapshot().isEmpty())
    }

    @Test
    fun mostRecentlyResumedActivityIsRestoredLast() = withStore { store, app ->
        val first = Activity()
        val second = Activity()
        app.callback.onActivityResumed(first)
        app.callback.onActivityResumed(second)
        app.callback.onActivityResumed(first)
        assertEquals(listOf(second, first), store.snapshot().map { it.activity })
    }

    @Test
    fun repeatedObserveRegistersOnlyOnceAndCloseInvalidatesCapturedCallbacks() = withStore { store, app ->
        store.observe(app)
        assertEquals(1, app.registrations)
        val callback = app.callback
        callback.onActivityStarted(Activity())
        store.close()
        callback.onActivityResumed(Activity())
        assertEquals(0, app.callbacks.size)
        assertTrue(store.snapshot().isEmpty())
    }

    @Test
    fun callbackFromAClosedRegistrationCannotModifyAReplacementObservation() = withStore { store, app ->
        val oldCallback = app.callback
        store.close()
        store.observe(app)
        val current = Activity()
        app.callback.onActivityResumed(current)
        oldCallback.onActivityStopped(current)
        oldCallback.onActivityStarted(Activity())
        val snapshot = store.snapshot()
        assertEquals(1, snapshot.size)
        assertSame("old lifecycle callback replaced the current observation", current, snapshot.single().activity)
        assertTrue(snapshot.single().resumed)
    }

    @Test
    fun capacityRemainsBoundedAndReportsEveryEvictedObservation() = withStore { store, app ->
        val activities = List(80) { Activity() }
        activities.forEach { app.callback.onActivityStarted(it) }
        assertEquals(64, store.snapshot().size)
        assertEquals(16L, store.takeCapacityLoss())
        assertEquals(0L, store.takeCapacityLoss())
        assertSame(activities.last(), store.snapshot().last().activity)
    }

    @Test
    fun pausedActivityIsEvictedBeforeAResumedActivity() = withStore { store, app ->
        val activities = List(65) { Activity() }
        activities.take(64).forEach { app.callback.onActivityStarted(it) }
        app.callback.onActivityResumed(activities.first())
        app.callback.onActivityStarted(activities.last())
        val snapshot = store.snapshot()
        assertTrue(snapshot.any { it.activity === activities.first() })
        assertFalse(snapshot.any { it.activity === activities[1] })
        assertEquals(1L, store.takeCapacityLoss())
    }

    @Test
    fun observerDoesNotKeepAnActivityAlive() = withStore { store, app ->
        val queue = ReferenceQueue<Activity>()
        val weak = observeTemporary(app.callback, queue)
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(2L)
        while (weak.get() != null && System.nanoTime() < deadline) {
            System.gc()
            queue.remove(25L)
        }
        assertNull("lifecycle observer retained its Activity", weak.get())
        assertTrue(store.snapshot().isEmpty())
    }

    @Test
    fun failedRegistrationReleasesTheApplicationCallbackBeforePropagating() {
        val store = ActivityObservationStore()
        val failure = IllegalStateException("registration failed after add")
        val app = TrackingApplication(failure)
        assertSame(failure, assertThrows(IllegalStateException::class.java) { store.observe(app) })
        assertTrue(app.callbacks.isEmpty())
        assertTrue(store.snapshot().isEmpty())
        store.close()
    }

    private fun observeTemporary(
        callback: Application.ActivityLifecycleCallbacks,
        queue: ReferenceQueue<Activity>,
    ): WeakReference<Activity> {
        val activity = Activity()
        callback.onActivityStarted(activity)
        return WeakReference(activity, queue)
    }

    private fun withStore(action: (ActivityObservationStore, TrackingApplication) -> Unit) {
        val store = ActivityObservationStore()
        val app = TrackingApplication()
        try {
            store.observe(app)
            action(store, app)
        } finally {
            store.close()
        }
    }

    private class TrackingApplication(private val failure: Throwable? = null) : Application() {
        val callbacks = mutableListOf<ActivityLifecycleCallbacks>()
        var registrations = 0
        val callback: ActivityLifecycleCallbacks get() = callbacks.single()
        override fun registerActivityLifecycleCallbacks(callback: ActivityLifecycleCallbacks) {
            callbacks += callback
            registrations++
            failure?.let { throw it }
        }
        override fun unregisterActivityLifecycleCallbacks(callback: ActivityLifecycleCallbacks) {
            callbacks.remove(callback)
        }
    }
}
