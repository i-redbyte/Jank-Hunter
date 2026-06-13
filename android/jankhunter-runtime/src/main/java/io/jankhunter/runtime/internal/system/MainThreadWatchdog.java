package io.jankhunter.runtime.internal.system;

import android.os.Handler;
import android.os.Looper;
import android.os.SystemClock;

import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;

import io.jankhunter.runtime.JankHunter;

public final class MainThreadWatchdog {
    private final Handler mainHandler = new Handler(Looper.getMainLooper());
    private final AtomicBoolean running = new AtomicBoolean(false);
    private final AtomicLong lastBeatMs = new AtomicLong();
    private final long thresholdMs;
    private Thread thread;

    public MainThreadWatchdog(long thresholdMs) {
        this.thresholdMs = thresholdMs;
    }

    public void start() {
        if (!running.compareAndSet(false, true)) {
            return;
        }
        beat();
        thread = new Thread(new Runnable() {
            @Override
            public void run() {
                loop();
            }
        }, "JankHunterMainWatchdog");
        thread.setDaemon(true);
        thread.start();
    }

    public void stop() {
        running.set(false);
        if (thread != null) {
            thread.interrupt();
        }
    }

    private void beat() {
        mainHandler.post(new Runnable() {
            @Override
            public void run() {
                lastBeatMs.set(SystemClock.elapsedRealtime());
                if (running.get()) {
                    mainHandler.postDelayed(this, Math.max(100, thresholdMs / 2));
                }
            }
        });
    }

    private void loop() {
        long lastReportedAt = 0;
        while (running.get()) {
            long now = SystemClock.elapsedRealtime();
            long delay = now - lastBeatMs.get();
            if (delay >= thresholdMs && now - lastReportedAt >= thresholdMs) {
                lastReportedAt = now;
                StackTraceElement[] stack = Looper.getMainLooper().getThread().getStackTrace();
                String stackHint = stack.length == 0 ? "unknown" : stack[0].getClassName() + "." + stack[0].getMethodName();
                JankHunter.recordStall(JankHunter.currentOwner(), stackHint, delay);
            }
            try {
                Thread.sleep(Math.max(100, thresholdMs / 2));
            } catch (InterruptedException ignored) {
                Thread.currentThread().interrupt();
            }
        }
    }
}
