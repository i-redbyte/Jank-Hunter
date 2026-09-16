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
    val featureGate = RuntimeFeatureGate()
    val collectionEpochs = RuntimeCollectionEpochs()
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

    /** Immutable build-time baseline used for every non-cumulative remote reconfiguration. */
    var baseConfig: JankHunterConfig?
        get() = configuration.get().baseConfig
        set(value) {
            updateConfiguration { current -> current.copy(baseConfig = value) }
        }

    /** Last storage selected through the storage valve, including an explicit built-in `null`. */
    var selectedBinaryStorage: JankHunterBinaryStorage?
        get() = configuration.get().selectedBinaryStorage
        set(value) {
            updateConfiguration { current -> current.copy(selectedBinaryStorage = value) }
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
        updateConfiguration { current ->
            current.copy(
                config = config,
                baseConfig = config,
                selectedBinaryStorage = config.binaryStorage(),
                lifecycleGeneration = current.lifecycleGeneration + 1L,
            )
        }
    }

    fun bindCurrentConfiguration(config: JankHunterConfig): JankHunterConfig {
        while (true) {
            val current = configuration.get()
            val selectedStorage = current.selectedBinaryStorage
            val effectiveConfig = if (config.binaryStorage() === selectedStorage) {
                config
            } else {
                config.toBuilder().binaryStorage(selectedStorage).build()
            }
            val updated = current.copy(
                config = effectiveConfig,
                lifecycleGeneration = current.lifecycleGeneration + 1L,
            )
            if (configuration.compareAndSet(current, updated)) return effectiveConfig
        }
    }

    fun advanceLifecycleGeneration() {
        updateConfiguration { current ->
            current.copy(lifecycleGeneration = current.lifecycleGeneration + 1L)
        }
    }

    fun clearConfiguration() {
        updateConfiguration { current ->
            RuntimeConfigurationSnapshot(lifecycleGeneration = current.lifecycleGeneration + 1L)
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
    val baseConfig: JankHunterConfig? = null,
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
