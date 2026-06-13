package io.jankhunter.runtime.internal.system;

import android.os.Handler;
import android.os.Looper;
import android.view.Choreographer;

import java.util.Arrays;
import java.util.concurrent.atomic.AtomicBoolean;

import io.jankhunter.runtime.JankHunter;

public final class FpsMonitor implements Choreographer.FrameCallback {
    private final Handler mainHandler = new Handler(Looper.getMainLooper());
    private final AtomicBoolean running = new AtomicBoolean(false);
    private final long windowNanos;
    private final long jankFrameThresholdMs;

    private Choreographer choreographer;
    private long windowStartNanos;
    private long lastFrameTimeNanos;
    private long frameCount;
    private long jankCount;
    private long[] frameDurationsMs = new long[180];
    private int durationCount;

    public FpsMonitor(long windowMs, long jankFrameThresholdMs) {
        this.windowNanos = Math.max(250L, windowMs) * 1_000_000L;
        this.jankFrameThresholdMs = Math.max(1L, jankFrameThresholdMs);
    }

    public void start() {
        if (!running.compareAndSet(false, true)) {
            return;
        }
        mainHandler.post(new Runnable() {
            @Override
            public void run() {
                choreographer = Choreographer.getInstance();
                reset();
                choreographer.postFrameCallback(FpsMonitor.this);
            }
        });
    }

    public void stop() {
        running.set(false);
        mainHandler.post(new Runnable() {
            @Override
            public void run() {
                if (choreographer != null) {
                    choreographer.removeFrameCallback(FpsMonitor.this);
                }
                reset();
            }
        });
    }

    @Override
    public void doFrame(long frameTimeNanos) {
        if (!running.get()) {
            return;
        }

        if (windowStartNanos == 0L) {
            windowStartNanos = frameTimeNanos;
            lastFrameTimeNanos = frameTimeNanos;
            postNext();
            return;
        }

        long frameDurationMs = Math.max(0L, (frameTimeNanos - lastFrameTimeNanos) / 1_000_000L);
        lastFrameTimeNanos = frameTimeNanos;
        frameCount++;
        if (frameDurationMs >= jankFrameThresholdMs) {
            jankCount++;
        }
        recordDuration(frameDurationMs);

        long elapsedNanos = frameTimeNanos - windowStartNanos;
        if (elapsedNanos >= windowNanos) {
            long windowMs = Math.max(1L, elapsedNanos / 1_000_000L);
            JankHunter.recordUiWindow(
                    JankHunter.currentScreen(),
                    windowMs,
                    frameCount,
                    jankCount,
                    percentile(50),
                    percentile(95),
                    percentile(99)
            );
            JankHunter.recordGauge("ui.fps_x100", (frameCount * 100_000L) / windowMs);
            resetWindow(frameTimeNanos);
        }

        postNext();
    }

    private void postNext() {
        Choreographer local = choreographer;
        if (local != null && running.get()) {
            local.postFrameCallback(this);
        }
    }

    private void reset() {
        windowStartNanos = 0L;
        lastFrameTimeNanos = 0L;
        frameCount = 0L;
        jankCount = 0L;
        durationCount = 0;
    }

    private void resetWindow(long frameTimeNanos) {
        windowStartNanos = frameTimeNanos;
        frameCount = 0L;
        jankCount = 0L;
        durationCount = 0;
    }

    private void recordDuration(long value) {
        if (durationCount == frameDurationsMs.length) {
            frameDurationsMs = Arrays.copyOf(frameDurationsMs, frameDurationsMs.length * 2);
        }
        frameDurationsMs[durationCount++] = value;
    }

    private long percentile(int percentile) {
        if (durationCount == 0) {
            return 0L;
        }
        long[] copy = Arrays.copyOf(frameDurationsMs, durationCount);
        Arrays.sort(copy);
        int index = (int) Math.floor((copy.length - 1) * (percentile / 100.0));
        if (index < 0) {
            index = 0;
        }
        if (index >= copy.length) {
            index = copy.length - 1;
        }
        return copy[index];
    }
}
