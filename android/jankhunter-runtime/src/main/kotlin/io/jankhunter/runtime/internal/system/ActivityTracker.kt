package io.jankhunter.runtime.internal.system

import android.app.Activity
import android.app.Application
import android.os.Bundle
import android.os.SystemClock
import io.jankhunter.runtime.JankHunterOperation
import io.jankhunter.runtime.RuntimeCollectorCallbacks
import io.jankhunter.runtime.RuntimeHookGuard
import io.jankhunter.runtime.integration.JankHunterJankStats
import io.jankhunter.runtime.internal.io.QualityCounterId

internal class ActivityTracker(
    private val callbacks: RuntimeCollectorCallbacks,
    private val jankStatsEnabled: Boolean = false,
    private val frameMonitor: FpsMonitor? = null,
    private val onCardinalityLoss: () -> Unit = {
        callbacks.recordQuality(QualityCounterId.LIFECYCLE_REGISTRY_LIMIT)
    },
) : Application.ActivityLifecycleCallbacks {
    @Volatile private var active = true
    private var startedActivities = 0
    private val createdAtMs = SystemClock.elapsedRealtime()
    private val activityStates = BoundedRegistry<Activity, ActivityState>(MAX_TRACKED_ACTIVITIES) { state ->
        state.resumedAtMs == 0L
    }
    private val jankStatsHandles = linkedMapOf<Activity, JankHunterJankStats.Handle>()
    private val resumedActivities = LastResumedRegistry<Activity>()
    private var activeJankStatsActivity: Activity? = null
    private var lastResumedScreen: String? = null
    private var firstResumeRecorded = false

    override fun onActivityCreated(activity: Activity, savedInstanceState: Bundle?) = whileActive {
        val screenName = screenName(activity)
        val now = now()
        makeRoomFor(activity)
        callbacks.setScreen(screenName)
        activityStates[activity] = newActivityState(screenName, now)
        recordLifecycle(screenName, "created")
        if (savedInstanceState != null) {
            callbacks.recordCounter("screen.${screenKey(screenName)}.lifecycle.restored.count", 1)
        }
    }

    override fun onActivityStarted(activity: Activity) = whileActive {
        val screenName = screenName(activity)
        val state = state(activity, screenName)
        if (state.started) return@whileActive
        state.started = true
        val firstStarted = startedActivities++ == 0
        if (firstStarted) {
            callbacks.setUiVisible(true)
            callbacks.recordCounter("app.lifecycle.ui_visible.count", 1)
            frameMonitor?.setWindowActive(true)
        }
        state.startedAtMs = now()
        callbacks.setScreen(screenName)
        if (state.openOperation == null) {
            state.openOperation = startScreenOpenOperation()
        }
        recordLifecycle(screenName, "started")
        callbacks.recordGauge("app.lifecycle.started_activities", startedActivities.toLong())
        installJankStats(activity)
    }

    override fun onActivityResumed(activity: Activity) = whileActive {
        val screenName = screenName(activity)
        val state = state(activity, screenName)
        val now = now()
        state.resumedAtMs = now
        callbacks.setScreen(screenName)
        recordLifecycle(screenName, "resumed")
        state.openOperation?.success()
        state.openOperation = null
        if (!firstResumeRecorded) {
            firstResumeRecorded = true
            callbacks.recordGauge("app.lifecycle.first_resume_ms", now - createdAtMs)
        }
        recordTransition(screenName)
        installJankStats(activity)
        setJankStatsTracking(activity, true)
    }

    override fun onActivityPaused(activity: Activity) = whileActive {
        val screenName = screenName(activity)
        val state = activityStates[activity]
        val now = now()
        recordLifecycle(screenName, "paused")
        if (state != null && state.resumedAtMs > 0) {
            callbacks.recordGauge("screen.${screenKey(screenName)}.lifecycle.foreground_duration_ms", now - state.resumedAtMs)
            state.resumedAtMs = 0L
        }
        setJankStatsTracking(activity, false)
    }

    override fun onActivityStopped(activity: Activity) = whileActive {
        val screenName = screenName(activity)
        val state = activityStates[activity]
        val now = now()
        recordLifecycle(screenName, "stopped")
        setJankStatsTracking(activity, false)
        if (state != null && state.startedAtMs > 0) {
            callbacks.recordGauge("screen.${screenKey(screenName)}.lifecycle.visible_duration_ms", now - state.startedAtMs)
            state.startedAtMs = 0L
        }
        state?.openOperation?.cancel()
        if (state != null) state.openOperation = null
        val wasStarted = forgetStarted(state)
        callbacks.recordGauge("app.lifecycle.started_activities", startedActivities.toLong())
        if (wasStarted && startedActivities == 0) {
            frameMonitor?.setWindowActive(false)
            callbacks.setUiVisible(false)
            callbacks.recordCounter("app.lifecycle.ui_hidden.count", 1)
            callbacks.requestFlush()
        }
    }

    override fun onActivitySaveInstanceState(activity: Activity, outState: Bundle) = whileActive {
        callbacks.recordCounter("screen.${screenKey(screenName(activity))}.lifecycle.save_state.count", 1)
    }

    override fun onActivityDestroyed(activity: Activity) = whileActive {
        val screenName = screenName(activity)
        val state = activityStates.remove(activity)
        val wasStarted = forgetStarted(state)
        // Release UI ownership before telemetry: a failed metric must not retain a destroyed window.
        removeJankStatsHandle(activity)
        if (wasStarted && startedActivities == 0) {
            frameMonitor?.setWindowActive(false)
            callbacks.setUiVisible(false)
        }
        state?.openOperation?.cancel()
        recordLifecycle(screenName, "destroyed")
        if (wasStarted) callbacks.recordGauge("app.lifecycle.started_activities", startedActivities.toLong())
        if (state != null && state.createdAtMs > 0) {
            callbacks.recordGauge("screen.${screenKey(screenName)}.lifecycle.lifetime_ms", now() - state.createdAtMs)
        }
        val changingConfigurations = runCatching { activity.isChangingConfigurations }.getOrDefault(false)
        if (!changingConfigurations) {
            callbacks.recordCounter("jankhunter.object_watcher.activity_destroyed.watch.count", 1)
            callbacks.watchDestroyedActivity(activity, "lifecycle.destroyed.$screenName")
        }
    }

    fun deactivate() { active = false }

    /** Restore observation, without inventing created/started/resumed events or screen-open time. */
    fun restoreObservedActivity(activity: Activity, resumed: Boolean) = whileActive {
        if (activity.isDestroyed || activity.isFinishing || activityStates.containsKey(activity)) return@whileActive
        val screen = screenName(activity)
        val now = now()
        makeRoomFor(activity)
        val restored = ActivityState(screen, createdAtMs = 0L, started = true, startedAtMs = now)
        activityStates[activity] = restored
        startedActivities++
        callbacks.setUiVisible(true)
        frameMonitor?.setWindowActive(true)
        if (resumed || lastResumedScreen == null) callbacks.setScreen(screen)
        installJankStats(activity)
        if (resumed) {
            restored.resumedAtMs = now
            firstResumeRecorded = true
            lastResumedScreen = screen
            setJankStatsTracking(activity, true)
        }
        callbacks.recordGauge("app.lifecycle.started_activities", startedActivities.toLong())
    }

    private inline fun whileActive(action: () -> Unit) {
        if (active) RuntimeHookGuard.run(action)
    }

    fun close() {
        deactivate()
        activityStates.forEachValue { state -> state.openOperation?.cancel() }
        for (handle in jankStatsHandles.values) {
            handle.uninstall()
        }
        jankStatsHandles.clear()
        resumedActivities.clear()
        activeJankStatsActivity = null
        frameMonitor?.setJankStatsActive(false, sourceChanged = true)
        frameMonitor?.setWindowActive(false)
        activityStates.clear()
    }

    private fun installJankStats(activity: Activity) {
        val canonicalFrameMonitor = frameMonitor ?: return
        if (!jankStatsEnabled || jankStatsHandles.containsKey(activity)) return
        makeRoomFor(activity)
        val handle = JankHunterJankStats.install(activity.window) { isJank, durationNanos ->
            canonicalFrameMonitor.onJankStatsFrame(callbacks.currentScreen(), durationNanos, isJank)
        } ?: return
        handle.setTrackingEnabled(false)
        jankStatsHandles[activity] = handle
    }

    private fun setJankStatsTracking(activity: Activity, enabled: Boolean) {
        if (enabled) {
            resumedActivities.onResumed(activity)
        } else {
            resumedActivities.onNotResumed(activity)
        }
        updateJankStatsTracking()
    }

    private fun removeJankStatsHandle(activity: Activity) {
        resumedActivities.onNotResumed(activity)
        jankStatsHandles.remove(activity)?.uninstall()
        if (activeJankStatsActivity === activity) {
            activeJankStatsActivity = null
        }
        updateJankStatsTracking()
    }

    private fun updateJankStatsTracking() {
        val desired = resumedActivities.latestMatching(jankStatsHandles::containsKey)
        val previous = activeJankStatsActivity
        if (previous === desired) return

        previous?.let { jankStatsHandles[it] }?.setTrackingEnabled(false)
        activeJankStatsActivity = desired
        frameMonitor?.setJankStatsActive(desired != null, sourceChanged = true)
        desired?.let { jankStatsHandles[it] }?.setTrackingEnabled(true)
    }

    private fun makeRoomFor(activity: Activity) {
        val evicted = activityStates.makeRoomFor(activity) ?: return
        // The gauge covers retained observations; evicted state is explicitly quality-accounted.
        forgetStarted(evicted.value)
        evicted.value.openOperation?.cancel()
        removeJankStatsHandle(evicted.key)
        RuntimeHookGuard.run(onCardinalityLoss)
    }

    private fun forgetStarted(state: ActivityState?): Boolean {
        if (state == null || !state.started) return false
        state.started = false
        if (startedActivities > 0) startedActivities--
        return true
    }

    private fun state(activity: Activity, screenName: String): ActivityState {
        makeRoomFor(activity)
        return activityStates.getOrPut(activity) {
            newActivityState(screenName, now())
        }.also {
            it.screenName = screenName
        }
    }

    private fun recordLifecycle(screenName: String, event: String) {
        callbacks.recordCounter("screen.${screenKey(screenName)}.lifecycle.$event.count", 1)
    }

    private fun recordTransition(toScreen: String) {
        val fromScreen = lastResumedScreen
        val toKey = screenKey(toScreen)
        if (fromScreen != null && fromScreen != toScreen) {
            val transitionKey = LifecycleMetricNames.transition(fromScreen, toScreen)
            callbacks.recordCounter("screen.transition.count", 1)
            callbacks.recordCounter("screen.transition.$transitionKey.count", 1)
            callbacks.recordCounter("screen.transition.to.$toKey.count", 1)
        }
        lastResumedScreen = toScreen
    }

    private fun screenName(activity: Activity): String = activity.componentName.className

    private fun screenKey(screenName: String?): String = LifecycleMetricNames.screen(screenName)

    private fun now(): Long = SystemClock.elapsedRealtime()

    private fun newActivityState(screenName: String, createdAtMs: Long): ActivityState {
        callbacks.setScreen(screenName)
        return ActivityState(
            screenName = screenName,
            createdAtMs = createdAtMs,
            openOperation = startScreenOpenOperation(),
        )
    }

    private fun startScreenOpenOperation(): JankHunterOperation? {
        return callbacks.startScreenOpenOperation()
    }

    private data class ActivityState(
        var screenName: String,
        val createdAtMs: Long,
        var started: Boolean = false,
        var startedAtMs: Long = 0L,
        var resumedAtMs: Long = 0L,
        var openOperation: JankHunterOperation? = null,
    )

    private companion object {
        private const val MAX_TRACKED_ACTIVITIES = 64
    }
}

internal class BoundedRegistry<K : Any, V : Any>(
    private val capacity: Int,
    private val preferredEviction: (V) -> Boolean,
) {
    private val entries = linkedMapOf<K, V>()

    init {
        require(capacity > 0) { "bounded registry capacity must be positive" }
    }

    operator fun set(key: K, value: V) {
        entries[key] = value
    }

    operator fun get(key: K): V? = entries[key]

    fun containsKey(key: K): Boolean = entries.containsKey(key)

    fun remove(key: K): V? = entries.remove(key)

    fun clear() = entries.clear()

    fun getOrPut(key: K, defaultValue: () -> V): V = entries.getOrPut(key, defaultValue)

    fun makeRoomFor(key: K): EvictedEntry<K, V>? {
        if (entries.containsKey(key) || entries.size < capacity) return null
        val evicted = entries.entries.firstOrNull { preferredEviction(it.value) }?.key
            ?: entries.keys.first()
        val value = checkNotNull(entries.remove(evicted))
        return EvictedEntry(evicted, value)
    }

    fun forEachValue(action: (V) -> Unit) = entries.values.forEach(action)

    internal fun size(): Int = entries.size
}

internal data class EvictedEntry<K : Any, V : Any>(
    val key: K,
    val value: V,
)
