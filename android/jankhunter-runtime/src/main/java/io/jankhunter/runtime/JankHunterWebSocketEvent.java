package io.jankhunter.runtime;

/**
 * Неизменяемые и ограниченные сведения о жизненном цикле соединения WebSocket.
 *
 * <p>Java используется намеренно для минимального и стабильного двоичного интерфейса без служебных
 * классов и методов, создаваемых компилятором Kotlin. Тела сообщений, полные адреса, заголовки и
 * тексты исключений намеренно не сохраняются.
 */
public final class JankHunterWebSocketEvent {
    public static final int STAGE_OPENED = 1;
    public static final int STAGE_CLOSED = 2;
    public static final int STAGE_FAILED = 3;

    public static final int FAILURE_UNKNOWN = 0;
    public static final int FAILURE_TIMEOUT = 1;
    public static final int FAILURE_CONNECTION = 2;
    public static final int FAILURE_TLS = 3;
    public static final int FAILURE_PROTOCOL = 4;
    public static final int FAILURE_IO = 5;
    public static final int FAILURE_OTHER = 6;

    private final JankHunterContextSnapshot contextSnapshot;
    private final String route;
    private final String owner;
    private final long connectionId;
    private final int stage;
    private final long durationMs;
    private final int statusCode;
    private final int closeCode;
    private final int failureKind;
    private final long textMessages;
    private final long binaryMessages;
    private final long receivedBytes;
    private final int reconnectOrdinal;

    public JankHunterWebSocketEvent(
            JankHunterContextSnapshot contextSnapshot,
            String route,
            String owner,
            long connectionId,
            int stage,
            long durationMs,
            int statusCode,
            int closeCode,
            int failureKind,
            long textMessages,
            long binaryMessages,
            long receivedBytes,
            int reconnectOrdinal) {
        this.contextSnapshot = contextSnapshot;
        this.route = route;
        this.owner = owner;
        this.connectionId = connectionId;
        this.stage = stage;
        this.durationMs = durationMs;
        this.statusCode = statusCode;
        this.closeCode = closeCode;
        this.failureKind = failureKind;
        this.textMessages = textMessages;
        this.binaryMessages = binaryMessages;
        this.receivedBytes = receivedBytes;
        this.reconnectOrdinal = reconnectOrdinal;
    }

    public JankHunterContextSnapshot getContextSnapshot() { return contextSnapshot; }
    public String getRoute() { return route; }
    public String getOwner() { return owner; }
    public long getConnectionId() { return connectionId; }
    public int getStage() { return stage; }
    public long getDurationMs() { return durationMs; }
    public int getStatusCode() { return statusCode; }
    public int getCloseCode() { return closeCode; }
    public int getFailureKind() { return failureKind; }
    public long getTextMessages() { return textMessages; }
    public long getBinaryMessages() { return binaryMessages; }
    public long getReceivedBytes() { return receivedBytes; }
    public int getReconnectOrdinal() { return reconnectOrdinal; }
}
