package io.jankhunter.runtime

import android.app.Application
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.os.SystemClock
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.system.ActivityTracker
import io.jankhunter.runtime.internal.system.DeviceSnapshots
import io.jankhunter.runtime.internal.system.FpsMonitor
import io.jankhunter.runtime.internal.system.MainLooperDispatchMonitor
import io.jankhunter.runtime.internal.system.MainThreadWatchdog
import io.jankhunter.runtime.internal.system.MemorySampler
import io.jankhunter.runtime.internal.system.MemoryTrimReporter
import io.jankhunter.runtime.internal.system.ObjectRetentionWatcher
import io.jankhunter.runtime.internal.system.ProcessNames
import io.jankhunter.runtime.internal.system.ProcessExitReporter
import io.jankhunter.runtime.internal.system.SystemContextSampler
import java.io.File
import java.util.concurrent.Callable
import java.util.concurrent.Executor
import java.util.concurrent.ExecutorService
import java.util.concurrent.ThreadPoolExecutor
import java.util.concurrent.atomic.AtomicBoolean

object JankHunter {
    private val started = AtomicBoolean(false)
    private val owner = ThreadLocal<String>()

    @Volatile
    private var writer: AsyncLogWriter? = null

    @Volatile
    private var config: JankHunterConfig? = null

    @Volatile
    private var watchdog: MainThreadWatchdog? = null

    @Volatile
    private var dispatchMonitor: MainLooperDispatchMonitor? = null

    @Volatile
    private var memorySampler: MemorySampler? = null

    @Volatile
    private var memoryTrimReporter: MemoryTrimReporter? = null

    @Volatile
    private var componentCallbackContext: Context? = null

    @Volatile
    private var systemContextSampler: SystemContextSampler? = null

    @Volatile
    private var objectRetentionWatcher: ObjectRetentionWatcher? = null

    @Volatile
    private var fpsMonitor: FpsMonitor? = null

    @Volatile
    private var application: Application? = null

    @Volatile
    private var activityTracker: ActivityTracker? = null

    @Volatile
    private var screen = "unknown"

    @JvmStatic
    fun init(context: Context?) {
        init(context, JankHunterConfig.builder().build())
    }

    @JvmStatic
    fun init(context: Context?, providedConfig: JankHunterConfig?) {
        if (context == null || providedConfig == null || !providedConfig.enabled()) return

        val appContext = context.applicationContext ?: context
        val processName = ProcessNames.current(appContext)
        val mainProcessName = appContext.packageName
        if (!providedConfig.isProcessAllowed(processName, mainProcessName)) return
        if (!started.compareAndSet(false, true)) return

        config = providedConfig

        val directory = providedConfig.logDirectory() ?: File(appContext.filesDir, "jankhunter")
        val redactedProcessName = providedConfig.redactProcessName(processName)
            ?.takeIf { it.isNotBlank() }
            ?: "unknown"
        val asyncWriter = AsyncLogWriter.open(
            directory,
            providedConfig,
            ProcessNames.safeFileSuffix(redactedProcessName, mainProcessName),
        )
        writer = asyncWriter

        val identity = appIdentity(appContext)
        val device = DeviceSnapshots.current()
        asyncWriter.session(
            identity.versionName,
            identity.versionCode,
            device.displayName,
            Build.VERSION.SDK_INT,
            redactedProcessName,
            device.androidRelease,
            device.securityPatch,
            device.primaryAbi,
            device.supportedAbis,
            device.manufacturer,
            device.brand,
            device.hardware,
            device.board,
            device.product,
        )

        if (providedConfig.autoStartCollectors()) {
            if (appContext is Application) {
                application = appContext
                activityTracker = ActivityTracker(providedConfig.jankStatsEnabled()).also {
                    appContext.registerActivityLifecycleCallbacks(it)
                }
            }
            watchdog = MainThreadWatchdog(providedConfig.mainThreadStallThresholdMs()).also { it.start() }
            dispatchMonitor = MainLooperDispatchMonitor(providedConfig.mainThreadStallThresholdMs()).also { it.start() }
            memoryTrimReporter = MemoryTrimReporter().also {
                appContext.registerComponentCallbacks(it)
                componentCallbackContext = appContext
            }
            memorySampler = MemorySampler(appContext, providedConfig.memorySampleIntervalMs()).also { it.start() }
            if (providedConfig.systemSamplerEnabled()) {
                systemContextSampler = SystemContextSampler(
                    appContext,
                    providedConfig.systemSampleIntervalMs(),
                ).also { it.start() }
            }
            if (providedConfig.processExitInfoEnabled()) {
                ProcessExitReporter.report(appContext)
            }
            if (providedConfig.objectWatcherEnabled()) {
                objectRetentionWatcher = ObjectRetentionWatcher(
                    providedConfig.retainedObjectDelayMs(),
                    providedConfig.retainedObjectForceGcEnabled(),
                ).also { it.start() }
            }
            if (providedConfig.fpsMonitorEnabled()) {
                fpsMonitor = FpsMonitor(
                    providedConfig.fpsWindowMs(),
                    providedConfig.jankFrameThresholdMs(),
                ).also { it.start() }
            }
        }
    }

