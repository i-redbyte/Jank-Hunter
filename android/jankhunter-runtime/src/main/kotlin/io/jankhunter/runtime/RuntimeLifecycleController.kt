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
    private val reconfigureLock = Any()

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
            JankHunterManifestConfig.mergeBuildMetadata(providedConfig, context)
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

    fun reconfigure(reason: String?, updater: JankHunterConfigUpdater): Boolean {
        return synchronized(reconfigureLock) reconfigure@{
            val snapshot = synchronized(state.lifecycleLock) snapshot@{
                val context = state.initContext ?: return@snapshot null
                val baseConfig = state.baseConfig ?: return@snapshot null
                ReconfigurationSnapshot(context, baseConfig, state.lifecycleGeneration)
            } ?: return@reconfigure false

            val requestedConfig = buildReconfiguredConfig(snapshot, updater)
                ?: return@reconfigure false
            synchronized(state.storageValveLock) {
                synchronized(state.lifecycleLock) apply@{
                    if (state.lifecycleGeneration != snapshot.generation) return@apply false
                    val updatedConfig = requestedConfig.toBuilder()
                        .binaryStorage(state.selectedBinaryStorage)
                        .build()
                    applyReconfigurationLocked(snapshot.context, updatedConfig, reason)
                }
            }
        }
    }

    fun diagnostics(): JankHunterInitDiagnostics = state.initDiagnostics

    fun shutdown() {
        synchronized(state.lifecycleLock) {
            session.stop(clearInit = true)
        }
    }

    private fun initLocked(
        context: Context?,
        config: JankHunterConfig?,
    ): Boolean {
        val attempt = state.initAttempts.incrementAndGet()
        if (context == null) {
            coordinator.recordInitStatus("missing_context", attempt)
            return false
        }
        if (config == null) {
            coordinator.recordInitStatus("missing_config", attempt)
            return false
        }
        var processNameForDiagnostics: String? = null
        var directoryForDiagnostics: File? = null
        try {
            val appContext = runtimeContext(context)
            val processName = ProcessNames.current(appContext)
            processNameForDiagnostics = processName
            if (!coordinator.isStopped()) {
                coordinator.recordInitStatus("already_started", attempt, processName)
                return false
            }
            if (!config.enabled()) {
                bindInitialConfig(appContext, config, runtimeEnabled = false)
                markCollectionInactive()
                coordinator.recordInitStatus("disabled", attempt, processName)
                return true
            }
            if (!config.isProcessAllowed(processName, appContext.packageName)) {
                coordinator.recordInitStatus("process_not_allowed", attempt, processName)
                return true
            }

            bindInitialConfig(appContext, config, config.runtimeEnabled())

            markCollectionInactive()
            if (!config.runtimeEnabled()) {
                coordinator.recordInitStatus("runtime_disabled", attempt, processName)
                return true
            }
            directoryForDiagnostics = session.logDirectory(appContext, config)
            directoryForDiagnostics = session.start(appContext, config, attempt, processName)
            return true
        } catch (throwable: Throwable) {
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.RUNTIME_LIFECYCLE)
            session.stop(clearInit = false)
            coordinator.recordInitFailure(throwable, attempt, processNameForDiagnostics, directoryForDiagnostics)
            return false
        }
    }

    private fun buildReconfiguredConfig(
        snapshot: ReconfigurationSnapshot,
        updater: JankHunterConfigUpdater,
    ): JankHunterConfig? {
        return try {
            snapshot.baseConfig.toBuilder()
                .also(updater::update)
                .build()
        } catch (throwable: Throwable) {
            RuntimeHookGuard.rethrowFatal(throwable)
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.RUNTIME_LIFECYCLE)
            synchronized(state.lifecycleLock) {
                if (state.lifecycleGeneration == snapshot.generation) {
                    coordinator.recordInitFailure(
                        throwable,
                        state.initAttempts.get(),
                        processName = null,
                        logDirectory = null,
                    )
                }
            }
            null
        }
    }

    private fun applyReconfigurationLocked(
        context: Context,
        config: JankHunterConfig,
        reason: String?,
    ): Boolean {
        val previousConfig = state.config ?: return false
        val previousRuntimeEnabled = state.runtimeEnabled.get()
        val previousStarted = state.started.get()
        val attempt = state.initAttempts.incrementAndGet()
        var processName: String? = null
        var directory: File? = null

        return try {
            processName = ProcessNames.current(context)
            if (config.enabled() && !config.isProcessAllowed(processName, context.packageName)) {
                session.stop(clearInit = false)
                bindCurrentConfig(context, config, config.runtimeEnabled())
                markCollectionInactive()
                coordinator.recordInitStatus("process_not_allowed", attempt, processName)
                return true
            }

            session.stop(clearInit = false)
            val runtimeEnabled = config.enabled() && config.runtimeEnabled()
            bindCurrentConfig(context, config, runtimeEnabled)
            markCollectionInactive()
            when {
                !config.enabled() -> coordinator.recordInitStatus("disabled", attempt, processName)
                !runtimeEnabled -> coordinator.recordInitStatus("runtime_disabled", attempt, processName)
                else -> {
                    directory = session.logDirectory(context, config)
                    directory = session.start(context, config, attempt, processName)
                    recordRuntimeToggleReason("reconfigured", reason)
                }
            }
            true
        } catch (throwable: Throwable) {
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.RUNTIME_LIFECYCLE)
            session.stop(clearInit = false)
            coordinator.recordInitFailure(throwable, attempt, processName, directory)
            val failureDiagnostics = state.initDiagnostics
            restorePreviousSessionLocked(
                context = context,
                config = previousConfig,
                runtimeEnabled = previousRuntimeEnabled,
                wasStarted = previousStarted,
            )
            state.initDiagnostics = failureDiagnostics
            false
        }
    }

    private fun restorePreviousSessionLocked(
        context: Context,
        config: JankHunterConfig,
        runtimeEnabled: Boolean,
        wasStarted: Boolean,
    ) {
        bindCurrentConfig(context, config, runtimeEnabled)
        if (!wasStarted || !runtimeEnabled || !config.enabled()) return
        val attempt = state.initAttempts.incrementAndGet()
        var processName: String? = null
        var directory: File? = null
        try {
            processName = ProcessNames.current(context)
            directory = session.logDirectory(context, config)
            session.start(context, config, attempt, processName)
        } catch (throwable: Throwable) {
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.RUNTIME_LIFECYCLE)
            session.stop(clearInit = false)
            coordinator.recordInitFailure(throwable, attempt, processName, directory)
        }
    }

    private fun bindInitialConfig(
        context: Context,
        config: JankHunterConfig,
        runtimeEnabled: Boolean,
    ) {
        state.baseConfig = config
        state.selectedBinaryStorage = config.binaryStorage()
        bindCurrentConfig(context, config, runtimeEnabled)
    }

    private fun bindCurrentConfig(
        context: Context,
        config: JankHunterConfig,
        runtimeEnabled: Boolean,
    ) {
        state.initContext = context
        state.config = config
        state.runtimeEnabled.set(runtimeEnabled)
        state.lifecycleGeneration++
    }

    private fun markCollectionInactive() {
        state.collectionInactiveSinceElapsedMs.compareAndSet(0L, elapsedRealtimeMs.getAsLong())
    }

    private fun setRuntimeEnabledLocked(enabled: Boolean, reason: String?): Boolean {
        if (!enabled) {
            state.runtimeEnabled.set(false)
            state.lifecycleGeneration++
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
            state.runtimeEnabled.set(false)
            coordinator.recordInitStatus("runtime_enable_missing_init", state.initAttempts.get())
            return false
        }

        val attempt = state.initAttempts.incrementAndGet()
        var processNameForDiagnostics: String? = null
        var directoryForDiagnostics: File? = null
        return try {
            val processName = ProcessNames.current(appContext)
            processNameForDiagnostics = processName
            if (!config.isProcessAllowed(processName, appContext.packageName)) {
                state.runtimeEnabled.set(false)
                coordinator.recordInitStatus("process_not_allowed", attempt, processName)
                return false
            }
            state.runtimeEnabled.set(true)
            state.lifecycleGeneration++
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

    private class ReconfigurationSnapshot(
        val context: Context,
        val baseConfig: JankHunterConfig,
        val generation: Long,
    )
}
