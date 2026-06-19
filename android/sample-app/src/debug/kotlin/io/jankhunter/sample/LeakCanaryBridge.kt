package io.jankhunter.sample

import leakcanary.AppWatcher
import leakcanary.LeakCanary

internal object LeakCanaryBridge {
    private var configured = false

    fun configure() {
        if (configured) return
        LeakCanary.config = LeakCanary.config.copy(retainedVisibleThreshold = 1)
        configured = true
    }

    fun watch(watchedObject: Any, description: String) {
        configure()
        AppWatcher.objectWatcher.expectWeaklyReachable(
            watchedObject = watchedObject,
            description = description,
        )
    }

    fun status(): String {
        configure()
        return "LeakCanary debug: ON · retained threshold 1 · notification / launcher report"
    }
}
