package io.jankhunter.sample

import android.content.Context
import leakcanary.AppWatcher
import leakcanary.LeakCanary

internal object LeakCanaryBridge {
    fun configureAutomatic() {
        LeakCanary.config = LeakCanary.config.copy(
            dumpHeap = false,
            showNotifications = false,
        )
    }

    fun configure() {
        LeakCanary.config = LeakCanary.config.copy(
            dumpHeap = true,
            retainedVisibleThreshold = 1,
            showNotifications = true,
        )
    }

    fun watch(watchedObject: Any, description: String) {
        configure()
        AppWatcher.objectWatcher.expectWeaklyReachable(
            watchedObject = watchedObject,
            description = description,
        )
    }

    fun status(context: Context): String {
        configure()
        return context.getString(R.string.leakcanary_debug_status)
    }
}
