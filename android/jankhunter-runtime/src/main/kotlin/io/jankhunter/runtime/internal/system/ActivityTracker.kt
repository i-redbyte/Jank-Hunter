package io.jankhunter.runtime.internal.system

import android.app.Activity
import android.app.Application
import android.os.Bundle
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.integration.JankHunterJankStats

class ActivityTracker(
    private val jankStatsEnabled: Boolean = false,
) : Application.ActivityLifecycleCallbacks {
    private var startedActivities = 0
    private val jankStatsHandles = linkedMapOf<Activity, JankHunterJankStats.Handle>()

    override fun onActivityCreated(activity: Activity, savedInstanceState: Bundle?) {
        JankHunter.setScreen(activity.componentName.className)
    }

    override fun onActivityStarted(activity: Activity) {
        if (startedActivities == 0) {
            JankHunter.recordCounter("app.lifecycle.foreground.count", 1)
        }
        startedActivities++
        JankHunter.setScreen(activity.componentName.className)
        installJankStats(activity)
    }

    override fun onActivityResumed(activity: Activity) {
        JankHunter.setScreen(activity.componentName.className)
        installJankStats(activity)
    }

    override fun onActivityPaused(activity: Activity) = Unit

    override fun onActivityStopped(activity: Activity) {
        if (startedActivities > 0) {
            startedActivities--
        }
        if (startedActivities == 0) {
            JankHunter.recordCounter("app.lifecycle.background.count", 1)
            JankHunter.flush()
        }
    }

    override fun onActivitySaveInstanceState(activity: Activity, outState: Bundle) = Unit

    override fun onActivityDestroyed(activity: Activity) {
        jankStatsHandles.remove(activity)?.uninstall()
    }

    fun close() {
        for (handle in jankStatsHandles.values) {
            handle.uninstall()
        }
        jankStatsHandles.clear()
    }

    private fun installJankStats(activity: Activity) {
        if (!jankStatsEnabled || jankStatsHandles.containsKey(activity)) return
        val screenName = activity.componentName.className
        val handle = JankHunterJankStats.install(activity.window, screenName) ?: return
        handle.addState("screen", screenName)
        jankStatsHandles[activity] = handle
    }
}
