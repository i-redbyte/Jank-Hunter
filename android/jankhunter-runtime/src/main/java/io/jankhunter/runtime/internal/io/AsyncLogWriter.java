package io.jankhunter.runtime.internal.io;

import java.io.File;
import java.io.IOException;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;

import io.jankhunter.runtime.JankHunterConfig;

public final class AsyncLogWriter {
    private final BlockingQueue<Action> queue;
    private final AtomicBoolean running = new AtomicBoolean(true);
    private final AtomicLong dropped = new AtomicLong();
    private final Thread worker;

    private BinaryLogWriter writer;

    private AsyncLogWriter(File file, JankHunterConfig config) throws IOException {
        this.queue = new ArrayBlockingQueue<Action>(config.maxQueueSize());
        this.writer = new BinaryLogWriter(file);
        this.worker = new Thread(new Runnable() {
            @Override
            public void run() {
                loop();
            }
        }, "JankHunterWriter");
        this.worker.setDaemon(true);
        this.worker.start();
    }

    public static AsyncLogWriter open(File directory, JankHunterConfig config) {
        if (!directory.exists() && !directory.mkdirs()) {
            throw new IllegalStateException("Cannot create Jank Hunter log directory: " + directory);
        }
        File file = new File(directory, "session-" + System.currentTimeMillis() + ".jhlog");
        try {
            return new AsyncLogWriter(file, config);
        } catch (IOException e) {
            throw new IllegalStateException("Cannot open Jank Hunter log file", e);
        }
    }

    public void session(final String appVersion, final String build, final String device, final int sdkInt) {
        offer(new Action() {
            @Override
            public void write(BinaryLogWriter writer) throws IOException {
                writer.session(appVersion, build, device, sdkInt);
            }
        });
    }

    public void screen(final String screen) {
        offer(new Action() {
            @Override
            public void write(BinaryLogWriter writer) throws IOException {
                writer.screen(screen);
            }
        });
    }

    public void http(final String owner, final String route, final long durationMs, final long dnsMs, final long connectMs, final long ttfbMs, final int statusClass, final long rxBytes, final long txBytes, final long flags) {
        offer(new Action() {
            @Override
            public void write(BinaryLogWriter writer) throws IOException {
                writer.http(owner, route, durationMs, dnsMs, connectMs, ttfbMs, statusClass, rxBytes, txBytes, flags);
            }
        });
    }

    public void stall(final String owner, final String stackHint, final long durationMs) {
        offer(new Action() {
            @Override
            public void write(BinaryLogWriter writer) throws IOException {
                writer.stall(owner, stackHint, durationMs);
            }
        });
    }

    public void memory(final long pssKb, final long javaHeapKb, final long nativeHeapKb) {
        offer(new Action() {
            @Override
            public void write(BinaryLogWriter writer) throws IOException {
                writer.memory(pssKb, javaHeapKb, nativeHeapKb);
            }
        });
    }

    public void uiWindow(final String screen, final long windowMs, final long frameCount, final long jankCount, final long p50Ms, final long p95Ms, final long p99Ms) {
        offer(new Action() {
            @Override
            public void write(BinaryLogWriter writer) throws IOException {
                writer.uiWindow(screen, windowMs, frameCount, jankCount, p50Ms, p95Ms, p99Ms);
            }
        });
    }

    public void counter(final String name, final long value) {
        offer(new Action() {
            @Override
            public void write(BinaryLogWriter writer) throws IOException {
                writer.counter(name, value);
            }
        });
    }

    public void gauge(final String name, final long value) {
        offer(new Action() {
            @Override
            public void write(BinaryLogWriter writer) throws IOException {
                writer.gauge(name, value);
            }
        });
    }

    public void close() {
        running.set(false);
        worker.interrupt();
        try {
            worker.join(1000);
        } catch (InterruptedException ignored) {
            Thread.currentThread().interrupt();
        }
        try {
            writer.close();
        } catch (IOException ignored) {
        }
    }

    private void offer(Action action) {
        if (!queue.offer(action)) {
            dropped.incrementAndGet();
        }
    }

    private void loop() {
        while (running.get() || !queue.isEmpty()) {
            try {
                Action action = queue.poll(250, TimeUnit.MILLISECONDS);
                if (action != null) {
                    action.write(writer);
                }
                long lost = dropped.getAndSet(0);
                if (lost > 0) {
                    writer.counter("jankhunter.events_dropped.count", lost);
                }
            } catch (InterruptedException ignored) {
                Thread.currentThread().interrupt();
            } catch (IOException ignored) {
            }
        }
    }

    private interface Action {
        void write(BinaryLogWriter writer) throws IOException;
    }
}
