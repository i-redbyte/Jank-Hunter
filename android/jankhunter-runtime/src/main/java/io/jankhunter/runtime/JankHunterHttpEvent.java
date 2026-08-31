package io.jankhunter.runtime;

/**
 * Неизменяемые и ограниченные сведения об HTTP-запросе, передаваемые в JankHunter.
 *
 * <p>Java используется намеренно для минимального и стабильного двоичного интерфейса без служебных
 * классов и методов, создаваемых компилятором Kotlin. Объект не хранит запрос, ответ, полный адрес,
 * узел, IP-адрес или исключение.
 */
public final class JankHunterHttpEvent {
    public static final int FAILURE_PHASE_UNKNOWN = 0;
    public static final int FAILURE_PHASE_CALL = 1;
    public static final int FAILURE_PHASE_QUEUE = 2;
    public static final int FAILURE_PHASE_DNS = 3;
    public static final int FAILURE_PHASE_CONNECT = 4;
    public static final int FAILURE_PHASE_TLS = 5;
    public static final int FAILURE_PHASE_REQUEST = 6;
    public static final int FAILURE_PHASE_RESPONSE = 7;
    public static final int FAILURE_PHASE_CANCELLED = 8;

    public static final int FAILURE_KIND_UNKNOWN = 0;
    public static final int FAILURE_KIND_DNS = 1;
    public static final int FAILURE_KIND_TIMEOUT = 2;
    public static final int FAILURE_KIND_CONNECTION = 3;
    public static final int FAILURE_KIND_TLS = 4;
    public static final int FAILURE_KIND_PROTOCOL = 5;
    public static final int FAILURE_KIND_CANCELLED = 6;
    public static final int FAILURE_KIND_IO = 7;
    public static final int FAILURE_KIND_OTHER = 8;

    public static final int PROTOCOL_UNKNOWN = 0;
    public static final int PROTOCOL_HTTP_1_0 = 1;
    public static final int PROTOCOL_HTTP_1_1 = 2;
    public static final int PROTOCOL_HTTP_2 = 3;
    public static final int PROTOCOL_HTTP_3 = 4;

    private final JankHunterContextSnapshot contextSnapshot;
    private final String requestLabel;
    private final String serviceAlias;
    private final long durationMs;
    private final long queueMs;
    private final long dnsMs;
    private final long connectMs;
    private final long tlsMs;
    private final long requestMs;
    private final long ttfbMs;
    private final long responseMs;
    private final int statusCode;
    private final int failurePhase;
    private final int failureKind;
    private final int protocol;
    private final long responseBodyBytes;
    private final long requestBodyBytes;
    private final int attempts;
    private final int dnsAttempts;
    private final int connectAttempts;
    private final int tlsAttempts;
    private final int connectFailures;
    private final int tlsFailures;
    private final int redirects;
    private final long flags;

    /**
     * Создаёт готовый снимок без промежуточного построителя: прямой конструктор намеренно сохраняет
     * одну аллокацию на завершённый HTTP-запрос в горячем сетевом пути.
     */
    public JankHunterHttpEvent(
            JankHunterContextSnapshot contextSnapshot,
            String requestLabel,
            String serviceAlias,
            long durationMs,
            long queueMs,
            long dnsMs,
            long connectMs,
            long tlsMs,
            long requestMs,
            long ttfbMs,
            long responseMs,
            int statusCode,
            int failurePhase,
            int failureKind,
            int protocol,
            long responseBodyBytes,
            long requestBodyBytes,
            int attempts,
            int dnsAttempts,
            int connectAttempts,
            int tlsAttempts,
            int connectFailures,
            int tlsFailures,
            int redirects,
            long flags) {
        this.contextSnapshot = contextSnapshot;
        this.requestLabel = requestLabel;
        this.serviceAlias = serviceAlias;
        this.durationMs = durationMs;
        this.queueMs = queueMs;
        this.dnsMs = dnsMs;
        this.connectMs = connectMs;
        this.tlsMs = tlsMs;
        this.requestMs = requestMs;
        this.ttfbMs = ttfbMs;
        this.responseMs = responseMs;
        this.statusCode = statusCode;
        this.failurePhase = failurePhase;
        this.failureKind = failureKind;
        this.protocol = protocol;
        this.responseBodyBytes = responseBodyBytes;
        this.requestBodyBytes = requestBodyBytes;
        this.attempts = attempts;
        this.dnsAttempts = dnsAttempts;
        this.connectAttempts = connectAttempts;
        this.tlsAttempts = tlsAttempts;
        this.connectFailures = connectFailures;
        this.tlsFailures = tlsFailures;
        this.redirects = redirects;
        this.flags = flags;
    }

    public JankHunterContextSnapshot getContextSnapshot() { return contextSnapshot; }
    public String getRequestLabel() { return requestLabel; }
    public String getServiceAlias() { return serviceAlias; }
    public long getDurationMs() { return durationMs; }
    public long getQueueMs() { return queueMs; }
    public long getDnsMs() { return dnsMs; }
    public long getConnectMs() { return connectMs; }
    public long getTlsMs() { return tlsMs; }
    public long getRequestMs() { return requestMs; }
    public long getTtfbMs() { return ttfbMs; }
    public long getResponseMs() { return responseMs; }
    public int getStatusCode() { return statusCode; }
    public int getFailurePhase() { return failurePhase; }
    public int getFailureKind() { return failureKind; }
    public int getProtocol() { return protocol; }
    public long getResponseBodyBytes() { return responseBodyBytes; }
    public long getRequestBodyBytes() { return requestBodyBytes; }
    public int getAttempts() { return attempts; }
    public int getDnsAttempts() { return dnsAttempts; }
    public int getConnectAttempts() { return connectAttempts; }
    public int getTlsAttempts() { return tlsAttempts; }
    public int getConnectFailures() { return connectFailures; }
    public int getTlsFailures() { return tlsFailures; }
    public int getRedirects() { return redirects; }
    public long getFlags() { return flags; }
}
