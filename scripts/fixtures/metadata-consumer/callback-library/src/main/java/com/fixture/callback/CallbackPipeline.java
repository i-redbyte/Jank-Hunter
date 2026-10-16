package com.fixture.callback;

import java.util.function.Supplier;

/** Application-independent bounded synchronous callback fixture. No JH calls or logger dependency. */
public final class CallbackPipeline {
    private static final int PRE_INIT = 0;
    private static final int DRAINING = 1;
    private static final int READY = 2;
    private final Object monitor = new Object();
    private final String[] pending = new String[256];
    private final Runnable handled;
    private final Runnable dropped;
    private final Runnable failure;
    private volatile int state = PRE_INIT;
    private int count;
    private Runnable masker = () -> {};

    public CallbackPipeline(Runnable handled, Runnable dropped, Runnable failure) {
        this.handled = handled;
        this.dropped = dropped;
        this.failure = failure;
    }

    public boolean holdsMonitor() { return Thread.holdsLock(monitor); }

    public void logDebug(Supplier<String> supplier, String tag) {
        if (state == READY) {
            deliver(supplier.get());
            return;
        }
        synchronized (monitor) {
            if (state != READY) {
                String value = supplier.get();
                if (count == pending.length) dropped.run();
                else pending[count++] = value;
                return;
            }
        }
        deliver(supplier.get());
    }

    public void logInfo(String value, String tag) { logDebug(() -> value, tag); }

    public void initialize(Runnable masking) {
        synchronized (monitor) {
            if (state != PRE_INIT) throw new IllegalStateException("already initialized");
            state = DRAINING;
            masker = masking;
            try {
                for (int index = 0; index < count; index++) {
                    String value = pending[index];
                    pending[index] = null;
                    deliver(value);
                }
            } finally {
                for (int index = 0; index < count; index++) pending[index] = null;
                count = 0;
                state = READY;
            }
        }
    }

    private void deliver(String value) {
        // Bounded synthetic masking. Never capture application messages or arguments.
        if (value.length() > 64) throw new IllegalArgumentException("fixture payload exceeds limit");
        try {
            masker.run();
            handled.run();
        } catch (RuntimeException | Error error) {
            failure.run();
            throw error;
        }
    }
}
