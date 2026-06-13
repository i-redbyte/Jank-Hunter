package io.jankhunter.runtime.internal.system

import android.app.Activity
import android.app.Application
import android.os.Bundle
import io.jankhunter.runtime.JankHunter

class ActivityTracker : Application.ActivityLifecycleCallbacks {
    private var startedActivities = 0

    override fun onActivityCreated(activity: Activity, savedInstanceState: Bundle?) {
        JankHunter.setScreen(activity.componentName.className)
    }

    override fun onActivityStarted(activity: Activity) {
        if (startedActivities == 0) {
            JankHunter.recordCounter("app.lifecycle.foreground.count", 1)
        }
        startedActivities++
        JankHunter.setScreen(activity.componentName.className)
    }

    override fun onActivityResumed(activity: Activity) {
        JankHunter.setScreen(activity.componentName.className)
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

    override fun onActivityDestroyed(activity: Activity) = Unit
}
