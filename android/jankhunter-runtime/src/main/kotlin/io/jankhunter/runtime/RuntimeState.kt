package io.jankhunter.runtime

import android.app.Application
import android.content.Context
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.ProcessLogSnapshotCoordinator
import io.jankhunter.runtime.internal.system.ActivityTracker
import io.jankhunter.runtime.internal.system.ActivityObservationStore
import io.jankhunter.runtime.internal.system.FpsMonitor
import io.jankhunter.runtime.internal.system.MainLooperDispatchMonitor
import io.jankhunter.runtime.internal.system.MainThreadWatchdog
import io.jankhunter.runtime.internal.system.MemorySampler
import io.jankhunter.runtime.internal.system.ObjectRetentionWatcher
import io.jankhunter.runtime.internal.system.RetainedHeapDumper
import io.jankhunter.runtime.internal.system.RuntimeMaintenanceScheduler
import io.jankhunter.runtime.internal.system.SystemContextSampler
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReference

internal class RuntimeState {
    val activityObservation = ActivityObservationStore()
    val lifecycleLock = Any()
    val storageValveLock = Any()
    private val configuration = AtomicReference(RuntimeConfigurationSnapshot())
    private val processObservationModes = AtomicReference<JankHunterExecutionObservationModes?>()
    private val defaultObservationModes = JankHunterExecutionObservationModes()

    fun observationModes(): JankHunterExecutionObservationModes = processObservationModes.get() ?: defaultObservationModes

    private fun selectObservationModes(config: JankHunterConfig) {
        if (processObservationModes.get() == null) {
            processObservationModes.compareAndSet(null,
                JankHunterExecutionObservationModes(config.taskObservationMode(), config.coroutineObservationMode()))
        }
    }
    val featureGate = RuntimeFeatureGate()
    val workCleanup = RuntimeWorkCleanup()
    val collectionEpochs = RuntimeCollectionEpochs(workCleanup)
    val started = AtomicBoolean(false)
    val initAttempts = AtomicLong()
    val initFailures = AtomicLong()
    val uiVisibility = AtomicInteger(RuntimeUiVisibility.UNKNOWN.wireValue)
    val runtimeEnabled = AtomicBoolean(true)
    val collectionInactiveSinceElapsedMs = AtomicLong()
    val heapDumpInProgress = AtomicBoolean(false)
    val heapDumpAttributionUntilMs = AtomicLong()

    @Volatile
    var lifecycle = RuntimeLifecycle.STOPPED

    @Volatile
    var writer: AsyncLogWriter? = null

    @Volatile
    var logSnapshotCoordinator: ProcessLogSnapshotCoordinator? = null

    @Volatile
    var snapshotExpectedProcessCount: Int = 0

    var config: JankHunterConfig?
        get() = configuration.get().config
        set(value) {
            updateConfiguration { current -> current.copy(config = value) }
        }

    /** Changes whenever bound configuration or runtime availability changes. */
    var lifecycleGeneration: Long
        get() = configuration.get().lifecycleGeneration
        set(value) {
            updateConfiguration { current -> current.copy(lifecycleGeneration = value) }
        }

    @Volatile
    var initContext: Context? = null

    @Volatile
    var mainThreadContext: JankHunterContext? = null

    @Volatile
    var watchdog: MainThreadWatchdog? = null

    @Volatile
    var dispatchMonitor: MainLooperDispatchMonitor? = null

    @Volatile
    var memorySampler: MemorySampler? = null

    @Volatile
    var workRegistryCollector: io.jankhunter.runtime.internal.system.WorkRegistryCollector? = null

    @Volatile
    var workRegistryHandle: io.jankhunter.runtime.internal.system.MaintenanceHandle? = null

    @Volatile
    var threadSnapshotCollector: io.jankhunter.runtime.internal.system.ThreadSnapshotCollector? = null

    @Volatile
    var systemContextSampler: SystemContextSampler? = null

    @Volatile
    var maintenanceScheduler: RuntimeMaintenanceScheduler? = null

    @Volatile
    var objectRetentionWatcher: ObjectRetentionWatcher? = null

    @Volatile
    var retainedHeapDumper: RetainedHeapDumper? = null

    @Volatile
    var fpsMonitor: FpsMonitor? = null

    @Volatile
    var application: Application? = null

    @Volatile
    var activityTracker: ActivityTracker? = null

    @Volatile
    var crashFlushHandler: Thread.UncaughtExceptionHandler? = null

    @Volatile
    var previousCrashHandler: Thread.UncaughtExceptionHandler? = null

    @Volatile
    var initDiagnostics = JankHunterInitDiagnostics(status = "not_started")

    fun configurationSnapshot(): RuntimeConfigurationSnapshot = configuration.get()

