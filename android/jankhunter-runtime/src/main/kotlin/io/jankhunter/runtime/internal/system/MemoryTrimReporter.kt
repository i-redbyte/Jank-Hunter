package io.jankhunter.runtime.internal.system

import android.content.ComponentCallbacks2
import android.content.res.Configuration
import io.jankhunter.runtime.JankHunter

@Suppress("DEPRECATION")
class MemoryTrimReporter : ComponentCallbacks2 {
    override fun onTrimMemory(level: Int) {
        JankHunter.recordCounter("memory.trim.level.$level.count", 1)
        JankHunter.recordGauge("memory.trim.last_level", level.toLong())
        if (level >= ComponentCallbacks2.TRIM_MEMORY_RUNNING_LOW) {
            JankHunter.recordCounter("memory.trim.running_low_or_worse.count", 1)
        }
    }

    @Suppress("OVERRIDE_DEPRECATION")
    override fun onLowMemory() {
        JankHunter.recordCounter("memory.low_memory.callback.count", 1)
        JankHunter.recordGauge("memory.trim.last_level", ComponentCallbacks2.TRIM_MEMORY_COMPLETE.toLong())
    }

    override fun onConfigurationChanged(newConfig: Configuration) = Unit
}
