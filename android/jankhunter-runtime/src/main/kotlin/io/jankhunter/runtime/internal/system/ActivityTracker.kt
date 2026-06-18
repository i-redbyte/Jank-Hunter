package io.jankhunter.runtime.internal.system

import android.app.Activity
import android.app.Application
import android.os.Bundle
import android.os.SystemClock
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.integration.JankHunterJankStats

class ActivityTracker(
    private val jankStatsEnabled: Boolean = false,
) : Application.ActivityLifecycleCallbacks {
    private var startedActivities = 0
    private val createdAtMs = SystemClock.elapsedRealtime()
    private val activityStates = linkedMapOf<Activity, ActivityState>()
    private val jankStatsHandles = linkedMapOf<Activity, JankHunterJankStats.Handle>()
    private var lastResumedScreen: String? = null
    private var firstResumeRecorded = false

    override fun onActivityCreated(activity: Activity, savedInstanceState: Bundle?) {
        val screenName = screenName(activity)
        val now = now()
        activityStates[activity] = ActivityState(screenName, createdAtMs = now)
        JankHunter.setScreen(screenName)
        recordLifecycle(screenName, "created")
        if (savedInstanceState != null) {
            JankHunter.recordCounter("screen.${screenKey(screenName)}.lifecycle.restored.count", 1)
        }
    }

    override fun onActivityStarted(activity: Activity) {
        if (startedActivities == 0) {
            JankHunter.setAppForeground(true)
            JankHunter.recordCounter("app.lifecycle.foreground.count", 1)
        }
        startedActivities++
        val screenName = screenName(activity)
        val state = state(activity, screenName)
        state.startedAtMs = now()
        JankHunter.setScreen(screenName)
        recordLifecycle(screenName, "started")
        JankHunter.recordGauge("app.lifecycle.started_activities", startedActivities.toLong())
        installJankStats(activity)
    }

    override fun onActivityResumed(activity: Activity) {
        val screenName = screenName(activity)
        val state = state(activity, screenName)
        val now = now()
        state.resumedAtMs = now
        JankHunter.setScreen(screenName)
        recordLifecycle(screenName, "resumed")
        if (state.createdAtMs > 0) {
            JankHunter.recordGauge("screen.${screenKey(screenName)}.lifecycle.time_to_resume_ms", now - state.createdAtMs)
        }
        if (!firstResumeRecorded) {
            firstResumeRecorded = true
            JankHunter.recordGauge("app.lifecycle.first_resume_ms", now - createdAtMs)
        }
        recordTransition(screenName)
        installJankStats(activity)
    }

    override fun onActivityPaused(activity: Activity) {
        val screenName = screenName(activity)
        val state = state(activity, screenName)
        val now = now()
        recordLifecycle(screenName, "paused")
        if (state.resumedAtMs > 0) {
            JankHunter.recordGauge("screen.${screenKey(screenName)}.lifecycle.foreground_duration_ms", now - state.resumedAtMs)
            state.resumedAtMs = 0L
        }
    }

    override fun onActivityStopped(activity: Activity) {
        val screenName = screenName(activity)
        val state = state(activity, screenName)
        val now = now()
        recordLifecycle(screenName, "stopped")
        if (state.startedAtMs > 0) {
            JankHunter.recordGauge("screen.${screenKey(screenName)}.lifecycle.visible_duration_ms", now - state.startedAtMs)
            state.startedAtMs = 0L
        }
        if (startedActivities > 0) {
            startedActivities--
        }
        JankHunter.recordGauge("app.lifecycle.started_activities", startedActivities.toLong())
        if (startedActivities == 0) {
            JankHunter.setAppForeground(false)
            JankHunter.recordCounter("app.lifecycle.background.count", 1)
            JankHunter.flush()
        }
    }

    override fun onActivitySaveInstanceState(activity: Activity, outState: Bundle) {
        JankHunter.recordCounter("screen.${screenKey(screenName(activity))}.lifecycle.save_state.count", 1)
    }

    override fun onActivityDestroyed(activity: Activity) {
        val screenName = screenName(activity)
        recordLifecycle(screenName, "destroyed")
        activityStates.remove(activity)?.let { state ->
            if (state.createdAtMs > 0) {
                JankHunter.recordGauge("screen.${screenKey(screenName)}.lifecycle.lifetime_ms", now() - state.createdAtMs)
            }
        }
        jankStatsHandles.remove(activity)?.uninstall()
        JankHunter.watchActivity(activity, "lifecycle.destroyed.$screenName")
    }

    fun close() {
        for (handle in jankStatsHandles.values) {
            handle.uninstall()
        }
        jankStatsHandles.clear()
    }

    private fun installJankStats(activity: Activity) {
        if (!jankStatsEnabled || jankStatsHandles.containsKey(activity)) return
        val screenName = screenName(activity)
        val handle = JankHunterJankStats.install(activity.window, screenName) ?: return
        handle.addState("screen", screenName)
        jankStatsHandles[activity] = handle
    }

    private fun state(activity: Activity, screenName: String): ActivityState {
        return activityStates.getOrPut(activity) {
            ActivityState(screenName, createdAtMs = now())
        }.also {
            it.screenName = screenName
        }
    }

    private fun recordLifecycle(screenName: String, event: String) {
        JankHunter.recordCounter("screen.${screenKey(screenName)}.lifecycle.$event.count", 1)
    }

    private fun recordTransition(toScreen: String) {
        val fromScreen = lastResumedScreen
        val toKey = screenKey(toScreen)
        if (fromScreen != null && fromScreen != toScreen) {
            val transitionKey = LifecycleMetricNames.transition(fromScreen, toScreen)
            JankHunter.recordCounter("screen.transition.count", 1)
            JankHunter.recordCounter("screen.transition.$transitionKey.count", 1)
            JankHunter.recordCounter("screen.transition.to.$toKey.count", 1)
        }
        lastResumedScreen = toScreen
    }

    private fun screenName(activity: Activity): String = activity.componentName.className

    private fun screenKey(screenName: String?): String = LifecycleMetricNames.screen(screenName)

    private fun now(): Long = SystemClock.elapsedRealtime()

    private data class ActivityState(
        var screenName: String,
        val createdAtMs: Long,
        var startedAtMs: Long = 0L,
        var resumedAtMs: Long = 0L,
    )
}
