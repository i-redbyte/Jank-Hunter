package io.jankhunter.runtime

import android.app.Application
import android.content.Context
import io.jankhunter.runtime.internal.system.ActivityTracker
import io.jankhunter.runtime.internal.system.FpsMonitor
import io.jankhunter.runtime.internal.system.MainLooperDispatchMonitor
import io.jankhunter.runtime.internal.system.MainThreadWatchdog
import io.jankhunter.runtime.internal.system.MemorySampler
import io.jankhunter.runtime.internal.system.ObjectRetentionWatcher
import io.jankhunter.runtime.internal.system.ProcessExitReporter
import io.jankhunter.runtime.internal.system.RetainedHeapDumper
import io.jankhunter.runtime.internal.system.RuntimeMaintenanceScheduler
import io.jankhunter.runtime.internal.system.RuntimeMainThreadDispatcher
import io.jankhunter.runtime.internal.system.SystemContextSampler
import io.jankhunter.runtime.internal.monotonicDeadlineAfterMillis
import java.io.File

internal class RuntimeCollectorService(
    private val state: RuntimeState,
    private val callbacks: RuntimeCollectorCallbacks,
    private val bindRetentionWatcher: () -> RuntimeRetentionTelemetry.WatcherSession,
    private val mainThreadDispatcher: RuntimeMainThreadDispatcher = RuntimeMainThreadDispatcher(),
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
                    callbacks.bindFrameCallbacks(),
                    choreographerFallbackEnabled = config.fpsMonitorEnabled(),
                    exactAdmission = config.exactEventCollectionEnabled(),
                ).also { it.start() }
            }
        }
        if (appContext is Application) {
            RuntimeHookGuard.run {
                val tracker = ActivityTracker(
                    callbacks,
                    config.jankStatsEnabled(),
                    state.fpsMonitor,
                )
                state.application = appContext
                state.activityTracker = tracker
                if (!mainThreadDispatcher.dispatch {
                        registerActivityTracker(appContext, tracker)
                    }
                ) {
                    state.application = null
                    state.activityTracker = null
                    callbacks.recordCounter("jankhunter.activity_tracker.unavailable.count", 1)
                }
            }
        } else {
            callbacks.recordCounter("jankhunter.activity_tracker.unavailable.count", 1)
        }
        RuntimeHookGuard.run {
            state.watchdog = MainThreadWatchdog(config.mainThreadStallThresholdMs(), callbacks.bindMainThreadStallCallbacks()).also { it.start() }
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
                val retention = bindRetentionWatcher()
                val writer = state.writer
                state.objectRetentionWatcher = ObjectRetentionWatcher(
                    config.retainedObjectDelayMs(),
                    config.retainedObjectForceGcEnabled(),
                    reporter = retention::record,
                    exactAdmission = config.exactEventCollectionEnabled(),
                    onCardinalityLoss = { count ->
                        writer?.recordQuality(
                            io.jankhunter.runtime.internal.io.QualityCounterId.OBJECT_WATCHER_LIMIT,
                            count,
                        )
                    },
                    heapDumpMinRetainedAgeMs = config.retainedHeapDumpMinRetainedAgeMs(),
                    heapDumpReporter = if (heapDumpEnabled) retention::dump else null,
                ).also { it.start(maintenanceScheduler) }
            }
        }
    }

    fun stop(timeoutMs: Long = DEFAULT_STOP_TIMEOUT_MS) {
        val deadlineNs = monotonicDeadlineAfterMillis(timeoutMs.coerceAtLeast(1L))
        stopProducers(remainingTimeoutMs(deadlineNs))
        shutdownMaintenance(remainingTimeoutMs(deadlineNs))
    }

    fun stopProducers(timeoutMs: Long = DEFAULT_STOP_TIMEOUT_MS) {
        val deadlineNs = monotonicDeadlineAfterMillis(timeoutMs.coerceAtLeast(1L))
        val tracker = state.activityTracker
        val application = state.application
        // Invalidate a registration task that may still be waiting on the main queue before
        // asking that same queue to unregister the tracker.
        state.activityTracker = null
        state.application = null
        if (tracker != null) {
            tracker.deactivate()
            state.activityObservation.detach(tracker)
            val accepted = mainThreadDispatcher.dispatch {
                releaseActivityTracker(application, tracker)
            }
            if (!accepted) {
                // Application keeps lifecycle callbacks strongly. A rejected main-thread post must
                // not leave the complete collector graph reachable for the rest of the process.
                releaseActivityTracker(application, tracker)
                callbacks.recordCounter("jankhunter.activity_tracker.cleanup_rejected.count", 1)
            }
        }
        RuntimeHookGuard.swallow { state.watchdog?.stop(remainingTimeoutMs(deadlineNs)) }
        RuntimeHookGuard.swallow { state.dispatchMonitor?.stop() }
        RuntimeHookGuard.swallow { state.memorySampler?.stop() }
        RuntimeHookGuard.swallow { state.systemContextSampler?.stop() }
        RuntimeHookGuard.swallow {
            val writer = state.writer
            val result = state.objectRetentionWatcher?.stop(remainingTimeoutMs(deadlineNs))
            if (result != null) writer?.counter("jankhunter.object_watcher.stop.${result.counterName}.count", 1L)
        }
        RuntimeHookGuard.swallow { state.fpsMonitor?.stop(remainingTimeoutMs(deadlineNs)) }
    }

    fun shutdownMaintenance(timeoutMs: Long = DEFAULT_STOP_TIMEOUT_MS) {
        RuntimeHookGuard.swallow { state.maintenanceScheduler?.shutdown(timeoutMs.coerceAtLeast(1L)) }
    }

    fun reset() {
        state.uiVisibility.set(RuntimeUiVisibility.UNKNOWN.wireValue)
        // HPROF is process-wide and may outlive stop's deadline. Only its owner releases this
        // gate and sets the grace interval; resetting it here would permit overlapping dumps.
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

    private fun remainingTimeoutMs(deadlineNs: Long): Long {
        return ((deadlineNs - System.nanoTime()) / NANOS_PER_MILLISECOND).coerceAtLeast(1L)
    }

    private fun releaseActivityTracker(application: Application?, tracker: ActivityTracker) {
        state.activityObservation.detach(tracker)
        RuntimeHookGuard.swallow {
            try {
                application?.unregisterActivityLifecycleCallbacks(tracker)
            } finally {
                tracker.close()
            }
        }
    }

    private fun registerActivityTracker(application: Application, tracker: ActivityTracker) {
        var registrationFailure: Throwable? = null
        synchronized(state.lifecycleLock) {
            if (state.application !== application || state.activityTracker !== tracker) return
            try {
                if (!state.activityObservation.attach(tracker)) {
                    application.registerActivityLifecycleCallbacks(tracker)
                }
                for (observed in state.activityObservation.snapshot()) {
                    tracker.restoreObservedActivity(observed.activity, observed.resumed)
                }
                val lost = state.activityObservation.takeCapacityLoss()
                if (lost > 0L) callbacks.recordQuality(
                    io.jankhunter.runtime.internal.io.QualityCounterId.LIFECYCLE_REGISTRY_LIMIT, lost,
                )
            } catch (throwable: Throwable) {
                state.application = null
                state.activityTracker = null
                registrationFailure = throwable
            }
        }
        val failure = registrationFailure ?: return
        // Registration can have succeeded before replay/quality reporting failed.
        releaseActivityTracker(application, tracker)
        RuntimeHookGuard.rethrowFatal(failure)
        RuntimeHookFailureTracker.record(RuntimeHookFailureReason.COLLECTOR)
        RuntimeHookGuard.run(RuntimeHookFailureReason.COLLECTOR) {
            callbacks.recordCounter("jankhunter.activity_tracker.unavailable.count", 1)
        }
    }

    private companion object {
        const val DEFAULT_STOP_TIMEOUT_MS = 5_000L
        const val NANOS_PER_MILLISECOND = 1_000_000L
    }

}