    @JvmStatic
    fun isStarted(): Boolean = started.get()

    @JvmStatic
    fun shutdown() {
        activityTracker?.let { tracker ->
            application?.unregisterActivityLifecycleCallbacks(tracker)
            tracker.close()
        }
        watchdog?.stop()
        dispatchMonitor?.stop()
        memoryTrimReporter?.let { reporter ->
            componentCallbackContext?.unregisterComponentCallbacks(reporter)
        }
        memorySampler?.stop()
        systemContextSampler?.stop()
        objectRetentionWatcher?.stop()
        fpsMonitor?.stop()
        writer?.close()
        activityTracker = null
        application = null
        watchdog = null
        dispatchMonitor = null
        memoryTrimReporter = null
        componentCallbackContext = null
        memorySampler = null
        systemContextSampler = null
        objectRetentionWatcher = null
        fpsMonitor = null
        writer = null
        started.set(false)
    }

    @JvmStatic
    fun withOwner(ownerName: String?, runnable: Runnable) {
        val start = nowMs()
        try {
            callWithOwner(ownerName) {
                runnable.run()
            }
        } finally {
            val duration = nowMs() - start
            if (duration >= 250) {
                recordStall(ownerName, "explicit_owner_block", duration)
            }
        }
    }

    @JvmStatic
    fun <T> withOwner(ownerName: String?, callable: Callable<T>): T {
        val start = nowMs()
        try {
            return callWithOwner(ownerName) {
                callable.call()
            }
        } finally {
            val duration = nowMs() - start
            if (duration >= 250) {
                recordStall(ownerName, "explicit_owner_block", duration)
            }
        }
    }

    @JvmStatic
    fun wrapRunnable(runnable: Runnable?, ownerName: String?): Runnable? {
        if (runnable == null || runnable is JankHunterRunnable) return runnable
        return JankHunterRunnable(runnable, ownerName)
    }

    @JvmStatic
    fun <T> wrapCallable(callable: Callable<T>?, ownerName: String?): Callable<T>? {
        if (callable == null || callable is JankHunterCallable<*>) return callable
        return JankHunterCallable(callable, ownerName)
    }

    @JvmStatic
    fun wrapExecutor(executor: Executor?, name: String?, ownerName: String? = name): Executor? {
        if (executor == null || executor is JankHunterExecutor || executor is JankHunterExecutorService) return executor
        return if (executor is ExecutorService) {
            JankHunterExecutorService(executor, name, ownerName)
        } else {
            JankHunterExecutor(executor, name, ownerName)
        }
    }

