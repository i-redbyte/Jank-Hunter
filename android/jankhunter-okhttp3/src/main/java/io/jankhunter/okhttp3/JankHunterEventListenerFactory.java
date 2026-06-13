package io.jankhunter.okhttp3;

import java.io.IOException;
import java.net.InetSocketAddress;
import java.net.Proxy;
import java.util.List;

import io.jankhunter.runtime.JankHunter;
import io.jankhunter.runtime.internal.io.BinaryLogWriter;
import okhttp3.Call;
import okhttp3.EventListener;
import okhttp3.Protocol;
import okhttp3.Request;
import okhttp3.Response;

public final class JankHunterEventListenerFactory implements EventListener.Factory {
    private final EventListener.Factory delegate;

    public JankHunterEventListenerFactory() {
        this(null);
    }

    public JankHunterEventListenerFactory(EventListener.Factory delegate) {
        this.delegate = delegate;
    }

    @Override
    public EventListener create(Call call) {
        EventListener base = delegate == null ? EventListener.NONE : delegate.create(call);
        return new Listener(base);
    }

    private static final class Listener extends EventListener {
        private final EventListener delegate;
        private long startedAt;
        private long dnsStartedAt;
        private long connectStartedAt;
        private long responseStartedAt;
        private long dnsMs;
        private long connectMs;
        private long ttfbMs;
        private int statusClass;
        private long flags;

        Listener(EventListener delegate) {
            this.delegate = delegate;
        }

        @Override
        public void callStart(Call call) {
            startedAt = now();
            delegate.callStart(call);
        }

        @Override
        public void dnsStart(Call call, String domainName) {
            dnsStartedAt = now();
            delegate.dnsStart(call, domainName);
        }

        @Override
        public void dnsEnd(Call call, String domainName, List<java.net.InetAddress> inetAddressList) {
            dnsMs = elapsed(dnsStartedAt);
            delegate.dnsEnd(call, domainName, inetAddressList);
        }

        @Override
        public void connectStart(Call call, InetSocketAddress inetSocketAddress, Proxy proxy) {
            connectStartedAt = now();
            delegate.connectStart(call, inetSocketAddress, proxy);
        }

        @Override
        public void connectEnd(Call call, InetSocketAddress inetSocketAddress, Proxy proxy, Protocol protocol) {
            connectMs = elapsed(connectStartedAt);
            flags |= BinaryLogWriter.FLAG_HTTP_TLS;
            delegate.connectEnd(call, inetSocketAddress, proxy, protocol);
        }

        @Override
        public void responseHeadersStart(Call call) {
            responseStartedAt = now();
            delegate.responseHeadersStart(call);
        }

        @Override
        public void responseHeadersEnd(Call call, Response response) {
            ttfbMs = elapsed(responseStartedAt);
            statusClass = response.code() / 100;
            delegate.responseHeadersEnd(call, response);
        }

        @Override
        public void callEnd(Call call) {
            record(call, false);
            delegate.callEnd(call);
        }

        @Override
        public void callFailed(Call call, IOException ioe) {
            record(call, true);
            delegate.callFailed(call, ioe);
        }

        private void record(Call call, boolean failed) {
            Request request = call.request();
            long localFlags = flags | BinaryLogWriter.FLAG_APP_FOREGROUND;
            if (failed) {
                localFlags |= BinaryLogWriter.FLAG_HTTP_FAILED;
            }
            JankHunter.recordHttp(
                    JankHunter.currentOwner(),
                    request.method() + " " + request.url().encodedPath(),
                    elapsed(startedAt),
                    dnsMs,
                    connectMs,
                    ttfbMs,
                    statusClass,
                    0,
                    0,
                    localFlags
            );
        }

        private static long now() {
            return android.os.SystemClock.elapsedRealtime();
        }

        private static long elapsed(long start) {
            return start <= 0 ? 0 : Math.max(0, now() - start);
        }
    }
}
