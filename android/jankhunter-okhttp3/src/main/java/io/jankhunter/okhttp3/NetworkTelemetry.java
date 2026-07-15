package io.jankhunter.okhttp3;

import io.jankhunter.runtime.JankHunter;
import io.jankhunter.runtime.JankHunterContextSnapshot;

/** Package-private fail-open boundary shared by the HTTP and WebSocket state machines. */
interface NetworkTelemetry {
    JankHunterContextSnapshot captureContextSnapshot();

    void recordCounter(String name, long delta);

    void recordGauge(String name, long value);

    void recordHttp(
            JankHunterContextSnapshot contextSnapshot,
            String requestLabel,
            long durationMs,
            long dnsMs,
            long connectMs,
            long ttfbMs,
            int statusClass,
            long responseBodyBytes,
            long requestBodyBytes,
            long flags);
}

/** Package-private singleton; neither the helper nor its seam is exported from the AAR. */
final class RuntimeNetworkTelemetry implements NetworkTelemetry {
    static final RuntimeNetworkTelemetry INSTANCE = new RuntimeNetworkTelemetry();

    private RuntimeNetworkTelemetry() {}

    @Override
    public JankHunterContextSnapshot captureContextSnapshot() {
        return JankHunter.captureContextSnapshot();
    }

    @Override
    public void recordCounter(String name, long delta) {
        JankHunter.recordCounter(name, delta);
    }

    @Override
    public void recordGauge(String name, long value) {
        JankHunter.recordGauge(name, value);
    }

    @Override
    public void recordHttp(
            JankHunterContextSnapshot contextSnapshot,
            String requestLabel,
            long durationMs,
            long dnsMs,
            long connectMs,
            long ttfbMs,
            int statusClass,
            long responseBodyBytes,
            long requestBodyBytes,
            long flags) {
        JankHunter.recordHttpWithContextSnapshot(
                contextSnapshot,
                requestLabel,
                durationMs,
                dnsMs,
                connectMs,
                ttfbMs,
                statusClass,
                responseBodyBytes,
                requestBodyBytes,
                flags);
    }
}
