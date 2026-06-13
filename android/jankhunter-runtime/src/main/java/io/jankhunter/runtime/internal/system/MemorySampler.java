package io.jankhunter.runtime.internal.system;

import android.os.Debug;

import java.util.concurrent.atomic.AtomicBoolean;

import io.jankhunter.runtime.JankHunter;

public final class MemorySampler {
    private final long intervalMs;
    private final AtomicBoolean running = new AtomicBoolean(false);
    private Thread thread;

    public MemorySampler(android.content.Context context, long intervalMs) {
        this.intervalMs = intervalMs;
    }

    public void start() {
        if (!running.compareAndSet(false, true)) {
            return;
        }
        thread = new Thread(new Runnable() {
            @Override
            public void run() {
                loop();
            }
        }, "JankHunterMemorySampler");
        thread.setDaemon(true);
        thread.start();
    }

    public void stop() {
        running.set(false);
        if (thread != null) {
            thread.interrupt();
        }
    }

    private void loop() {
        Debug.MemoryInfo info = new Debug.MemoryInfo();
        while (running.get()) {
            Debug.getMemoryInfo(info);
            Runtime runtime = Runtime.getRuntime();
            long javaHeapKb = (runtime.totalMemory() - runtime.freeMemory()) / 1024L;
            long nativeHeapKb = Debug.getNativeHeapAllocatedSize() / 1024L;
            JankHunter.recordMemory(info.getTotalPss(), javaHeapKb, nativeHeapKb);
            try {
                Thread.sleep(intervalMs);
            } catch (InterruptedException ignored) {
                Thread.currentThread().interrupt();
            }
        }
    }
}
