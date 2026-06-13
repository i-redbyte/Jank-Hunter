package io.jankhunter.runtime.internal.io

import android.os.SystemClock
import java.io.BufferedOutputStream
import java.io.Closeable
import java.io.File
import java.io.FileOutputStream
import java.nio.charset.StandardCharsets
import kotlin.math.max

class BinaryLogWriter(file: File) : Closeable {
    private val out = BufferedOutputStream(FileOutputStream(file, true), 32 * 1024)
    private val dictionary = LinkedHashMap<String, Long>()
    private var nextDictionaryId = 1L
    private var lastTimestampMs = 0L

    init {
        if (file.length() == 0L) {
            out.write(MAGIC)
        }
    }

    @Synchronized
    fun session(appVersion: String?, build: String?, device: String?, sdkInt: Int) {
        val appVersionId = idFor(DICT_APP_VERSION, appVersion)
        val buildId = idFor(DICT_BUILD, build)
        val deviceId = idFor(DICT_DEVICE, device)
        val payload = Payload()
            .uvarint(appVersionId)
            .uvarint(buildId)
            .uvarint(deviceId)
            .uvarint(sdkInt.toLong())
        record(EVENT_SESSION, FLAG_APP_FOREGROUND, payload)
    }

    @Synchronized
    fun screen(screen: String?) {
        idFor(DICT_SCREEN, screen)
    }

    @Synchronized
    fun http(
        owner: String?,
        route: String?,
        durationMs: Long,
        dnsMs: Long,
        connectMs: Long,
        ttfbMs: Long,
        statusClass: Int,
        rxBytes: Long,
        txBytes: Long,
        flags: Long,
    ) {
        val ownerId = idFor(DICT_OWNER, owner)
        val routeId = idFor(DICT_ROUTE, route)
        val payload = Payload()
            .uvarint(ownerId)
            .uvarint(routeId)
            .uvarint(durationMs)
            .uvarint(dnsMs)
            .uvarint(connectMs)
            .uvarint(ttfbMs)
            .uvarint(statusClass.toLong())
            .uvarint(rxBytes)
            .uvarint(txBytes)
        record(EVENT_HTTP, flags, payload)
    }

    @Synchronized
    fun stall(owner: String?, stackHint: String?, durationMs: Long) {
        val ownerId = idFor(DICT_OWNER, owner)
        val stackId = idFor(DICT_STACK, stackHint)
        val payload = Payload().uvarint(ownerId).uvarint(stackId).uvarint(durationMs)
        record(EVENT_STALL, FLAG_THREAD_MAIN or FLAG_APP_FOREGROUND, payload)
    }

    @Synchronized
    fun memory(pssKb: Long, javaHeapKb: Long, nativeHeapKb: Long) {
        val payload = Payload().uvarint(pssKb).uvarint(javaHeapKb).uvarint(nativeHeapKb)
        record(EVENT_MEMORY, FLAG_APP_FOREGROUND, payload)
    }

    @Synchronized
    fun uiWindow(
        screen: String?,
        windowMs: Long,
        frameCount: Long,
        jankCount: Long,
        p50Ms: Long,
        p95Ms: Long,
        p99Ms: Long,
    ) {
        val screenId = idFor(DICT_SCREEN, screen)
        val payload = Payload()
            .uvarint(screenId)
            .uvarint(windowMs)
            .uvarint(frameCount)
            .uvarint(jankCount)
            .uvarint(p50Ms)
            .uvarint(p95Ms)
            .uvarint(p99Ms)
        record(EVENT_UI_WINDOW, FLAG_THREAD_MAIN or FLAG_APP_FOREGROUND, payload)
    }

    @Synchronized
    fun counter(name: String?, value: Long) {
        metric(EVENT_COUNTER, name, value)
    }

    @Synchronized
    fun gauge(name: String?, value: Long) {
        metric(EVENT_GAUGE, name, value)
    }

