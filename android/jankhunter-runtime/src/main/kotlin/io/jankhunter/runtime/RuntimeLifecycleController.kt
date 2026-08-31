package io.jankhunter.runtime

import android.app.Application
import android.content.Context
import io.jankhunter.runtime.internal.system.ProcessNames
import java.io.File
import java.util.concurrent.atomic.AtomicBoolean

internal class RuntimeLifecycleController(
    private val state: RuntimeState,
    private val coordinator: RuntimeCoordinator,
    private val session: RuntimeSessionController,
    private val metrics: RuntimeMetricsService,
    private val elapsedRealtimeMs: RuntimeLongSource,
) {
    private val autoInitAttempted = AtomicBoolean(false)

    fun init(context: Context?) {
        val config = context?.let(JankHunterManifestConfig::read)
            ?: JankHunterConfig.builder().build()
        synchronized(state.lifecycleLock) {
            initLocked(context, config)
        }
    }

    fun autoInit(context: Context?) {
        if (context == null) return
        if (!autoInitAttempted.compareAndSet(false, true)) return
        try {
            init(context)
        } catch (_: Throwable) {
            // Generated startup instrumentation must never take the host process down.
        }
    }

    fun init(context: Context?, providedConfig: JankHunterConfig?) {
        val effectiveConfig = if (context != null && providedConfig != null) {
            JankHunterManifestConfig.mergeBuildSymbolNamespace(providedConfig, context)
        } else {
            providedConfig
        }
        synchronized(state.lifecycleLock) {
            initLocked(context, effectiveConfig)
        }
    }

    fun isStarted(): Boolean = state.started.get()

    fun isRuntimeEnabled(): Boolean = state.runtimeEnabled.get()

    fun setRuntimeEnabled(enabled: Boolean, reason: String?): Boolean {
        return synchronized(state.lifecycleLock) {
            setRuntimeEnabledLocked(enabled, reason)
        }
    }

    fun diagnostics(): JankHunterInitDiagnostics = state.initDiagnostics

    fun shutdown() {
        synchronized(state.lifecycleLock) {
            session.stop(clearInit = true)
        }
    }

    private fun initLocked(context: Context?, config: JankHunterConfig?) {
        val attempt = state.initAttempts.incrementAndGet()
        if (context == null) {
            coordinator.recordInitStatus("missing_context", attempt)
            return
        }
        if (config == null) {
            coordinator.recordInitStatus("missing_config", attempt)
            return
        }
        if (!config.enabled()) {
            coordinator.recordInitStatus("disabled", attempt)
            return
        }

        var processNameForDiagnostics: String? = null
        var directoryForDiagnostics: File? = null
        try {
            val appContext = runtimeContext(context)
            val processName = ProcessNames.current(appContext)
            processNameForDiagnostics = processName
            if (!config.isProcessAllowed(processName, appContext.packageName)) {
                coordinator.recordInitStatus("process_not_allowed", attempt, processName)
                return
            }
            if (!coordinator.isStopped()) {
                coordinator.recordInitStatus("already_started", attempt, processName)
                return
            }

            state.initContext = appContext
            state.config = config
            state.collectionInactiveSinceElapsedMs.compareAndSet(0L, elapsedRealtimeMs.getAsLong())
            state.runtimeEnabled.set(config.runtimeEnabled())
            if (!config.runtimeEnabled()) {
                coordinator.recordInitStatus("runtime_disabled", attempt, processName)
                return
            }
            directoryForDiagnostics = session.logDirectory(appContext, config)
            directoryForDiagnostics = session.start(appContext, config, attempt, processName)
        } catch (throwable: Throwable) {
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.RUNTIME_LIFECYCLE)
            session.stop(clearInit = true)
            coordinator.recordInitFailure(throwable, attempt, processNameForDiagnostics, directoryForDiagnostics)
        }
    }

    private fun setRuntimeEnabledLocked(enabled: Boolean, reason: String?): Boolean {
        state.runtimeEnabled.set(enabled)
        if (!enabled) {
            state.collectionInactiveSinceElapsedMs.set(elapsedRealtimeMs.getAsLong())
            if (coordinator.isStarting()) {
                recordCounter("jankhunter.runtime.disabled.count", 1)
                recordRuntimeToggleReason("disabled", reason)
            } else if (!coordinator.isStopped()) {
                recordCounter("jankhunter.runtime.disabled.count", 1)
                recordRuntimeToggleReason("disabled", reason)
                session.stop(clearInit = false)
            }
            session.recordRuntimeDisabledStatus()
            return true
        }

        if (!coordinator.isStopped()) {
            return if (state.started.get()) {
                recordCounter("jankhunter.runtime.enabled.noop.count", 1)
                recordRuntimeToggleReason("enabled_noop", reason)
                true
            } else {
                coordinator.recordInitStatus("runtime_enable_in_progress", state.initAttempts.get())
                false
            }
        }

        val appContext = state.initContext
        val config = state.config
        if (appContext == null || config == null || !config.enabled()) {
            coordinator.recordInitStatus("runtime_enable_missing_init", state.initAttempts.get())
            return false
        }

        val attempt = state.initAttempts.incrementAndGet()
        var processNameForDiagnostics: String? = null
        var directoryForDiagnostics: File? = null
        return try {
            val processName = ProcessNames.current(appContext)
            processNameForDiagnostics = processName
            directoryForDiagnostics = session.logDirectory(appContext, config)
            directoryForDiagnostics = session.start(appContext, config, attempt, processName)
            recordCounter("jankhunter.runtime.enabled.count", 1)
            recordRuntimeToggleReason("enabled", reason)
            session.requestFlush()
            true
        } catch (throwable: Throwable) {
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.RUNTIME_LIFECYCLE)
            session.stop(clearInit = false)
            coordinator.recordInitFailure(throwable, attempt, processNameForDiagnostics, directoryForDiagnostics)
            false
        }
    }

    private fun recordRuntimeToggleReason(state: String, reason: String?) {
        val cleanReason = reason
            ?.takeIf { it.isNotBlank() }
            ?.let(::metricOwner)
            ?: DEFAULT_RUNTIME_TOGGLE_REASON
        recordCounter("jankhunter.runtime.$state.reason.$cleanReason.count", 1)
    }

    private fun recordCounter(name: String, value: Long) {
        RuntimeHookGuard.run { metrics.recordCounter(name, value) }
    }

    private fun runtimeContext(context: Context): Context {
        return (context.applicationContext as? Application)
            ?: (context as? Application)
            ?: (context.applicationContext ?: context)
    }

    private companion object {
        const val DEFAULT_RUNTIME_TOGGLE_REASON = "manual"
    }
}
