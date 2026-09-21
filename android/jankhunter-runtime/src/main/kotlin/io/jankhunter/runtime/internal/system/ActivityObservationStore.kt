package io.jankhunter.runtime.internal.system

import android.app.Activity
import android.app.Application
import android.os.Bundle
import io.jankhunter.runtime.RuntimeHookGuard
import io.jankhunter.runtime.internal.saturatingAdd
import java.lang.ref.WeakReference

/** Weak lifecycle state between sessions; the active tracker is attached only until producer stop. */
internal class ActivityObservationStore {
    private val registrationLock = Any()
    private val stateLock = Any()
    private val activities = ArrayList<Entry>()
    private var application: Application? = null
    // Registration identity prevents a captured old callback from modifying a replacement session.
    private var activeCallbacks: ObservationCallbacks? = null
    private var activeTracker: ActivityTracker? = null
    private var capacityLoss = 0L

    private inner class ObservationCallbacks : Application.ActivityLifecycleCallbacks {
        override fun onActivityCreated(activity: Activity, savedInstanceState: Bundle?) = dispatch(this) {
            it.onActivityCreated(activity, savedInstanceState)
        }
        override fun onActivityStarted(activity: Activity) {
            update(this, activity, resumed = false)
            dispatch(this) { it.onActivityStarted(activity) }
        }
        override fun onActivityResumed(activity: Activity) {
            update(this, activity, resumed = true)
            dispatch(this) { it.onActivityResumed(activity) }
        }
        override fun onActivityPaused(activity: Activity) {
            synchronized(stateLock) {
                val index = if (activeCallbacks === this) indexOf(activity) else -1
                if (index >= 0) activities[index].resumed = false
            }
            dispatch(this) { it.onActivityPaused(activity) }
        }
        override fun onActivityStopped(activity: Activity) {
            remove(this, activity)
            dispatch(this) { it.onActivityStopped(activity) }
        }
        override fun onActivityDestroyed(activity: Activity) {
            remove(this, activity)
            dispatch(this) { it.onActivityDestroyed(activity) }
        }
        override fun onActivitySaveInstanceState(activity: Activity, outState: Bundle) = dispatch(this) {
            it.onActivitySaveInstanceState(activity, outState)
        }
    }

    fun observe(application: Application) = synchronized(registrationLock) {
        if (this.application === application) return@synchronized
        closeRegistration()
        this.application = application
        val callbacks = ObservationCallbacks()
        synchronized(stateLock) { activeCallbacks = callbacks }
        try {
            application.registerActivityLifecycleCallbacks(callbacks)
        } catch (error: Throwable) {
            try {
                closeRegistration()
            } catch (cleanup: Throwable) {
                if (error !is VirtualMachineError && error !is ThreadDeath) RuntimeHookGuard.rethrowFatal(cleanup)
            }
            throw error
        }
    }

    fun close() = synchronized(registrationLock) { closeRegistration() }

    /** Main-thread attachment shares the existing callback, including its in-progress dispatch. */
    fun attach(tracker: ActivityTracker): Boolean = synchronized(stateLock) {
        if (activeCallbacks == null) return@synchronized false
        check(activeTracker == null || activeTracker === tracker)
        activeTracker = tracker
        true
    }

    /** Runs before queued main cleanup so the observer cannot retain a stopped collector. */
    fun detach(tracker: ActivityTracker) = synchronized(stateLock) {
        if (activeTracker === tracker) activeTracker = null
    }

    /** Transient strong references are consumed on main; no UI work runs under the store lock. */
    fun snapshot(): List<ObservedActivity> = synchronized(stateLock) {
        if (activities.isEmpty()) return@synchronized emptyList()
        val result = ArrayList<ObservedActivity>(activities.size)
        var index = 0
        while (index < activities.size) {
            val entry = activities[index]
            val activity = entry.activity.get()
            if (activity == null || activity.isDestroyed || activity.isFinishing) {
                activities.removeAt(index)
            } else {
                result.add(ObservedActivity(activity, entry.resumed))
                index++
            }
        }
        result
    }

    fun takeCapacityLoss(): Long = synchronized(stateLock) {
        val result = capacityLoss
        capacityLoss = 0L
        result
    }

    private fun closeRegistration() {
        val previous = application
        application = null
        val callbacks = synchronized(stateLock) {
            val previousCallbacks = activeCallbacks
            activeCallbacks = null
            activeTracker = null
            activities.clear()
            capacityLoss = 0L
            previousCallbacks
        }
        // Application methods do not run under stateLock; lifecycle callbacks only take stateLock.
        if (callbacks != null) previous?.unregisterActivityLifecycleCallbacks(callbacks)
    }

    private fun update(source: ObservationCallbacks, activity: Activity, resumed: Boolean) = synchronized(stateLock) {
        if (activeCallbacks !== source) return@synchronized
        val index = pruneAndFind(activity, removeMatching = false)
        if (index >= 0) {
            if (resumed) {
                val existing = activities[index]
                existing.resumed = true
                activities.removeAt(index)
                activities.add(existing)
            }
            return@synchronized
        }
        if (activities.size == CAPACITY) {
            var inactive = -1
            for (entryIndex in activities.indices) {
                if (!activities[entryIndex].resumed) {
                    inactive = entryIndex
                    break
                }
            }
            activities.removeAt(if (inactive >= 0) inactive else 0)
            capacityLoss = saturatingAdd(capacityLoss, 1L)
        }
        activities.add(Entry(WeakReference(activity), resumed))
        Unit
    }

    private fun indexOf(activity: Activity): Int {
        for (index in activities.indices) if (activities[index].activity.get() === activity) return index
        return -1
    }

    private fun remove(source: ObservationCallbacks, activity: Activity) = synchronized(stateLock) {
        if (activeCallbacks !== source) return@synchronized
        pruneAndFind(activity, removeMatching = true)
        Unit
    }

    /** Stable in-place compaction visits each weak reference once, without predicates or iterators. */
    private fun pruneAndFind(activity: Activity, removeMatching: Boolean): Int {
        var writeIndex = 0
        var foundIndex = -1
        for (readIndex in activities.indices) {
            val entry = activities[readIndex]
            val observed = entry.activity.get()
            if (observed != null && (!removeMatching || observed !== activity)) {
                if (observed === activity) foundIndex = writeIndex
                if (writeIndex != readIndex) activities[writeIndex] = entry
                writeIndex++
            }
        }
        while (activities.size > writeIndex) activities.removeAt(activities.lastIndex)
        return foundIndex
    }

    private inline fun dispatch(source: ObservationCallbacks, action: (ActivityTracker) -> Unit) {
        val tracker = synchronized(stateLock) { if (activeCallbacks === source) activeTracker else null }
        // No collector or application code runs under the store lock.
        if (tracker != null) action(tracker)
    }

    internal data class ObservedActivity(val activity: Activity, val resumed: Boolean)

    private class Entry(val activity: WeakReference<Activity>, var resumed: Boolean)

    private companion object {
        const val CAPACITY = 64
    }
}
