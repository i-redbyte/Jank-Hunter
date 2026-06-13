package io.jankhunter.runtime.internal.io

import android.os.SystemClock
import java.io.BufferedOutputStream
import java.io.Closeable
import java.io.File
import java.io.FileOutputStream
import java.nio.charset.StandardCharsets
import kotlin.math.max

class BinaryLogWriter(internal val file: File) : Closeable {
    private val out = BufferedOutputStream(FileOutputStream(file, true), 32 * 1024)
    private val dictionary = LinkedHashMap<String, Long>()
    private var nextDictionaryId = 1L
    private var lastTimestampMs = 0L
    private var logicalBytesWritten = file.length()

    init {
        if (file.length() == 0L) {
            out.write(MAGIC)
            logicalBytesWritten += MAGIC.size
        }
    }

    @Synchronized
    fun bytesWritten(): Long = logicalBytesWritten

    @Synchronized
    fun flush() {
        out.flush()
    }

    @Synchronized
    fun session(
        appVersion: String?,
        build: String?,
        device: String?,
        sdkInt: Int,
        processName: String?,
        androidRelease: String?,
        securityPatch: String?,
        primaryAbi: String?,
        supportedAbis: String?,
        manufacturer: String?,
        brand: String?,
        hardware: String?,
        board: String?,
        product: String?,
    ) {
        val appVersionId = idFor(DICT_APP_VERSION, appVersion)
        val buildId = idFor(DICT_BUILD, build)
        val deviceId = idFor(DICT_DEVICE, device)
        val processId = idFor(DICT_PROCESS, processName)
        val androidReleaseId = idFor(DICT_GENERIC, androidRelease)
        val securityPatchId = idFor(DICT_GENERIC, securityPatch)
        val primaryAbiId = idFor(DICT_GENERIC, primaryAbi)
        val supportedAbisId = idFor(DICT_GENERIC, supportedAbis)
        val manufacturerId = idFor(DICT_GENERIC, manufacturer)
        val brandId = idFor(DICT_GENERIC, brand)
        val hardwareId = idFor(DICT_GENERIC, hardware)
        val boardId = idFor(DICT_GENERIC, board)
        val productId = idFor(DICT_GENERIC, product)
        val payload = Payload()
            .uvarint(appVersionId)
            .uvarint(buildId)
            .uvarint(deviceId)
            .uvarint(sdkInt.toLong())
            .uvarint(processId)
            .uvarint(androidReleaseId)
            .uvarint(securityPatchId)
            .uvarint(primaryAbiId)
            .uvarint(supportedAbisId)
            .uvarint(manufacturerId)
            .uvarint(brandId)
            .uvarint(hardwareId)
            .uvarint(boardId)
            .uvarint(productId)
        record(EVENT_SESSION, FLAG_APP_FOREGROUND, payload)
    }

    @Synchronized
    fun screen(screen: String?) {
        idFor(DICT_SCREEN, screen)
    }

