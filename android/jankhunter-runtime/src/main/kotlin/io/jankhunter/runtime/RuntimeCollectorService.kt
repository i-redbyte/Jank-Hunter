package io.jankhunter.runtime

import android.app.Application
import android.content.Context
import io.jankhunter.runtime.internal.system.ActivityTracker
import io.jankhunter.runtime.internal.system.FpsMonitor
import io.jankhunter.runtime.internal.system.HeapDumpReporter
import io.jankhunter.runtime.internal.system.MainLooperDispatchMonitor
import io.jankhunter.runtime.internal.system.MainThreadWatchdog
import io.jankhunter.runtime.internal.system.MemorySampler
import io.jankhunter.runtime.internal.system.ObjectRetentionWatcher
import io.jankhunter.runtime.internal.system.ProcessExitReporter
import io.jankhunter.runtime.internal.system.RetentionReporter
import io.jankhunter.runtime.internal.system.RetainedHeapDumper
import io.jankhunter.runtime.internal.system.RuntimeMaintenanceScheduler
import io.jankhunter.runtime.internal.system.SystemContextSampler
import java.io.File

internal class RuntimeCollectorService(
    private val state: RuntimeState,
    private val callbacks: RuntimeCollectorCallbacks,
    private val retentionReporter: RetentionReporter,
    private val heapDumpReporter: HeapDumpReporter,
) {
    fun start(appContext: Context, config: JankHunterConfig, logDirectory: File) {
        val maintenanceScheduler = RuntimeMaintenanceScheduler(
            exactShutdown = config.exactEventCollectionEnabled(),
        )
        state.maintenanceScheduler = maintenanceScheduler
        if (!config.autoStartCollectors()) return
        if (config.fpsMonitorEnabled() || config.jankStatsEnabled()) {
            RuntimeHookGuard.run {
                state.fpsMonitor = FpsMonitor(
                    config.fpsWindowMs(),
                    config.jankFrameThresholdMs(),
                    callbacks,
                    choreographerFallbackEnabled = config.fpsMonitorEnabled(),
                    exactAdmission = config.exactEventCollectionEnabled(),
                ).also { it.start() }
            }
        }
        if (appContext is Application) {
            RuntimeHookGuard.run {
                state.application = appContext
                state.activityTracker = ActivityTracker(
                    callbacks,
                    config.jankStatsEnabled(),
                    state.fpsMonitor,
                ).also {
                    appContext.registerActivityLifecycleCallbacks(it)
                }
            }
        } else {
            callbacks.recordCounter("jankhunter.activity_tracker.unavailable.count", 1)
        }
        RuntimeHookGuard.run {
            state.watchdog = MainThreadWatchdog(config.mainThreadStallThresholdMs(), callbacks).also { it.start() }
        }
        if (config.mainLooperDispatchMonitorEnabled()) {
            RuntimeHookGuard.run {
                state.dispatchMonitor = MainLooperDispatchMonitor(
                    config.mainThreadStallThresholdMs(),
                    recordDispatch = callbacks::recordMainThreadDispatch,
                ).also {
                    it.start()
                }
            }
        }
        RuntimeHookGuard.run {
            state.memorySampler = MemorySampler(
                config.memorySampleIntervalMs(),
                callbacks,
                callbacks::isUserRelevantForSampling,
            ).also { it.start(maintenanceScheduler) }
        }
        if (config.systemSamplerEnabled()) {
            RuntimeHookGuard.run {
                state.systemContextSampler = SystemContextSampler(
                    appContext,
                    config.systemSampleIntervalMs(),
                    callbacks,
                    callbacks::isUserRelevantForSampling,
                ).also { it.start(maintenanceScheduler) }
            }
        }
        if (config.processExitInfoEnabled()) {
            maintenanceScheduler.execute {
                ProcessExitReporter(callbacks).report(appContext)
            }
        }
        if (config.objectWatcherEnabled()) {
            RuntimeHookGuard.run {
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
                    reporter = retentionReporter,
                    exactAdmission = config.exactEventCollectionEnabled(),
                    onCardinalityLoss = { count ->
                        callbacks.recordQuality(
                            io.jankhunter.runtime.internal.io.QualityCounterId.OBJECT_WATCHER_LIMIT,
                            count,
                        )
                    },
                    heapDumpMinRetainedAgeMs = config.retainedHeapDumpMinRetainedAgeMs(),
                    heapDumpReporter = if (heapDumpEnabled) heapDumpReporter else null,
                ).also { it.start(maintenanceScheduler) }
            }
        }
    }

    fun stop() {
        RuntimeHookGuard.swallow {
            state.activityTracker?.let { tracker ->
                try {
                    state.application?.unregisterActivityLifecycleCallbacks(tracker)
                } finally {
                    tracker.close()
                }
            }
        }
        RuntimeHookGuard.swallow { state.watchdog?.stop() }
        RuntimeHookGuard.swallow { state.dispatchMonitor?.stop() }
        RuntimeHookGuard.swallow { state.memorySampler?.stop() }
        RuntimeHookGuard.swallow { state.systemContextSampler?.stop() }
        RuntimeHookGuard.swallow { state.objectRetentionWatcher?.stop() }
        RuntimeHookGuard.swallow { state.fpsMonitor?.stop() }
        RuntimeHookGuard.swallow { state.maintenanceScheduler?.shutdown() }
    }

    fun switchBinaryStorage(storage: JankHunterBinaryStorage?) {
        state.retainedHeapDumper?.switchBinaryStorage(storage)
    }

    fun reset() {
        state.uiVisibility.set(RuntimeUiVisibility.UNKNOWN.wireValue)
        state.heapDumpInProgress.set(false)
        state.heapDumpAttributionUntilMs.set(0L)
        state.activityTracker = null
        state.mainThreadContext = null
        state.application = null
        state.watchdog = null
        state.dispatchMonitor = null
        state.memorySampler = null
        state.systemContextSampler = null
        state.maintenanceScheduler = null
        state.objectRetentionWatcher = null
        state.retainedHeapDumper = null
        state.fpsMonitor = null
    }

}
