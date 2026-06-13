package io.jankhunter.runtime;

import java.io.File;

public final class JankHunterConfig {
    private final boolean enabled;
    private final boolean autoStartCollectors;
    private final long mainThreadStallThresholdMs;
    private final long memorySampleIntervalMs;
    private final boolean fpsMonitorEnabled;
    private final long fpsWindowMs;
    private final long jankFrameThresholdMs;
    private final int maxQueueSize;
    private final long maxLogBytes;
    private final File logDirectory;

    private JankHunterConfig(Builder builder) {
        this.enabled = builder.enabled;
        this.autoStartCollectors = builder.autoStartCollectors;
        this.mainThreadStallThresholdMs = builder.mainThreadStallThresholdMs;
        this.memorySampleIntervalMs = builder.memorySampleIntervalMs;
        this.fpsMonitorEnabled = builder.fpsMonitorEnabled;
        this.fpsWindowMs = builder.fpsWindowMs;
        this.jankFrameThresholdMs = builder.jankFrameThresholdMs;
        this.maxQueueSize = builder.maxQueueSize;
        this.maxLogBytes = builder.maxLogBytes;
        this.logDirectory = builder.logDirectory;
    }

    public boolean enabled() {
        return enabled;
    }

    public boolean autoStartCollectors() {
        return autoStartCollectors;
    }

    public long mainThreadStallThresholdMs() {
        return mainThreadStallThresholdMs;
    }

    public long memorySampleIntervalMs() {
        return memorySampleIntervalMs;
    }

    public boolean fpsMonitorEnabled() {
        return fpsMonitorEnabled;
    }

    public long fpsWindowMs() {
        return fpsWindowMs;
    }

    public long jankFrameThresholdMs() {
        return jankFrameThresholdMs;
    }

    public int maxQueueSize() {
        return maxQueueSize;
    }

    public long maxLogBytes() {
        return maxLogBytes;
    }

    public File logDirectory() {
        return logDirectory;
    }

    public static Builder builder() {
        return new Builder();
    }

    public static final class Builder {
        private boolean enabled = true;
        private boolean autoStartCollectors = true;
        private long mainThreadStallThresholdMs = 700;
        private long memorySampleIntervalMs = 10_000;
        private boolean fpsMonitorEnabled = true;
        private long fpsWindowMs = 1_000;
        private long jankFrameThresholdMs = 32;
        private int maxQueueSize = 2048;
        private long maxLogBytes = 5L * 1024L * 1024L;
        private File logDirectory;

        public Builder enabled(boolean value) {
            this.enabled = value;
            return this;
        }

        public Builder autoStartCollectors(boolean value) {
            this.autoStartCollectors = value;
            return this;
        }

        public Builder mainThreadStallThresholdMs(long value) {
            this.mainThreadStallThresholdMs = value;
            return this;
        }

        public Builder memorySampleIntervalMs(long value) {
            this.memorySampleIntervalMs = value;
            return this;
        }

        public Builder fpsMonitorEnabled(boolean value) {
            this.fpsMonitorEnabled = value;
            return this;
        }

        public Builder fpsWindowMs(long value) {
            this.fpsWindowMs = value;
            return this;
        }

        public Builder jankFrameThresholdMs(long value) {
            this.jankFrameThresholdMs = value;
            return this;
        }

        public Builder maxQueueSize(int value) {
            this.maxQueueSize = value;
            return this;
        }

        public Builder maxLogBytes(long value) {
            this.maxLogBytes = value;
            return this;
        }

        public Builder logDirectory(File value) {
            this.logDirectory = value;
            return this;
        }

        public JankHunterConfig build() {
            return new JankHunterConfig(this);
        }
    }
}
