package io.jankhunter.runtime;

import android.app.Application;
import android.content.Context;
import android.content.pm.PackageInfo;
import android.content.pm.PackageManager;
import android.os.Build;

import java.io.File;
import java.util.concurrent.atomic.AtomicBoolean;

import io.jankhunter.runtime.internal.io.AsyncLogWriter;
import io.jankhunter.runtime.internal.system.ActivityTracker;
import io.jankhunter.runtime.internal.system.FpsMonitor;
import io.jankhunter.runtime.internal.system.MainThreadWatchdog;
import io.jankhunter.runtime.internal.system.MemorySampler;

public final class JankHunter {
    private static final AtomicBoolean STARTED = new AtomicBoolean(false);
    private static final ThreadLocal<String> OWNER = new ThreadLocal<String>();

    private static volatile AsyncLogWriter writer;
    private static volatile JankHunterConfig config;
    private static volatile MainThreadWatchdog watchdog;
    private static volatile MemorySampler memorySampler;
    private static volatile FpsMonitor fpsMonitor;
    private static volatile String currentScreen = "unknown";

    private JankHunter() {
    }

    public static void init(Context context) {
        init(context, JankHunterConfig.builder().build());
    }

    public static void init(Context context, JankHunterConfig providedConfig) {
        if (context == null || providedConfig == null || !providedConfig.enabled()) {
            return;
        }
        if (!STARTED.compareAndSet(false, true)) {
            return;
        }

        Context appContext = context.getApplicationContext();
        config = providedConfig;

        File directory = providedConfig.logDirectory();
        if (directory == null) {
            directory = new File(appContext.getFilesDir(), "jankhunter");
        }

        AsyncLogWriter asyncWriter = AsyncLogWriter.open(directory, providedConfig);
        writer = asyncWriter;
        AppIdentity identity = appIdentity(appContext);
        asyncWriter.session(identity.versionName, identity.versionCode, Build.MANUFACTURER + " " + Build.MODEL, Build.VERSION.SDK_INT);

        if (providedConfig.autoStartCollectors()) {
            if (appContext instanceof Application) {
                ((Application) appContext).registerActivityLifecycleCallbacks(new ActivityTracker());
            }
            watchdog = new MainThreadWatchdog(providedConfig.mainThreadStallThresholdMs());
            watchdog.start();
            memorySampler = new MemorySampler(appContext, providedConfig.memorySampleIntervalMs());
            memorySampler.start();
            if (providedConfig.fpsMonitorEnabled()) {
                fpsMonitor = new FpsMonitor(providedConfig.fpsWindowMs(), providedConfig.jankFrameThresholdMs());
                fpsMonitor.start();
            }
        }
    }

    public static boolean isStarted() {
        return STARTED.get();
    }

    public static void shutdown() {
        MainThreadWatchdog localWatchdog = watchdog;
        if (localWatchdog != null) {
            localWatchdog.stop();
        }
        MemorySampler localSampler = memorySampler;
        if (localSampler != null) {
            localSampler.stop();
        }
        FpsMonitor localFpsMonitor = fpsMonitor;
        if (localFpsMonitor != null) {
            localFpsMonitor.stop();
        }
        AsyncLogWriter localWriter = writer;
        if (localWriter != null) {
            localWriter.close();
        }
        STARTED.set(false);
    }

    public static void withOwner(String owner, Runnable runnable) {
        String previous = OWNER.get();
        OWNER.set(owner);
        long start = nowMs();
        try {
            runnable.run();
        } finally {
            long duration = nowMs() - start;
            if (duration >= 250) {
                recordStall(owner, "explicit_owner_block", duration);
            }
            if (previous == null) {
                OWNER.remove();
            } else {
                OWNER.set(previous);
            }
        }
    }

    public static String currentOwner() {
        String owner = OWNER.get();
        return owner == null ? "unknown" : owner;
    }

    public static String currentScreen() {
        return currentScreen;
    }

    public static void setScreen(String screen) {
        currentScreen = screen == null || screen.length() == 0 ? "unknown" : screen;
        AsyncLogWriter local = writer;
        if (local != null) {
            local.screen(currentScreen);
        }
    }

    public static void recordHttp(String owner, String route, long durationMs, long dnsMs, long connectMs, long ttfbMs, int statusClass, long rxBytes, long txBytes, long flags) {
        AsyncLogWriter local = writer;
        if (local != null) {
            local.http(owner, route, durationMs, dnsMs, connectMs, ttfbMs, statusClass, rxBytes, txBytes, flags);
        }
    }

    public static void recordStall(String owner, String stackHint, long durationMs) {
        AsyncLogWriter local = writer;
        if (local != null) {
            local.stall(owner, stackHint, durationMs);
        }
    }

    public static void recordMemory(long pssKb, long javaHeapKb, long nativeHeapKb) {
        AsyncLogWriter local = writer;
        if (local != null) {
            local.memory(pssKb, javaHeapKb, nativeHeapKb);
        }
    }

    public static void recordUiWindow(String screen, long windowMs, long frameCount, long jankCount, long p50Ms, long p95Ms, long p99Ms) {
        AsyncLogWriter local = writer;
        if (local != null) {
            local.uiWindow(screen, windowMs, frameCount, jankCount, p50Ms, p95Ms, p99Ms);
        }
    }

    public static void recordCounter(String name, long value) {
        AsyncLogWriter local = writer;
        if (local != null) {
            local.counter(name, value);
        }
    }

    public static void recordGauge(String name, long value) {
        AsyncLogWriter local = writer;
        if (local != null) {
            local.gauge(name, value);
        }
    }

    private static long nowMs() {
        return android.os.SystemClock.elapsedRealtime();
    }

    private static AppIdentity appIdentity(Context context) {
        try {
            PackageInfo info = context.getPackageManager().getPackageInfo(context.getPackageName(), 0);
            String versionName = info.versionName == null ? "unknown" : info.versionName;
            String versionCode;
            if (Build.VERSION.SDK_INT >= 28) {
                versionCode = String.valueOf(info.getLongVersionCode());
            } else {
                versionCode = String.valueOf(info.versionCode);
            }
            return new AppIdentity(versionName, versionCode);
        } catch (PackageManager.NameNotFoundException e) {
            return new AppIdentity("unknown", "unknown");
        }
    }

    private static final class AppIdentity {
        final String versionName;
        final String versionCode;

        AppIdentity(String versionName, String versionCode) {
            this.versionName = versionName;
            this.versionCode = versionCode;
        }
    }
}