    fun bindInitialConfiguration(config: JankHunterConfig) {
        selectObservationModes(config)
        updateConfiguration { current ->
            current.copy(
                config = config,
                initializationGeneration = current.initializationGeneration + 1L,
                desiredRuntimeEnabled = config.runtimeEnabled(),
                selectedBinaryStorage = config.binaryStorage(),
                lifecycleGeneration = current.lifecycleGeneration + 1L,
            )
        }
    }

    fun bindCurrentConfiguration(
        config: JankHunterConfig,
        storageUpdate: RuntimeStorageUpdate? = null,
    ): JankHunterConfig {
        selectObservationModes(config)
        while (true) {
            val current = configuration.get()
            val selectedStorage = when (storageUpdate) {
                null -> current.selectedBinaryStorage
                is RuntimeStorageUpdate.Replace -> {
                    check(current.storageRequestId == storageUpdate.expectedRequestId) { "Stale storage update" }
                    config.binaryStorage()
                }
                is RuntimeStorageUpdate.Restore -> {
                    if (current.storageRequestId == storageUpdate.expectedRequestId) config.binaryStorage()
                    else current.selectedBinaryStorage
                }
            }
            val effectiveConfig = if (config.binaryStorage() === selectedStorage) {
                config
            } else {
                config.toBuilder().binaryStorage(selectedStorage).build()
            }
            val updated = current.copy(
                config = effectiveConfig,
                selectedBinaryStorage = selectedStorage,
                desiredRuntimeEnabled = config.runtimeEnabled(),
                lifecycleGeneration = current.lifecycleGeneration + 1L,
            )
            if (configuration.compareAndSet(current, updated)) return effectiveConfig
        }
    }

    /** Publishes explicit runtime intent together with the configuration used by the next start. */
    fun bindRuntimeEnabled(enabled: Boolean, invalidateSession: Boolean = true): JankHunterConfig? {
        while (true) {
            val current = configuration.get()
            val config = current.config?.toBuilder()?.runtimeEnabled(enabled)?.build()
            val updated = current.copy(
                config = config,
                desiredRuntimeEnabled = enabled,
                runtimeRequestGeneration = current.runtimeRequestGeneration + 1L,
                lifecycleGeneration = current.lifecycleGeneration + if (invalidateSession) 1L else 0L,
            )
            if (configuration.compareAndSet(current, updated)) return config
        }
    }

    fun clearConfiguration() {
        updateConfiguration { current ->
            RuntimeConfigurationSnapshot(
                initializationGeneration = current.initializationGeneration + 1L,
                lifecycleGeneration = current.lifecycleGeneration + 1L,
            )
        }
    }

    fun applyBinaryStorage(
        lifecycleGeneration: Long,
        requestId: Long,
        storage: JankHunterBinaryStorage?,
    ): Boolean {
        while (true) {
            val current = configuration.get()
            if (current.lifecycleGeneration != lifecycleGeneration) return false
            if (requestId < current.storageRequestId) return false
            if (requestId == current.storageRequestId) return true
            val activeConfig = current.config ?: return false
            val updatedConfig = activeConfig.toBuilder().binaryStorage(storage).build()
            val updated = current.copy(
                config = updatedConfig,
                selectedBinaryStorage = storage,
                storageRequestId = requestId,
            )
            if (configuration.compareAndSet(current, updated)) return true
        }
    }

    private inline fun updateConfiguration(
        transform: (RuntimeConfigurationSnapshot) -> RuntimeConfigurationSnapshot,
    ) {
        while (true) {
            val current = configuration.get()
            if (configuration.compareAndSet(current, transform(current))) return
        }
    }
}

internal data class RuntimeConfigurationSnapshot(
    val config: JankHunterConfig? = null,
    /** Identity of init/clear, without retaining an obsolete host configuration. */
    val initializationGeneration: Long = 0L,
    /** User intent survives session failure and restrictions imposed by the remaining config. */
    val desiredRuntimeEnabled: Boolean = true,
    /** Includes explicit no-op requests, without invalidating an active storage switch. */
    val runtimeRequestGeneration: Long = 0L,
    val selectedBinaryStorage: JankHunterBinaryStorage? = null,
    val lifecycleGeneration: Long = 0L,
    val storageRequestId: Long = 0L,
)

internal enum class RuntimeLifecycle {
    STOPPED,
    STARTING,
    STARTED,
    STOPPING,
}

/** Storage completion may publish independently of lifecycleLock while a session drains. */
internal sealed interface RuntimeStorageUpdate {
    data class Replace(val expectedRequestId: Long) : RuntimeStorageUpdate
    data class Restore(val expectedRequestId: Long) : RuntimeStorageUpdate
}
