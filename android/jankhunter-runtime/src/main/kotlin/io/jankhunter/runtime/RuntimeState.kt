package io.jankhunter.runtime

import android.app.Application
import android.content.Context
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.ProcessLogSnapshotCoordinator
import io.jankhunter.runtime.internal.system.ActivityTracker
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

internal class RuntimeState {
    val lifecycleLock = Any()
    val storageValveLock = Any()
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

    @Volatile
    var config: JankHunterConfig? = null

    /** Immutable build-time baseline used for every non-cumulative remote reconfiguration. */
    @Volatile
    var baseConfig: JankHunterConfig? = null

    /** Last storage selected through the storage valve, including an explicit built-in `null`. */
    @Volatile
    var selectedBinaryStorage: JankHunterBinaryStorage? = null

    /** Changes whenever bound configuration or runtime availability changes. Guarded by lifecycleLock. */
    var lifecycleGeneration: Long = 0L

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
}

internal enum class RuntimeLifecycle {
    STOPPED,
    STARTING,
    STARTED,
    STOPPING,
}