    private fun metric(eventType: Int, name: String?, value: Long) {
        val metricId = idFor(DICT_METRIC, name)
        val payload = Payload().uvarint(metricId).uvarint(value)
        record(eventType, 0L, payload)
    }

    private fun idFor(kind: Int, rawValue: String?): Long {
        val value = rawValue?.takeIf { it.isNotEmpty() } ?: "unknown"
        val key = "$kind:$value"
        dictionary[key]?.let { return it }

        val id = nextDictionaryId++
        dictionary[key] = id

        val payload = Payload().uvarint(kind.toLong()).uvarint(id).string(value)
        record(EVENT_DICTIONARY, 0L, payload)
        return id
    }

    private fun record(eventType: Int, flags: Long, payload: Payload) {
        val now = SystemClock.elapsedRealtime()
        val delta = if (lastTimestampMs == 0L) now else max(0L, now - lastTimestampMs)
        lastTimestampMs = now

        writeUvarint(out, eventType.toLong())
        writeUvarint(out, delta)
        writeUvarint(out, flags)
        writeUvarint(out, payload.size.toLong())
        payload.writeTo(out)
    }

    @Synchronized
    override fun close() {
        out.flush()
        out.close()
    }

    private class Payload {
        private var data = ByteArray(64)
        var size = 0
            private set

        fun uvarint(rawValue: Long): Payload {
            var value = rawValue
            while (value and 0x7FL.inv() != 0L) {
                byteValue(((value and 0x7F) or 0x80).toInt())
                value = value ushr 7
            }
            byteValue(value.toInt())
            return this
        }

        fun string(value: String): Payload {
            val bytes = value.toByteArray(StandardCharsets.UTF_8)
            uvarint(bytes.size.toLong())
            ensure(bytes.size)
            System.arraycopy(bytes, 0, data, size, bytes.size)
            size += bytes.size
            return this
        }

        fun writeTo(out: BufferedOutputStream) {
            out.write(data, 0, size)
        }

        private fun byteValue(value: Int) {
            ensure(1)
            data[size++] = value.toByte()
        }

        private fun ensure(extra: Int) {
            val required = size + extra
            if (required <= data.size) return

            var newSize = data.size * 2
            while (newSize < required) {
                newSize *= 2
            }
            data = data.copyOf(newSize)
        }
    }

    companion object {
        const val FLAG_HTTP_REUSED_CONNECTION: Long = 1L
        const val FLAG_HTTP_FAILED: Long = 1L shl 1
        const val FLAG_HTTP_TLS: Long = 1L shl 2
        const val FLAG_THREAD_MAIN: Long = 1L shl 3
        const val FLAG_APP_FOREGROUND: Long = 1L shl 4

        private val MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'L'.code.toByte(), 'O'.code.toByte(), 'G'.code.toByte(), '\r'.code.toByte(), '\n'.code.toByte(), 1)

        private const val EVENT_DICTIONARY = 1
        private const val EVENT_SESSION = 2
        private const val EVENT_HTTP = 4
        private const val EVENT_UI_WINDOW = 5
        private const val EVENT_STALL = 6
        private const val EVENT_MEMORY = 7
        private const val EVENT_COUNTER = 9
        private const val EVENT_GAUGE = 10

        private const val DICT_OWNER = 1
        private const val DICT_ROUTE = 2
        private const val DICT_SCREEN = 3
        private const val DICT_STACK = 5
        private const val DICT_METRIC = 6
        private const val DICT_DEVICE = 7
        private const val DICT_APP_VERSION = 8
        private const val DICT_BUILD = 9

        private fun writeUvarint(out: BufferedOutputStream, rawValue: Long) {
            var value = rawValue
            while (value and 0x7FL.inv() != 0L) {
                out.write(((value and 0x7F) or 0x80).toInt())
                value = value ushr 7
            }
            out.write(value.toInt())
        }
    }
}
