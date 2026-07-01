package io.jankhunter.runtime

import android.app.Application
import android.content.Context
import io.jankhunter.runtime.internal.system.ActivityTracker
import io.jankhunter.runtime.internal.system.FpsMonitor
import io.jankhunter.runtime.internal.system.MainLooperDispatchMonitor
import io.jankhunter.runtime.internal.system.MainThreadWatchdog
import io.jankhunter.runtime.internal.system.MemorySampler
import io.jankhunter.runtime.internal.system.MemoryTrimReporter
import io.jankhunter.runtime.internal.system.ObjectRetentionWatcher
import io.jankhunter.runtime.internal.system.ProcessExitReporter
import io.jankhunter.runtime.internal.system.RetainedHeapDumper
import io.jankhunter.runtime.internal.system.SystemContextSampler
import java.io.File

internal class RuntimeCollectorService(
    private val state: RuntimeState,
) {
    fun start(appContext: Context, config: JankHunterConfig, logDirectory: File) {
        if (!config.autoStartCollectors()) return
        if (appContext is Application) {
            state.application = appContext
            state.activityTracker = ActivityTracker(config.jankStatsEnabled()).also {
                appContext.registerActivityLifecycleCallbacks(it)
            }
        }
        state.watchdog = MainThreadWatchdog(config.mainThreadStallThresholdMs()).also { it.start() }
        if (config.mainLooperDispatchMonitorEnabled()) {
            state.dispatchMonitor = MainLooperDispatchMonitor(config.mainThreadStallThresholdMs()).also {
                it.start()
            }
        }
        state.memoryTrimReporter = MemoryTrimReporter().also {
            appContext.registerComponentCallbacks(it)
            state.componentCallbackContext = appContext
        }
        state.memorySampler = MemorySampler(
            appContext,
            config.memorySampleIntervalMs(),
            JankHunter::isAppForegroundForSampling,
        ).also { it.start() }
        if (config.systemSamplerEnabled()) {
            state.systemContextSampler = SystemContextSampler(
                appContext,
                config.systemSampleIntervalMs(),
                JankHunter::isAppForegroundForSampling,
            ).also { it.start() }
        }
        if (config.processExitInfoEnabled()) {
            ProcessExitReporter.report(appContext)
        }
        if (config.objectWatcherEnabled()) {
            if (config.retainedHeapDumpEnabled()) {
                state.retainedHeapDumper = RetainedHeapDumper(
                    config.retainedHeapDumpDirectory() ?: logDirectory,
                    config.binaryStorage(),
                    config.retainedHeapDumpMinIntervalMs(),
                    config.retainedHeapDumpMaxCount(),
                    config.retainedHeapDumpMinRetainedAgeMs(),
                )
            }
            state.objectRetentionWatcher = ObjectRetentionWatcher(
                config.retainedObjectDelayMs(),
                config.retainedObjectForceGcEnabled(),
            ).also { it.start() }
        }
        if (config.fpsMonitorEnabled()) {
            state.fpsMonitor = FpsMonitor(
                config.fpsWindowMs(),
                config.jankFrameThresholdMs(),
            ).also { it.start() }
        }
    }

    fun stop() {
        swallow {
            state.activityTracker?.let { tracker ->
                state.application?.unregisterActivityLifecycleCallbacks(tracker)
                tracker.close()
            }
        }
        swallow { state.watchdog?.stop() }
        swallow { state.dispatchMonitor?.stop() }
        swallow {
            state.memoryTrimReporter?.let { reporter ->
                state.componentCallbackContext?.unregisterComponentCallbacks(reporter)
            }
        }
        swallow { state.memorySampler?.stop() }
        swallow { state.systemContextSampler?.stop() }
        swallow { state.objectRetentionWatcher?.stop() }
        swallow { state.fpsMonitor?.stop() }
    }

    fun reset() {
        state.activityTracker = null
        state.application = null
        state.watchdog = null
        state.dispatchMonitor = null
        state.memoryTrimReporter = null
        state.componentCallbackContext = null
        state.memorySampler = null
        state.systemContextSampler = null
        state.objectRetentionWatcher = null
        state.retainedHeapDumper = null
        state.fpsMonitor = null
    }

    private inline fun swallow(block: () -> Unit) {
        try {
            block()
        } catch (_: Throwable) {
        }
    }
}