    @JvmStatic
    fun wrapExecutorService(executor: ExecutorService?, name: String?, ownerName: String? = name): ExecutorService? {
        if (executor == null || executor is JankHunterExecutorService) return executor
        return JankHunterExecutorService(executor, name, ownerName)
    }

    @JvmStatic
    fun currentOwner(): String = owner.get() ?: "unknown"

    @JvmStatic
    fun currentScreen(): String = screen

    @JvmStatic
    fun setScreen(screenName: String?) {
        screen = screenName?.takeIf { it.isNotEmpty() } ?: "unknown"
        writer?.screen(screen)
    }

    @JvmStatic
    fun flush() {
        writer?.flush()
    }

    @JvmStatic
    fun recordHttp(
        owner: String?,
        route: String?,
        durationMs: Long,
        dnsMs: Long,
        connectMs: Long,
        ttfbMs: Long,
        statusClass: Int,
        rxBytes: Long,
        txBytes: Long,
        flags: Long,
    ) {
        writer?.http(
            owner,
            config?.redactRoute(route) ?: route,
            durationMs,
            dnsMs,
            connectMs,
            ttfbMs,
            statusClass,
            rxBytes,
            txBytes,
            flags,
        )
    }

    @JvmStatic
    fun recordStall(owner: String?, stackHint: String?, durationMs: Long) {
        writer?.stall(owner, stackHint, durationMs)
    }

    @JvmStatic
    fun recordMemory(pssKb: Long, javaHeapKb: Long, nativeHeapKb: Long) {
        writer?.memory(pssKb, javaHeapKb, nativeHeapKb)
    }

    @JvmStatic
    fun recordRetained(className: String?, ageMs: Long, count: Long) {
        writer?.retained(className, ageMs, count)
    }

    @JvmStatic
    fun watchObject(instance: Any?, description: String? = null) {
        objectRetentionWatcher?.watch(instance, description)
    }

    @JvmStatic
    fun watchActivity(activity: android.app.Activity?) {
        watchObject(activity, activity?.javaClass?.name)
    }

    @JvmStatic
    fun watchFragment(fragmentLike: Any?, name: String? = null) {
        watchObject(fragmentLike, name ?: fragmentLike?.javaClass?.name)
    }

    @JvmStatic
    fun watchCloseable(closeable: java.io.Closeable?, name: String? = null) {
        watchObject(closeable, name ?: closeable?.javaClass?.name)
    }

    @JvmStatic
    fun recordContext(
        networkKind: Int,
        batteryPct: Int,
        availMemoryKb: Long,
        batteryState: Int,
        batteryTempDeciC: Int,
        lowMemory: Boolean,
        networkMetered: Boolean,
        networkValidated: Boolean,
        rxBytes: Long,
        txBytes: Long,
        totalMemoryKb: Long,
        freeStorageKb: Long,
        totalStorageKb: Long,
        networkVpn: Boolean,
    ) {
        writer?.context(
            networkKind,
            batteryPct,
            availMemoryKb,
            batteryState,
            batteryTempDeciC,
            lowMemory,
            networkMetered,
            networkValidated,
            rxBytes,
            txBytes,
            totalMemoryKb,
            freeStorageKb,
            totalStorageKb,
            networkVpn,
        )
    }

    @JvmStatic
    fun recordUiWindow(
        screen: String?,
        windowMs: Long,
        frameCount: Long,
        jankCount: Long,
        p50Ms: Long,
        p95Ms: Long,
        p99Ms: Long,
    ) {
        writer?.uiWindow(screen, windowMs, frameCount, jankCount, p50Ms, p95Ms, p99Ms)
    }

    @JvmStatic
    fun recordCounter(name: String?, value: Long) {
        writer?.counter(name, value)
    }

    @JvmStatic
    fun recordGauge(name: String?, value: Long) {
        writer?.gauge(name, value)
    }

    internal fun <T> callWithOwner(ownerName: String?, block: () -> T): T {
        val previous = owner.get()
        owner.set(ownerName)
        try {
            return block()
        } finally {
            if (previous == null) {
                owner.remove()
            } else {
                owner.set(previous)
            }
        }
    }

