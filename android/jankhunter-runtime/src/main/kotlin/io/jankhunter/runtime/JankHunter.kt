package io.jankhunter.runtime

import android.app.Application
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.os.SystemClock
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.system.ActivityTracker
import io.jankhunter.runtime.internal.system.FpsMonitor
import io.jankhunter.runtime.internal.system.MainThreadWatchdog
import io.jankhunter.runtime.internal.system.MemorySampler
import java.io.File
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
    private var memorySampler: MemorySampler? = null

    @Volatile
    private var fpsMonitor: FpsMonitor? = null

    @Volatile
    private var screen = "unknown"

    @JvmStatic
    fun init(context: Context?) {
        init(context, JankHunterConfig.builder().build())
    }

    @JvmStatic
    fun init(context: Context?, providedConfig: JankHunterConfig?) {
        if (context == null || providedConfig == null || !providedConfig.enabled()) return
        if (!started.compareAndSet(false, true)) return

        val appContext = context.applicationContext
        config = providedConfig

        val directory = providedConfig.logDirectory() ?: File(appContext.filesDir, "jankhunter")
        val asyncWriter = AsyncLogWriter.open(directory, providedConfig)
        writer = asyncWriter

        val identity = appIdentity(appContext)
        asyncWriter.session(
            identity.versionName,
            identity.versionCode,
            "${Build.MANUFACTURER} ${Build.MODEL}",
            Build.VERSION.SDK_INT,
        )

        if (providedConfig.autoStartCollectors()) {
            if (appContext is Application) {
                appContext.registerActivityLifecycleCallbacks(ActivityTracker())
            }
            watchdog = MainThreadWatchdog(providedConfig.mainThreadStallThresholdMs()).also { it.start() }
            memorySampler = MemorySampler(appContext, providedConfig.memorySampleIntervalMs()).also { it.start() }
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
        watchdog?.stop()
        memorySampler?.stop()
        fpsMonitor?.stop()
        writer?.close()
        watchdog = null
        memorySampler = null
        fpsMonitor = null
        writer = null
        started.set(false)
    }

    @JvmStatic
    fun withOwner(ownerName: String?, runnable: Runnable) {
        val previous = owner.get()
        owner.set(ownerName)
        val start = nowMs()
        try {
            runnable.run()
        } finally {
            val duration = nowMs() - start
            if (duration >= 250) {
                recordStall(ownerName, "explicit_owner_block", duration)
            }
            if (previous == null) {
                owner.remove()
            } else {
                owner.set(previous)
            }
        }
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
        writer?.http(owner, route, durationMs, dnsMs, connectMs, ttfbMs, statusClass, rxBytes, txBytes, flags)
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

    private fun nowMs(): Long = SystemClock.elapsedRealtime()

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
}
