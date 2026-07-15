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
import io.jankhunter.runtime.internal.system.RuntimeMaintenanceScheduler
import io.jankhunter.runtime.internal.system.SystemContextSampler
import java.io.File

internal class RuntimeCollectorService(
    private val state: RuntimeState,
) {
    fun start(appContext: Context, config: JankHunterConfig, logDirectory: File) {
        val maintenanceScheduler = RuntimeMaintenanceScheduler()
        state.maintenanceScheduler = maintenanceScheduler
        if (!config.autoStartCollectors()) return
        if (config.fpsMonitorEnabled() || config.jankStatsEnabled()) {
            state.fpsMonitor = FpsMonitor(
                config.fpsWindowMs(),
                config.jankFrameThresholdMs(),
                choreographerFallbackEnabled = config.fpsMonitorEnabled(),
            ).also { it.start() }
        }
        if (appContext is Application) {
            state.application = appContext
            state.activityTracker = ActivityTracker(
                config.jankStatsEnabled(),
                state.fpsMonitor,
            ).also {
                appContext.registerActivityLifecycleCallbacks(it)
            }
        } else {
            JankHunter.recordCounter("jankhunter.activity_tracker.unavailable.count", 1)
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
        ).also { it.start(maintenanceScheduler) }
        if (config.systemSamplerEnabled()) {
            state.systemContextSampler = SystemContextSampler(
                appContext,
                config.systemSampleIntervalMs(),
                JankHunter::isAppForegroundForSampling,
            ).also { it.start(maintenanceScheduler) }
        }
        if (config.processExitInfoEnabled()) {
            maintenanceScheduler.execute {
                ProcessExitReporter.report(appContext)
            }
        }
        if (config.objectWatcherEnabled()) {
            val heapDumpEnabled = config.retainedHeapDumpEnabled()
            if (heapDumpEnabled) {
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
                heapDumpMinRetainedAgeMs = config.retainedHeapDumpMinRetainedAgeMs(),
                heapDumpReporter = if (heapDumpEnabled) JankHunter::dumpWatchedRetainedHeap else null,
            ).also { it.start(maintenanceScheduler) }
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
        swallow { state.maintenanceScheduler?.shutdown() }
    }

    fun reset() {
        state.activityTracker = null
        state.mainThreadContext = null
        state.application = null
        state.watchdog = null
        state.dispatchMonitor = null
        state.memoryTrimReporter = null
        state.componentCallbackContext = null
        state.memorySampler = null
        state.systemContextSampler = null
        state.maintenanceScheduler = null
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
