package io.jankhunter.runtime.internal.io;

import java.io.BufferedOutputStream;
import java.io.Closeable;
import java.io.File;
import java.io.FileOutputStream;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.HashMap;
import java.util.Map;

public final class BinaryLogWriter implements Closeable {
    public static final long FLAG_HTTP_REUSED_CONNECTION = 1L;
    public static final long FLAG_HTTP_FAILED = 1L << 1;
    public static final long FLAG_HTTP_TLS = 1L << 2;
    public static final long FLAG_THREAD_MAIN = 1L << 3;
    public static final long FLAG_APP_FOREGROUND = 1L << 4;

    private static final byte[] MAGIC = new byte[]{'J', 'H', 'L', 'O', 'G', '\r', '\n', 1};

    private static final int EVENT_DICTIONARY = 1;
    private static final int EVENT_SESSION = 2;
    private static final int EVENT_HTTP = 4;
    private static final int EVENT_UI_WINDOW = 5;
    private static final int EVENT_STALL = 6;
    private static final int EVENT_MEMORY = 7;
    private static final int EVENT_COUNTER = 9;
    private static final int EVENT_GAUGE = 10;

    private static final int DICT_OWNER = 1;
    private static final int DICT_ROUTE = 2;
    private static final int DICT_SCREEN = 3;
    private static final int DICT_STACK = 5;
    private static final int DICT_METRIC = 6;
    private static final int DICT_DEVICE = 7;
    private static final int DICT_APP_VERSION = 8;
    private static final int DICT_BUILD = 9;

    private final BufferedOutputStream out;
    private final Map<String, Long> dictionary = new HashMap<String, Long>();
    private long nextDictionaryId = 1;
    private long lastTimestampMs;

    public BinaryLogWriter(File file) throws IOException {
        this.out = new BufferedOutputStream(new FileOutputStream(file, true), 32 * 1024);
        if (file.length() == 0) {
            out.write(MAGIC);
        }
    }

    public synchronized void session(String appVersion, String build, String device, int sdkInt) throws IOException {
        long appVersionId = idFor(DICT_APP_VERSION, appVersion);
        long buildId = idFor(DICT_BUILD, build);
        long deviceId = idFor(DICT_DEVICE, device);
        Payload payload = new Payload();
        payload.uvarint(appVersionId).uvarint(buildId).uvarint(deviceId).uvarint(sdkInt);
        record(EVENT_SESSION, FLAG_APP_FOREGROUND, payload);
    }

    public synchronized void screen(String screen) throws IOException {
        idFor(DICT_SCREEN, screen);
    }

    public synchronized void http(String owner, String route, long durationMs, long dnsMs, long connectMs, long ttfbMs, int statusClass, long rxBytes, long txBytes, long flags) throws IOException {
        long ownerId = idFor(DICT_OWNER, owner);
        long routeId = idFor(DICT_ROUTE, route);
        Payload payload = new Payload();
        payload.uvarint(ownerId).uvarint(routeId).uvarint(durationMs).uvarint(dnsMs).uvarint(connectMs)
                .uvarint(ttfbMs).uvarint(statusClass).uvarint(rxBytes).uvarint(txBytes);
        record(EVENT_HTTP, flags, payload);
    }

    public synchronized void stall(String owner, String stackHint, long durationMs) throws IOException {
        long ownerId = idFor(DICT_OWNER, owner);
        long stackId = idFor(DICT_STACK, stackHint);
        Payload payload = new Payload();
        payload.uvarint(ownerId).uvarint(stackId).uvarint(durationMs);
        record(EVENT_STALL, FLAG_THREAD_MAIN | FLAG_APP_FOREGROUND, payload);
    }

    public synchronized void memory(long pssKb, long javaHeapKb, long nativeHeapKb) throws IOException {
        Payload payload = new Payload();
        payload.uvarint(pssKb).uvarint(javaHeapKb).uvarint(nativeHeapKb);
        record(EVENT_MEMORY, FLAG_APP_FOREGROUND, payload);
    }

    public synchronized void uiWindow(String screen, long windowMs, long frameCount, long jankCount, long p50Ms, long p95Ms, long p99Ms) throws IOException {
        long screenId = idFor(DICT_SCREEN, screen);
        Payload payload = new Payload();
        payload.uvarint(screenId).uvarint(windowMs).uvarint(frameCount).uvarint(jankCount)
                .uvarint(p50Ms).uvarint(p95Ms).uvarint(p99Ms);
        record(EVENT_UI_WINDOW, FLAG_THREAD_MAIN | FLAG_APP_FOREGROUND, payload);
    }

    public synchronized void counter(String name, long value) throws IOException {
        metric(EVENT_COUNTER, name, value);
    }

    public synchronized void gauge(String name, long value) throws IOException {
        metric(EVENT_GAUGE, name, value);
    }

    private void metric(int eventType, String name, long value) throws IOException {
        long metricId = idFor(DICT_METRIC, name);
        Payload payload = new Payload();
        payload.uvarint(metricId).uvarint(value);
        record(eventType, 0, payload);
    }

    private long idFor(int kind, String value) throws IOException {
        if (value == null || value.length() == 0) {
            value = "unknown";
        }
        String key = kind + ":" + value;
        Long existing = dictionary.get(key);
        if (existing != null) {
            return existing.longValue();
        }
        long id = nextDictionaryId++;
        dictionary.put(key, id);

        Payload payload = new Payload();
        payload.uvarint(kind).uvarint(id).string(value);
        record(EVENT_DICTIONARY, 0, payload);
        return id;
    }

    private void record(int eventType, long flags, Payload payload) throws IOException {
        long now = android.os.SystemClock.elapsedRealtime();
        long delta = lastTimestampMs == 0 ? now : Math.max(0, now - lastTimestampMs);
        lastTimestampMs = now;

        writeUvarint(out, eventType);
        writeUvarint(out, delta);
        writeUvarint(out, flags);
        writeUvarint(out, payload.size());
        payload.writeTo(out);
    }

    @Override
    public synchronized void close() throws IOException {
        out.flush();
        out.close();
    }

    private static void writeUvarint(BufferedOutputStream out, long value) throws IOException {
        while ((value & ~0x7FL) != 0) {
            out.write((int) ((value & 0x7F) | 0x80));
            value >>>= 7;
        }
        out.write((int) value);
    }

    private static final class Payload {
        private byte[] data = new byte[64];
        private int size;

        Payload uvarint(long value) {
            while ((value & ~0x7FL) != 0) {
                byteValue((int) ((value & 0x7F) | 0x80));
                value >>>= 7;
            }
            byteValue((int) value);
            return this;
        }

        Payload string(String value) {
            byte[] bytes = value.getBytes(StandardCharsets.UTF_8);
            uvarint(bytes.length);
            ensure(bytes.length);
            System.arraycopy(bytes, 0, data, size, bytes.length);
            size += bytes.length;
            return this;
        }

        int size() {
            return size;
        }

        void writeTo(BufferedOutputStream out) throws IOException {
            out.write(data, 0, size);
        }

        private void byteValue(int value) {
            ensure(1);
            data[size++] = (byte) value;
        }

        private void ensure(int extra) {
            int required = size + extra;
            if (required <= data.length) {
                return;
            }
            int newSize = data.length * 2;
            while (newSize < required) {
                newSize *= 2;
            }
            byte[] next = new byte[newSize];
            System.arraycopy(data, 0, next, 0, size);
            data = next;
        }
    }
}