    @Synchronized
    fun context(
        networkKind: Int,
        batteryPct: Int,
        availMemoryKb: Long,
        batteryState: Int,
        batteryTempDeciC: Int,
        lowMemory: Boolean,
        networkMetered: Boolean,
        networkValidated: Boolean,
        rxBytes: Long,
        txBytes: Long,
        totalMemoryKb: Long,
        freeStorageKb: Long,
        totalStorageKb: Long,
        networkVpn: Boolean,
    ) {
        val flags = if (networkMetered) FLAG_NETWORK_METERED else 0L
        val payload = Payload()
            .uvarint(networkKind.toLong())
            .uvarint(batteryPct.toLong())
            .uvarint(availMemoryKb)
            .uvarint(batteryState.toLong())
            .uvarint(batteryTempDeciC.toLong())
            .uvarint(boolean(lowMemory))
            .uvarint(boolean(networkMetered))
            .uvarint(boolean(networkValidated))
            .uvarint(rxBytes)
            .uvarint(txBytes)
            .uvarint(totalMemoryKb)
            .uvarint(freeStorageKb)
            .uvarint(totalStorageKb)
            .uvarint(boolean(networkVpn))
        record(EVENT_CONTEXT, flags or FLAG_APP_FOREGROUND, payload)
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
    fun retained(className: String?, ageMs: Long, count: Long) {
        val classId = idFor(DICT_CLASS, className)
        val payload = Payload().uvarint(classId).uvarint(ageMs).uvarint(count)
        record(EVENT_RETAINED, FLAG_APP_FOREGROUND, payload)
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

        val written = writeCompactHeader(
            out,
            eventType,
            delta,
            flags,
            needsPayloadLength(eventType),
            payload.size,
        )
        payload.writeTo(out)
        logicalBytesWritten += written + payload.size
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
        const val FLAG_NETWORK_METERED: Long = 1L shl 5

        private val MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'L'.code.toByte(), 'O'.code.toByte(), 'G'.code.toByte(), '\r'.code.toByte(), '\n'.code.toByte(), FORMAT_VERSION.toByte())

        private const val EVENT_DICTIONARY = 1
        private const val EVENT_SESSION = 2
        private const val EVENT_CONTEXT = 3
        private const val EVENT_HTTP = 4
        private const val EVENT_UI_WINDOW = 5
        private const val EVENT_STALL = 6
        private const val EVENT_MEMORY = 7
        private const val EVENT_RETAINED = 8
        private const val EVENT_COUNTER = 9
        private const val EVENT_GAUGE = 10

        private const val DICT_GENERIC = 0
        private const val DICT_OWNER = 1
        private const val DICT_ROUTE = 2
        private const val DICT_SCREEN = 3
        private const val DICT_CLASS = 4
        private const val DICT_STACK = 5
        private const val DICT_METRIC = 6
        private const val DICT_DEVICE = 7
        private const val DICT_APP_VERSION = 8
        private const val DICT_BUILD = 9
        private const val DICT_PROCESS = 10

        private fun writeUvarint(out: BufferedOutputStream, rawValue: Long): Int {
            var value = rawValue
            var count = 0
            while (value and 0x7FL.inv() != 0L) {
                out.write(((value and 0x7F) or 0x80).toInt())
                value = value ushr 7
                count++
            }
            out.write(value.toInt())
            return count + 1
        }

        private fun writeCompactHeader(
            out: BufferedOutputStream,
            eventType: Int,
            deltaMs: Long,
            flags: Long,
            payloadLength: Boolean,
            payloadSize: Int,
        ): Int {
            val deltaCode = compactDeltaCode(deltaMs)
            var header = eventType and COMPACT_EVENT_TYPE_MASK
            if (flags != 0L) {
                header = header or COMPACT_HEADER_HAS_FLAGS
            }
            if (payloadLength) {
                header = header or COMPACT_HEADER_HAS_PAYLOAD_LEN
            }
            header = header or (deltaCode shl COMPACT_HEADER_DELTA_SHIFT)
            out.write(header)
            var written = 1
            written += writeCompactDelta(out, deltaCode, deltaMs)
            if (flags != 0L) {
                written += writeUvarint(out, flags)
            }
            if (payloadLength) {
                written += writeUvarint(out, payloadSize.toLong())
            }
            return written
        }

        private fun compactDeltaCode(deltaMs: Long): Int {
            return when {
                deltaMs == 0L -> COMPACT_DELTA_ZERO
                deltaMs <= 0xffL -> COMPACT_DELTA_UINT8
                deltaMs <= 0xffffL -> COMPACT_DELTA_UINT16
                else -> COMPACT_DELTA_UVARINT
            }
        }

        private fun writeCompactDelta(out: BufferedOutputStream, code: Int, deltaMs: Long): Int {
            return when (code) {
                COMPACT_DELTA_ZERO -> 0
                COMPACT_DELTA_UINT8 -> {
                    out.write(deltaMs.toInt() and 0xff)
                    1
                }
                COMPACT_DELTA_UINT16 -> {
                    out.write(deltaMs.toInt() and 0xff)
                    out.write((deltaMs.toInt() ushr 8) and 0xff)
                    2
                }
                else -> writeUvarint(out, deltaMs)
            }
        }

        private fun needsPayloadLength(eventType: Int): Boolean {
            return when (eventType) {
                EVENT_DICTIONARY,
                EVENT_SESSION,
                EVENT_CONTEXT -> true
                else -> false
            }
        }

        private fun boolean(value: Boolean): Long = if (value) 1L else 0L

        private const val FORMAT_VERSION = 2
        private const val COMPACT_EVENT_TYPE_MASK = 0x0f
        private const val COMPACT_HEADER_HAS_FLAGS = 1 shl 4
        private const val COMPACT_HEADER_HAS_PAYLOAD_LEN = 1 shl 5
        private const val COMPACT_HEADER_DELTA_SHIFT = 6
        private const val COMPACT_DELTA_ZERO = 0
        private const val COMPACT_DELTA_UINT8 = 1
        private const val COMPACT_DELTA_UINT16 = 2
        private const val COMPACT_DELTA_UVARINT = 3
    }
}
