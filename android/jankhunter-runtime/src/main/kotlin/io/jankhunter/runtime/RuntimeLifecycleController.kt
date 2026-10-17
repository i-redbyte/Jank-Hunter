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
        if (state.writer?.isWorkerThread() == true) return
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
        } catch (throwable: Throwable) {
            RuntimeHookGuard.rethrowFatal(throwable)
            // Generated startup instrumentation must never take the host process down.
        }
    }

    fun init(context: Context?, providedConfig: JankHunterConfig?) {
        if (state.writer?.isWorkerThread() == true) return
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
        if (state.writer?.isWorkerThread() == true) return false
        return synchronized(state.lifecycleLock) {
            setRuntimeEnabledLocked(enabled, reason)
        }
    }

    fun reconfigure(reason: String?, updater: JankHunterConfigUpdater): Boolean {
        // A host startup callback already owns lifecycleLock. Waiting for the update/valve
        // lock from there inverts their order and can also replace an unfinished startup.
        if (Thread.holdsLock(state.lifecycleLock) || state.writer?.isWorkerThread() == true) return false
        return synchronized(reconfigureLock) configUpdate@{
            val snapshot = synchronized(state.lifecycleLock) readSnapshot@{
                val context = state.initContext ?: return@readSnapshot null
                val configuration = state.configurationSnapshot()
                val config = configuration.config ?: return@readSnapshot null
                ReconfigurationSnapshot(context, config, configuration.desiredRuntimeEnabled,
                    configuration.lifecycleGeneration, configuration.runtimeRequestGeneration, configuration.storageRequestId)
            } ?: return@configUpdate false

            val requestedConfig = buildReconfiguredConfig(snapshot, updater)
                ?: return@configUpdate false
            synchronized(state.storageValveLock) {
                synchronized(state.lifecycleLock) apply@{
                    val configuration = state.configurationSnapshot()
                    if (configuration.lifecycleGeneration != snapshot.generation) return@apply false
                    if (configuration.runtimeRequestGeneration != snapshot.runtimeRequestGeneration) return@apply false
                    if (requestedConfig.storageExplicitlySet &&
                        configuration.storageRequestId != snapshot.storageRequestId
                    ) return@apply false
                    val storage = if (requestedConfig.storageExplicitlySet) requestedConfig.config.binaryStorage()
                        else configuration.selectedBinaryStorage
                    // A storage switch can complete while the updater runs outside lifecycleLock.
                    // Reject conflicting ownership before stopping the current session.
                    if (storage != null && requestedConfig.config.storagePolicy() != null) return@apply false
                    val updatedConfig = requestedConfig.config.toBuilder()
                        .binaryStorage(storage)
                        .build()
                    applyReconfigurationLocked(snapshot.context, updatedConfig, reason,
                        if (requestedConfig.storageExplicitlySet) RuntimeStorageUpdate.Replace(snapshot.storageRequestId)
                        else null)
                }
            }
        }
    }

    fun diagnostics(): JankHunterInitDiagnostics = state.initDiagnostics

    fun shutdown() {
        if (state.writer?.isWorkerThread() == true) return
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
        val diagnostics = StartupDiagnostics()
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
                bindInitialConfig(appContext, config, runtimeEnabled = false)
                markCollectionInactive()
                coordinator.recordInitStatus("process_not_allowed", attempt, processName)
                return true
            }

            bindInitialConfig(appContext, config, config.runtimeEnabled())

            markCollectionInactive()
            if (!config.runtimeEnabled()) {
                coordinator.recordInitStatus("runtime_disabled", attempt, processName)
                return true
            }
            diagnostics.directory = session.logDirectory(appContext, config)
            diagnostics.directory = session.start(appContext, config, attempt, processName)
            return true
        } catch (throwable: Throwable) {
            cleanupFailedSession(
                throwable,
                attempt,
                processNameForDiagnostics,
                diagnostics.directory,
            )
            RuntimeHookGuard.rethrowFatal(throwable)
            return false
        }
    }

    private fun buildReconfiguredConfig(
        snapshot: ReconfigurationSnapshot,
        updater: JankHunterConfigUpdater,
    ): RequestedReconfiguration? {
        return try {
            val builder = snapshot.config.toBuilder()
                .runtimeEnabled(snapshot.desiredRuntimeEnabled)
                .also(updater::update)
            RequestedReconfiguration(builder.build(), builder.binaryStorageExplicitlySet)
        } catch (throwable: Throwable) {
            RuntimeHookGuard.rethrowFatal(throwable)
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.RUNTIME_LIFECYCLE)
            synchronized(state.lifecycleLock) {
                val configuration = state.configurationSnapshot()
                if (configuration.lifecycleGeneration == snapshot.generation &&
                    configuration.runtimeRequestGeneration == snapshot.runtimeRequestGeneration
                ) {
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
        storageUpdate: RuntimeStorageUpdate.Replace?,
    ): Boolean {
        val previousConfiguration = state.configurationSnapshot()
        val previousConfig = previousConfiguration.config ?: return false
        val previousRuntimeEnabled = state.runtimeEnabled.get()
        val previousStarted = state.started.get()
        val attempt = state.initAttempts.incrementAndGet()
        var processName: String? = null
        val diagnostics = StartupDiagnostics()

        return try {
            processName = ProcessNames.current(context)
            if (config.enabled() && !config.isProcessAllowed(processName, context.packageName)) {
                session.stop(clearInit = false)
                bindCurrentConfig(context, config, runtimeEnabled = false, storageUpdate = storageUpdate)
                markCollectionInactive()
                coordinator.recordInitStatus("process_not_allowed", attempt, processName)
                return true
            }

            session.stop(clearInit = false)
            val runtimeEnabled = config.enabled() && config.runtimeEnabled()
            val activeConfig = bindCurrentConfig(context, config, runtimeEnabled, storageUpdate)
            markCollectionInactive()
            when {
                !activeConfig.enabled() -> coordinator.recordInitStatus("disabled", attempt, processName)
                !runtimeEnabled -> coordinator.recordInitStatus("runtime_disabled", attempt, processName)
                else -> {
                    diagnostics.directory = session.logDirectory(context, activeConfig)
                    diagnostics.directory = session.start(context, activeConfig, attempt, processName)
                    recordRuntimeToggleReason("reconfigured", reason)
                }
            }
            true
        } catch (throwable: Throwable) {
            // Host startup callbacks may explicitly shut down or replace this initialization.
            val current = state.configurationSnapshot()
            if (current.initializationGeneration != previousConfiguration.initializationGeneration || current.config == null ||
                state.started.get() && current.config !== previousConfig
            ) {
                RuntimeHookGuard.rethrowFatal(throwable)
                return false
            }
            cleanupFailedSession(throwable, attempt, processName, diagnostics.directory)
            RuntimeHookGuard.rethrowFatal(throwable)
            val failureDiagnostics = state.initDiagnostics
            val requestChanged = current.runtimeRequestGeneration != previousConfiguration.runtimeRequestGeneration
            val restoredConfig = if (requestChanged) {
                previousConfig.toBuilder().runtimeEnabled(current.desiredRuntimeEnabled).build()
            } else previousConfig
            restorePreviousSessionLocked(
                context = context,
                config = restoredConfig,
                runtimeEnabled = if (requestChanged) restoredConfig.enabled() && current.desiredRuntimeEnabled
                    else previousRuntimeEnabled,
                wasStarted = previousStarted,
                storageRequestId = previousConfiguration.storageRequestId,
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
        storageRequestId: Long,
    ) {
        val activeConfig = bindCurrentConfig(context, config, runtimeEnabled,
            RuntimeStorageUpdate.Restore(storageRequestId))
        if (!wasStarted || !runtimeEnabled || !activeConfig.enabled()) return
        val attempt = state.initAttempts.incrementAndGet()
        var processName: String? = null
        var directory: File? = null
        try {
            processName = ProcessNames.current(context)
            directory = session.logDirectory(context, activeConfig)
            session.start(context, activeConfig, attempt, processName)
        } catch (throwable: Throwable) {
            cleanupFailedSession(throwable, attempt, processName, directory)
            RuntimeHookGuard.rethrowFatal(throwable)
        }
    }

    private fun bindInitialConfig(
        context: Context,
        config: JankHunterConfig,
        runtimeEnabled: Boolean,
    ) {
        state.initContext = context
        state.bindInitialConfiguration(config)
        state.runtimeEnabled.set(runtimeEnabled)
        RuntimeHookGuard.run(RuntimeHookFailureReason.COLLECTOR) {
            if (config.enabled() && context is Application) {
                state.activityObservation.observe(context)
            } else {
                state.activityObservation.close()
            }
        }
    }

    private fun bindCurrentConfig(
        context: Context,
        config: JankHunterConfig,
        runtimeEnabled: Boolean,
        storageUpdate: RuntimeStorageUpdate? = null,
    ): JankHunterConfig {
        state.initContext = context
        val activeConfig = state.bindCurrentConfiguration(config, storageUpdate)
        state.runtimeEnabled.set(runtimeEnabled)
        return activeConfig
    }

    private fun markCollectionInactive() {
        state.collectionInactiveSinceElapsedMs.compareAndSet(0L, elapsedRealtimeMs.getAsLong())
    }

    private fun setRuntimeEnabledLocked(enabled: Boolean, reason: String?): Boolean {
        if (!enabled) {
            coordinator.disableHooks()
            state.runtimeEnabled.set(false)
            state.bindRuntimeEnabled(false)
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
                state.bindRuntimeEnabled(true, invalidateSession = false)
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
        if (appContext == null || config == null) {
            state.runtimeEnabled.set(false)
            coordinator.recordInitStatus("runtime_enable_missing_init", state.initAttempts.get())
            return false
        }

        // Store the explicit request even when a global or process gate prevents this start.
        val activeConfig = state.bindRuntimeEnabled(true) ?: return false
        if (!activeConfig.enabled()) {
            state.runtimeEnabled.set(false)
            coordinator.recordInitStatus("disabled", state.initAttempts.get())
            return false
        }
        val attempt = state.initAttempts.incrementAndGet()
        var processNameForDiagnostics: String? = null
        val diagnostics = StartupDiagnostics()
        return try {
            val processName = ProcessNames.current(appContext)
            processNameForDiagnostics = processName
            if (!config.isProcessAllowed(processName, appContext.packageName)) {
                state.runtimeEnabled.set(false)
                coordinator.recordInitStatus("process_not_allowed", attempt, processName)
                return false
            }
            state.runtimeEnabled.set(true)
            diagnostics.directory = session.logDirectory(appContext, activeConfig)
            diagnostics.directory = session.start(appContext, activeConfig, attempt, processName)
            recordCounter("jankhunter.runtime.enabled.count", 1)
            recordRuntimeToggleReason("enabled", reason)
            session.requestFlush()
            true
        } catch (throwable: Throwable) {
            cleanupFailedSession(
                throwable,
                attempt,
                processNameForDiagnostics,
                diagnostics.directory,
            )
            RuntimeHookGuard.rethrowFatal(throwable)
            false
        }
    }

    private fun cleanupFailedSession(
        failure: Throwable,
        attempt: Long,
        processName: String?,
        directory: File?,
    ) {
        val failureIsFatal = failure is VirtualMachineError || failure is ThreadDeath
        if (!failureIsFatal) {
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.RUNTIME_LIFECYCLE)
        }
        preservePrimaryFailure(failure, failureIsFatal) {
            session.stop(clearInit = false)
        }
        preservePrimaryFailure(failure, failureIsFatal) {
            coordinator.recordInitFailure(failure, attempt, processName, directory)
        }
    }

    private inline fun preservePrimaryFailure(
        primary: Throwable,
        primaryIsFatal: Boolean,
        block: () -> Unit,
    ) {
        try {
            block()
        } catch (cleanupFailure: Throwable) {
            if (cleanupFailure !== primary) primary.addSuppressed(cleanupFailure)
            if (!primaryIsFatal) RuntimeHookGuard.rethrowFatal(cleanupFailure)
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

    private class RequestedReconfiguration(
        val config: JankHunterConfig,
        val storageExplicitlySet: Boolean,
    )

    private class ReconfigurationSnapshot(
        val context: Context,
        val config: JankHunterConfig,
        val desiredRuntimeEnabled: Boolean,
        val generation: Long,
        val runtimeRequestGeneration: Long,
        val storageRequestId: Long,
    )
}

private class StartupDiagnostics { var directory: File? = null }