    internal fun recordWrappedWork(ownerName: String?, kind: String, durationMs: Long, failed: Boolean) {
        val owner = metricOwner(ownerName)
        if (failed) {
            recordCounter("owner.$owner.$kind.failure.count", 1)
        }
        if (durationMs >= WRAPPED_WORK_GAUGE_THRESHOLD_MS) {
            recordGauge("owner.$owner.$kind.duration_ms", durationMs)
        }
    }

    internal fun recordExecutorWait(name: String?, ownerName: String?, waitMs: Long) {
        val executorName = metricExecutorName(name)
        if (waitMs > 0) {
            recordGauge("executor.$executorName.wait_ms", waitMs)
        }
        recordCounter("executor.$executorName.started.count", 1)
        ownerName?.takeIf { it.isNotBlank() }?.let {
            recordCounter("owner.${metricOwner(it)}.executor.started.count", 1)
        }
    }

    internal fun recordExecutorSnapshot(name: String?, executor: Executor, queued: Int) {
        val executorName = metricExecutorName(name)
        recordGauge("executor.$executorName.queue_depth", queued.toLong())
        if (executor is ThreadPoolExecutor) {
            recordGauge("executor.$executorName.active_count", executor.snapshotActiveCount().toLong())
            recordGauge("executor.$executorName.pool_size", executor.poolSize.toLong())
            recordGauge("executor.$executorName.completed_task_count", executor.completedTaskCount)
        }
    }

    internal fun runExecutorTask(
        name: String?,
        ownerName: String?,
        command: Runnable,
        clock: () -> Long = ::nowMs,
    ) {
        val start = clock()
        var failed = false
        try {
            callWithOwner(ownerName) {
                command.run()
            }
        } catch (throwable: Throwable) {
            failed = true
            throw throwable
        } finally {
            val executorName = metricExecutorName(name)
            val durationMs = clock() - start
            recordGauge("executor.$executorName.service_ms", durationMs)
            if (failed) {
                recordCounter("executor.$executorName.failure.count", 1)
            }
            recordWrappedWork(ownerName, "executor", durationMs, failed)
        }
    }

    internal fun recordMainThreadDispatch(durationMs: Long, thresholdMs: Long, source: String?) {
        recordGauge("main_thread.dispatch.duration_ms", durationMs)
        if (durationMs >= thresholdMs) {
            val overThresholdMs = durationMs - thresholdMs
            recordCounter("main_thread.dispatch.slow.count", 1)
            recordGauge("main_thread.dispatch.over_threshold_ms", overThresholdMs)
            recordCounter("screen.${metricOwner(currentScreen())}.main_thread.slow_dispatch.count", 1)
            recordCounter("main_thread.dispatch.source.${metricOwner(source)}.slow.count", 1)
        }
    }

    private fun nowMs(): Long = SystemClock.elapsedRealtime()

    private fun metricOwner(ownerName: String?): String {
        return ownerName
            ?.takeIf { it.isNotBlank() }
            ?.replace(Regex("\\s+"), "_")
            ?: "unknown"
    }

    private fun appIdentity(context: Context): AppIdentity {
        return try {
            val info = context.packageManager.getPackageInfo(context.packageName, 0)
            val versionName = info.versionName ?: "unknown"
            val versionCode = if (Build.VERSION.SDK_INT >= 28) {
                info.longVersionCode.toString()
            } else {
                @Suppress("DEPRECATION")
                info.versionCode.toString()
            }
            AppIdentity(versionName, versionCode)
        } catch (_: PackageManager.NameNotFoundException) {
            AppIdentity("unknown", "unknown")
        }
    }

    private data class AppIdentity(
        val versionName: String,
        val versionCode: String,
    )

    private const val WRAPPED_WORK_GAUGE_THRESHOLD_MS = 50L
}
