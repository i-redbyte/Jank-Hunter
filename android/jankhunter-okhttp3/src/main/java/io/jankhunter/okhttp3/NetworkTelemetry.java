package io.jankhunter.okhttp3;

import io.jankhunter.runtime.JankHunterNetworkRuntime;
import io.jankhunter.runtime.JankHunterContextSnapshot;
import io.jankhunter.runtime.JankHunterHttpEvent;
import io.jankhunter.runtime.JankHunterWebSocketEvent;

/**
 * Внутренняя отказоустойчивая граница для сбора HTTP- и WebSocket-событий.
 *
 * <p>Java используется намеренно, чтобы сохранить настоящую пакетную видимость на уровне JVM.
 * Kotlin {@code internal} сделал бы этот интерфейс открытым в байткоде и частью двоичного интерфейса
 * библиотеки.
 */
interface NetworkTelemetry {
    default boolean isHttpCollectionEnabled() {
        return true;
    }

    JankHunterContextSnapshot captureContextSnapshot();

    void recordHttp(JankHunterHttpEvent event);

    void recordWebSocket(JankHunterWebSocketEvent event);
}

/**
 * Внутренняя реализация, передающая сетевые события в среду выполнения JankHunter.
 *
 * <p>Java сохраняет пакетную видимость класса и не позволяет случайно экспортировать эту реализацию
 * из AAR.
 */
final class RuntimeNetworkTelemetry implements NetworkTelemetry {
    static final RuntimeNetworkTelemetry INSTANCE = new RuntimeNetworkTelemetry();

    private RuntimeNetworkTelemetry() {}

    @Override
    public boolean isHttpCollectionEnabled() {
        return JankHunterNetworkRuntime.isHttpActive();
    }

    @Override
    public JankHunterContextSnapshot captureContextSnapshot() {
        return JankHunterNetworkRuntime.captureContext();
    }

    @Override
    public void recordHttp(JankHunterHttpEvent event) {
        JankHunterNetworkRuntime.recordHttp(event);
    }

    @Override
    public void recordWebSocket(JankHunterWebSocketEvent event) {
        JankHunterNetworkRuntime.recordWebSocket(event);
    }
}
