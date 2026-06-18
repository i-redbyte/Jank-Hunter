package io.jankhunter.runtime.internal.io

import android.os.SystemClock
import java.io.BufferedOutputStream
import java.io.Closeable
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.io.OutputStream
import java.nio.charset.StandardCharsets
import java.util.zip.GZIPOutputStream
import kotlin.math.max

class BinaryLogWriter(
    internal val file: File,
    maxDictionaryEntries: Int = DictionaryIds.DEFAULT_MAX_REGULAR_ENTRIES,
    maxDictionaryValueBytes: Int = DictionaryIds.DEFAULT_MAX_VALUE_BYTES,
    compressionEnabled: Boolean = false,
) : Closeable {
    private val fileOut = FileOutputStream(file, true)
    private val bufferedOut = BufferedOutputStream(fileOut, 32 * 1024)
    private val compressedOut: GZIPOutputStream?
    private val out: OutputStream
    private val dictionary = DictionaryIds(maxDictionaryEntries, maxDictionaryValueBytes)
    private var lastTimestampMs = 0L
    private var logicalBytesWritten = file.length()
    private var closed = false

    init {
        if (file.length() == 0L) {
            bufferedOut.write(MAGIC)
            bufferedOut.flush()
            logicalBytesWritten += MAGIC.size
        }
        compressedOut = if (compressionEnabled) {
            GZIPOutputStream(bufferedOut, IO_BUFFER_BYTES, true)
        } else {
            null
        }
        out = compressedOut ?: bufferedOut
    }

    @Synchronized
    fun bytesWritten(): Long = logicalBytesWritten

    @Synchronized
    fun flush() {
        ensureOpen()
        out.flush()
        bufferedOut.flush()
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
        deviceRooted: Boolean,
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
        var flags = FLAG_APP_FOREGROUND
        if (deviceRooted) flags = flags or FLAG_DEVICE_ROOTED
        record(EVENT_SESSION, flags, payload)
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
        var flags = FLAG_APP_FOREGROUND
        if (lowMemory) flags = flags or FLAG_CONTEXT_LOW_MEMORY
        if (networkMetered) flags = flags or FLAG_NETWORK_METERED
        if (networkValidated) flags = flags or FLAG_NETWORK_VALIDATED
        if (networkVpn) flags = flags or FLAG_NETWORK_VPN
        val payload = Payload()
            .uvarint(networkKind.toLong())
            .uvarint(batteryPct.toLong())
            .uvarint(availMemoryKb)
            .uvarint(batteryState.toLong())
            .svarint(batteryTempDeciC.toLong())
            .uvarint(rxBytes)
            .uvarint(txBytes)
            .uvarint(totalMemoryKb)
            .uvarint(freeStorageKb)
            .uvarint(totalStorageKb)
        record(EVENT_CONTEXT, flags, payload)
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
    fun retained(
        screen: String?,
        owner: String?,
        flow: String?,
        step: String?,
        className: String?,
        holder: String?,
        ageMs: Long,
        count: Long,
    ) {
        val screenId = idFor(DICT_SCREEN, screen)
        val ownerId = idFor(DICT_OWNER, owner)
        val flowId = idFor(DICT_FLOW, flow)
        val stepId = idFor(DICT_STEP, step)
        val classId = idFor(DICT_CLASS, className)
        val holderId = idFor(DICT_OWNER, holder)
        val flags = FLAG_APP_FOREGROUND or contextFlags(screenId, ownerId, flowId, stepId)
        val payload = Payload()
            .optionalContext(flags, screenId, ownerId, flowId, stepId)
            .uvarint(classId)
            .uvarint(holderId)
            .uvarint(ageMs)
            .uvarint(count)
        record(EVENT_RETAINED, flags, payload)
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
        if (value < 0L) {
            counter("jankhunter.metric.invalid_negative.counter.count", 1L)
            return
        }
        metric(EVENT_COUNTER, name, value, count = 1L, sum = value, max = value, mode = MetricAggregationMode.UNKNOWN)
    }

    @Synchronized
    fun gauge(name: String?, value: Long) {
        if (value < 0L) {
            counter("jankhunter.metric.invalid_negative.gauge.count", 1L)
            return
        }
        gauge(name, value, count = 1L, sum = value, max = value, mode = MetricAggregationMode.AVERAGE)
    }

    @Synchronized
    fun gauge(
        name: String?,
        value: Long,
        count: Long,
        sum: Long,
        max: Long,
        mode: MetricAggregationMode,
    ) {
        if (value < 0L || sum < 0L || max < 0L) {
            counter("jankhunter.metric.invalid_negative.gauge.count", 1L)
            return
        }
        val safeCount = if (count > 0L) count else 1L
        metric(EVENT_GAUGE, name, value, safeCount, sum, max, mode)
    }

    @Synchronized
    fun flowContext(screen: String?, owner: String?, flow: String?, step: String?) {
        val screenId = idFor(DICT_SCREEN, screen)
        val ownerId = idFor(DICT_OWNER, owner)
        val flowId = idFor(DICT_FLOW, flow)
        val stepId = idFor(DICT_STEP, step)
        val flags = contextFlags(screenId, ownerId, flowId, stepId)
        val payload = Payload()
            .optionalContext(flags, screenId, ownerId, flowId, stepId)
        record(EVENT_FLOW, flags, payload)
    }

    @Synchronized
    fun logSpam(
        screen: String?,
        owner: String?,
        flow: String?,
        step: String?,
        source: String?,
        level: Int,
        count: Long,
    ) {
        val screenId = idFor(DICT_SCREEN, screen)
        val ownerId = idFor(DICT_OWNER, owner)
        val flowId = idFor(DICT_FLOW, flow)
        val stepId = idFor(DICT_STEP, step)
        val sourceId = idFor(DICT_LOG_SOURCE, source)
        val flags = contextFlags(screenId, ownerId, flowId, stepId)
        val payload = Payload()
            .optionalContext(flags, screenId, ownerId, flowId, stepId)
            .uvarint(sourceId)
            .uvarint(level.toLong())
            .uvarint(count)
        record(EVENT_LOG_SPAM, flags, payload)
    }

    @Synchronized
    fun problemWindow(
        screen: String?,
        owner: String?,
        flow: String?,
        step: String?,
        kind: String?,
        windowMs: Long,
        count: Long,
        maxMs: Long,
    ) {
        val screenId = idFor(DICT_SCREEN, screen)
        val ownerId = idFor(DICT_OWNER, owner)
        val flowId = idFor(DICT_FLOW, flow)
        val stepId = idFor(DICT_STEP, step)
        val kindId = idFor(DICT_METRIC, kind)
        val flags = contextFlags(screenId, ownerId, flowId, stepId)
        val payload = Payload()
            .optionalContext(flags, screenId, ownerId, flowId, stepId)
            .uvarint(kindId)
            .uvarint(windowMs)
            .uvarint(count)
            .uvarint(maxMs)
        record(EVENT_PROBLEM, flags, payload)
    }

    @Synchronized
    fun runtimeCall(
        screen: String?,
        caller: String?,
        flow: String?,
        step: String?,
        callee: String?,
        count: Long,
        totalMs: Long,
        maxMs: Long,
    ) {
        val screenId = idFor(DICT_SCREEN, screen)
        val callerId = idFor(DICT_OWNER, caller)
        val flowId = idFor(DICT_FLOW, flow)
        val stepId = idFor(DICT_STEP, step)
        val calleeId = idFor(DICT_OWNER, callee)
        val flags = contextFlags(screenId, callerId, flowId, stepId)
        val payload = Payload()
            .optionalContext(flags, screenId, callerId, flowId, stepId)
            .uvarint(calleeId)
            .uvarint(count)
            .uvarint(totalMs)
            .uvarint(maxMs)
        record(EVENT_RUNTIME_CALL, flags, payload)
    }

    private fun metric(
        eventType: Int,
        name: String?,
        value: Long,
        count: Long,
        sum: Long,
        max: Long,
        mode: MetricAggregationMode,
    ) {
        val metricId = idFor(DICT_METRIC, name)
        val payload = Payload()
            .uvarint(metricId)
            .uvarint(value)
            .uvarint(count)
            .uvarint(sum)
            .uvarint(max)
            .uvarint(mode.wireValue)
        record(eventType, 0L, payload)
    }

    private fun idFor(kind: Int, rawValue: String?): Long {
        ensureOpen()
        val result = dictionary.idFor(kind, rawValue)
        result.definition?.let { definition ->
            val payload = Payload()
                .uvarint(definition.kind.toLong())
                .uvarint(definition.id)
                .dictionaryValue(definition.value)
            record(EVENT_DICTIONARY, 0L, payload)
        }
        return result.id
    }

    private fun record(eventType: Int, flags: Long, payload: Payload) {
        ensureOpen()
        val now = SystemClock.elapsedRealtime()
        val delta = if (lastTimestampMs == 0L) 0L else max(0L, now - lastTimestampMs)
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

    private fun ensureOpen() {
        if (closed) {
            throw IOException("BinaryLogWriter is closed")
        }
    }

    @Synchronized
    override fun close() {
        if (closed) return
        closed = true
        try {
            compressedOut?.finish()
            out.flush()
        } finally {
            out.close()
        }
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

        fun svarint(value: Long): Payload {
            return uvarint((value shl 1) xor (value shr 63))
        }

        fun optionalContext(flags: Long, screenId: Long, ownerId: Long, flowId: Long, stepId: Long): Payload {
            if (flags and FLAG_HAS_SCREEN != 0L) uvarint(screenId)
            if (flags and FLAG_HAS_OWNER != 0L) uvarint(ownerId)
            if (flags and FLAG_HAS_FLOW != 0L) uvarint(flowId)
            if (flags and FLAG_HAS_STEP != 0L) uvarint(stepId)
            return this
        }

        fun dictionaryValue(value: String): Payload {
            if (value.isEmpty()) {
                uvarint(0)
                uvarint(DICTIONARY_VALUE_UTF8.toLong())
                return string(value)
            }

            val utf8Bytes = value.toByteArray(StandardCharsets.UTF_8)
            val encoded = encodedDictionaryValue(value)
            if (encoded != null) {
                val utf8Size = uvarintSize(utf8Bytes.size.toLong()) + utf8Bytes.size
                val encodedSize = 1 + uvarintSize(encoded.codec.toLong()) + encoded.bytes.size
                if (encodedSize < utf8Size) {
                    uvarint(0)
                    uvarint(encoded.codec.toLong())
                    bytes(encoded.bytes)
                    return this
                }
            }
            return stringBytes(utf8Bytes)
        }

        private fun string(value: String): Payload {
            return stringBytes(value.toByteArray(StandardCharsets.UTF_8))
        }

        private fun stringBytes(bytes: ByteArray): Payload {
            uvarint(bytes.size.toLong())
            bytes(bytes)
            return this
        }

        private fun bytes(bytes: ByteArray): Payload {
            if (bytes.isEmpty()) return this
            ensure(bytes.size)
            System.arraycopy(bytes, 0, data, size, bytes.size)
            size += bytes.size
            return this
        }

        fun writeTo(out: OutputStream) {
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

        private data class EncodedValue(
            val codec: Int,
            val bytes: ByteArray,
        )

        private companion object {
            private fun encodedDictionaryValue(value: String): EncodedValue? {
                if (isIsoDate(value)) {
                    return EncodedValue(
                        DICTIONARY_VALUE_BCD_ISO_DATE,
                        packBcdDigits(value.substring(0, 4) + value.substring(5, 7) + value.substring(8, 10)),
                    )
                }
                if (isDecimalString(value)) {
                    val packed = packBcdDigits(value)
                    val payload = Payload().uvarint(value.length.toLong()).bytes(packed)
                    return EncodedValue(DICTIONARY_VALUE_BCD_DECIMAL, payload.copyBytes())
                }
                return null
            }

            private fun isDecimalString(value: String): Boolean {
                if (value.isEmpty()) return false
                return value.all { it in '0'..'9' }
            }

            private fun isIsoDate(value: String): Boolean {
                if (value.length != 10 || value[4] != '-' || value[7] != '-') return false
                val digitIndexes = intArrayOf(0, 1, 2, 3, 5, 6, 8, 9)
                return digitIndexes.all { value[it] in '0'..'9' }
            }

            private fun packBcdDigits(value: String): ByteArray {
                val out = ByteArray((value.length + 1) / 2)
                for (index in out.indices) {
                    val high = value[index * 2].code - '0'.code
                    val low = if (index * 2 + 1 < value.length) {
                        value[index * 2 + 1].code - '0'.code
                    } else {
                        0x0f
                    }
                    out[index] = ((high shl 4) or low).toByte()
                }
                return out
            }

            private fun uvarintSize(rawValue: Long): Int {
                var value = rawValue
                var count = 1
                while (value and 0x7FL.inv() != 0L) {
                    value = value ushr 7
                    count++
                }
                return count
            }
        }

        private fun copyBytes(): ByteArray {
            return data.copyOf(size)
        }
    }

    companion object {
        const val FLAG_HTTP_REUSED_CONNECTION: Long = 1L
        const val FLAG_HTTP_FAILED: Long = 1L shl 1
        const val FLAG_HTTP_TLS: Long = 1L shl 2
        const val FLAG_THREAD_MAIN: Long = 1L shl 3
        const val FLAG_APP_FOREGROUND: Long = 1L shl 4
        const val FLAG_NETWORK_METERED: Long = 1L shl 5
        const val FLAG_CONTEXT_LOW_MEMORY: Long = 1L shl 6
        const val FLAG_NETWORK_VALIDATED: Long = 1L shl 7
        const val FLAG_NETWORK_VPN: Long = 1L shl 8
        const val FLAG_DEVICE_ROOTED: Long = 1L shl 9
        const val FLAG_HAS_SCREEN: Long = 1L shl 10
        const val FLAG_HAS_OWNER: Long = 1L shl 11
        const val FLAG_HAS_FLOW: Long = 1L shl 12
        const val FLAG_HAS_STEP: Long = 1L shl 13

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
        private const val EVENT_FLOW = 11
        private const val EVENT_LOG_SPAM = 12
        private const val EVENT_PROBLEM = 13
        private const val EVENT_RUNTIME_CALL = 14

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
        private const val DICT_FLOW = 11
        private const val DICT_STEP = 12
        private const val DICT_LOG_SOURCE = 13

        private const val DICTIONARY_VALUE_UTF8 = 0
        private const val DICTIONARY_VALUE_BCD_DECIMAL = 1
        private const val DICTIONARY_VALUE_BCD_ISO_DATE = 2

        private fun writeUvarint(out: OutputStream, rawValue: Long): Int {
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
            out: OutputStream,
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

        private fun writeCompactDelta(out: OutputStream, code: Int, deltaMs: Long): Int {
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
                EVENT_CONTEXT,
                EVENT_COUNTER,
                EVENT_GAUGE,
                EVENT_RETAINED,
                EVENT_FLOW,
                EVENT_LOG_SPAM,
                EVENT_PROBLEM,
                EVENT_RUNTIME_CALL -> true
                else -> false
            }
        }

        private fun contextFlags(screenId: Long, ownerId: Long, flowId: Long, stepId: Long): Long {
            var flags = 0L
            if (screenId != 0L) flags = flags or FLAG_HAS_SCREEN
            if (ownerId != 0L) flags = flags or FLAG_HAS_OWNER
            if (flowId != 0L) flags = flags or FLAG_HAS_FLOW
            if (stepId != 0L) flags = flags or FLAG_HAS_STEP
            return flags
        }

        private const val FORMAT_VERSION = 5
        private const val IO_BUFFER_BYTES = 32 * 1024
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
