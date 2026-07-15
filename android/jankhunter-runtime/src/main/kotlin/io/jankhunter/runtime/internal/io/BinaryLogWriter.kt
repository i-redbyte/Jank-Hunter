package io.jankhunter.runtime.internal.io

import android.os.Process
import android.os.SystemClock
import io.jankhunter.runtime.JankHunterBinaryWriter
import java.io.BufferedOutputStream
import java.io.ByteArrayOutputStream
import java.io.Closeable
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.io.OutputStream
import java.nio.charset.StandardCharsets
import java.util.zip.CRC32
import java.util.zip.GZIPOutputStream

internal class LogSizeLimitReachedException(message: String) : IOException(message)

internal class BinaryLogWriter private constructor(
    internal val file: File?,
    internal val path: String,
    output: OutputStream,
    initialBytesWritten: Long,
    maxDictionaryEntries: Int,
    maxDictionaryValueBytes: Int,
    private val fileHeader: BinaryLogFileHeader,
    private val quality: LogQualityCounters,
    maxPhysicalBytes: Long,
) : Closeable {
    constructor(
        file: File,
        maxDictionaryEntries: Int = DictionaryIds.DEFAULT_MAX_REGULAR_ENTRIES,
        maxDictionaryValueBytes: Int = DictionaryIds.DEFAULT_MAX_VALUE_BYTES,
        maxPhysicalBytes: Long = 0L,
    ) : this(
        file = file,
        path = file.absolutePath,
        output = FileOutputStream(file, false),
        initialBytesWritten = 0L,
        maxDictionaryEntries = maxDictionaryEntries,
        maxDictionaryValueBytes = maxDictionaryValueBytes,
        fileHeader = defaultFileHeader(),
        quality = LogQualityCounters(),
        maxPhysicalBytes = maxPhysicalBytes,
    )

    internal constructor(
        file: File,
        maxDictionaryEntries: Int,
        maxDictionaryValueBytes: Int,
        fileHeader: BinaryLogFileHeader,
        quality: LogQualityCounters,
        maxPhysicalBytes: Long = 0L,
    ) : this(
        file = file,
        path = file.absolutePath,
        output = FileOutputStream(file, false),
        initialBytesWritten = 0L,
        maxDictionaryEntries = maxDictionaryEntries,
        maxDictionaryValueBytes = maxDictionaryValueBytes,
        fileHeader = fileHeader,
        quality = quality,
        maxPhysicalBytes = maxPhysicalBytes,
    )

    internal constructor(
        writer: JankHunterBinaryWriter,
        maxDictionaryEntries: Int = DictionaryIds.DEFAULT_MAX_REGULAR_ENTRIES,
        maxDictionaryValueBytes: Int = DictionaryIds.DEFAULT_MAX_VALUE_BYTES,
        maxPhysicalBytes: Long = 0L,
    ) : this(
        file = null,
        path = writer.path,
        output = ExternalBinaryOutputStream(writer),
        initialBytesWritten = writer.bytesWritten(),
        maxDictionaryEntries = maxDictionaryEntries,
        maxDictionaryValueBytes = maxDictionaryValueBytes,
        fileHeader = defaultFileHeader(),
        quality = LogQualityCounters(),
        maxPhysicalBytes = maxPhysicalBytes,
    )

    internal constructor(
        writer: JankHunterBinaryWriter,
        maxDictionaryEntries: Int,
        maxDictionaryValueBytes: Int,
        fileHeader: BinaryLogFileHeader,
        quality: LogQualityCounters,
        maxPhysicalBytes: Long = 0L,
    ) : this(
        file = null,
        path = writer.path,
        output = ExternalBinaryOutputStream(writer),
        initialBytesWritten = writer.bytesWritten(),
        maxDictionaryEntries = maxDictionaryEntries,
        maxDictionaryValueBytes = maxDictionaryValueBytes,
        fileHeader = fileHeader,
        quality = quality,
        maxPhysicalBytes = maxPhysicalBytes,
    )

    private val bufferedOut = BufferedOutputStream(output, IO_BUFFER_BYTES)
    private val physicalByteLimit = maxPhysicalBytes.takeIf { it > 0L } ?: Long.MAX_VALUE
    private val dictionary = DictionaryIds(
        maxDictionaryEntries,
        minOf(maxDictionaryValueBytes, MAX_ENCODED_DICTIONARY_VALUE_BYTES),
    )
    private val stableSymbolDefinitions = HashMap<Long, String>()
    private val rawChunk = ByteArrayOutputStream(JhlogV9.TARGET_RAW_CHUNK_BYTES)
    private val chunkTypeCounts = IntArray(JhlogV9.TYPE_SEGMENT_END + 1)
    private var chunkRecordCount = 0
    private var chunkSequence = 0L
    private var lastTimedRecordUs = fileHeader.segmentStartElapsedUs
    private var lastContext: ContextIds? = null
    private val producerOverride = ProducerMetadataBuffer()
    private val directProducer = ProducerMetadataBuffer()
    private var producerOverrideActive = false
    private var logicalBytesWritten = 0L
    private var segmentEventRecords = 0L
    private var segmentDictionaryRecords = 0L
    private var lastQualityGeneration = -1L
    private var lastQualitySequence = 0L
    private var commitQualityPending = false
    private var terminalChunkBuilding = false
    private var closed = false
    private var poisoned = false

    init {
        try {
            if (initialBytesWritten != 0L) {
                throw IOException("JHLOG v9 segments must be opened empty: $path")
            }
            writeFileHeader()
        } catch (error: Throwable) {
            runCatching { bufferedOut.close() }
            throw error
        }
    }

    @Synchronized
    fun bytesWritten(): Long = logicalBytesWritten

    @Synchronized
    fun flush() {
        ensureWritable()
        // Commit data first, then publish an exact snapshot in its own committed control chunk.
        commitChunk(final = false)
        commitQualityControlChunk()
        bufferedOut.flush()
    }

    @Synchronized
    internal fun withProducer(
        elapsedUs: Long,
        threadId: Long,
        context: LogEventContext?,
        block: BinaryLogWriter.() -> Unit,
    ) {
        val hadPrevious = producerOverrideActive
        val previousElapsedUs = producerOverride.elapsedUs
        val previousThreadId = producerOverride.threadId
        val previousContext = producerOverride.context
        producerOverride.set(elapsedUs, threadId, context)
        producerOverrideActive = true
        try {
            block()
        } finally {
            producerOverride.set(previousElapsedUs, previousThreadId, previousContext)
            producerOverrideActive = hadPrevious
        }
    }

    @Synchronized
    @Suppress("UNUSED_PARAMETER")
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
        appForeground: Boolean = true,
    ) {
        val payload = Payload()
            .symbolRef(optionalIdFor(DICT_APP_VERSION, appVersion))
            .symbolRef(optionalIdFor(DICT_BUILD, build))
            .symbolRef(optionalIdFor(DICT_DEVICE, device))
            .uvarint(nonNegative(sdkInt.toLong()))
            .symbolRef(optionalIdFor(DICT_GENERIC, androidRelease))
            .symbolRef(optionalIdFor(DICT_GENERIC, securityPatch))
            .symbolRef(optionalIdFor(DICT_GENERIC, primaryAbi))
            .symbolRef(optionalIdFor(DICT_GENERIC, supportedAbis))
            .symbolRef(optionalIdFor(DICT_GENERIC, manufacturer))
            .symbolRef(optionalIdFor(DICT_GENERIC, brand))
            .symbolRef(optionalIdFor(DICT_GENERIC, hardware))
            .symbolRef(optionalIdFor(DICT_GENERIC, board))
            .symbolRef(optionalIdFor(DICT_GENERIC, product))
        var attributes = foregroundFlag(appForeground)
        if (deviceRooted) attributes = attributes or FLAG_DEVICE_ROOTED
        record(JhlogV9.TYPE_SESSION, attributes, payload, currentProducerContext())
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
        foreground: Boolean = true,
    ) {
        var attributes = foregroundFlag(foreground)
        if (lowMemory) attributes = attributes or FLAG_CONTEXT_LOW_MEMORY
        if (networkMetered) attributes = attributes or FLAG_NETWORK_METERED
        if (networkValidated) attributes = attributes or FLAG_NETWORK_VALIDATED
        if (networkVpn) attributes = attributes or FLAG_NETWORK_VPN
        val payload = Payload()
            .uvarint(networkKind.coerceIn(0, 5).toLong())
            .uvarint(batteryPct.coerceIn(0, 100).toLong())
            .uvarint(nonNegative(availMemoryKb))
            .uvarint(nonNegative(batteryState.toLong()))
            .svarint(batteryTempDeciC.toLong())
            .uvarint(nonNegative(rxBytes))
            .uvarint(nonNegative(txBytes))
            .uvarint(nonNegative(totalMemoryKb))
            .uvarint(nonNegative(freeStorageKb))
            .uvarint(nonNegative(totalStorageKb))
        record(JhlogV9.TYPE_DEVICE_CONTEXT, attributes, payload, currentProducerContext())
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
        val safeDurationMs = nonNegative(durationMs)
        val payload = Payload()
            .symbolRef(idFor(DICT_ROUTE, route))
            .uvarint(safeDurationMs)
            .uvarint(clampDuration(dnsMs, safeDurationMs))
            .uvarint(clampDuration(connectMs, safeDurationMs))
            .uvarint(clampDuration(ttfbMs, safeDurationMs))
            .uvarint(statusClass.coerceIn(0, 5).toLong())
            .uvarint(nonNegative(rxBytes))
            .uvarint(nonNegative(txBytes))
        val context = currentProducerContext().withOwner(owner)
        record(JhlogV9.TYPE_HTTP, flags and FLAG_KNOWN_MASK, payload, context)
    }

    @Synchronized
    fun stall(
        screen: String?,
        owner: String?,
        flow: String?,
        step: String?,
        stackHint: String?,
        durationMs: Long,
        foreground: Boolean = true,
    ) {
        val payload = Payload()
            .symbolRef(idFor(DICT_STACK, stackHint))
            .uvarint(nonNegative(durationMs))
        val context = contextIds(screen, owner, flow, step)
        record(JhlogV9.TYPE_STALL, FLAG_THREAD_MAIN or foregroundFlag(foreground), payload, context)
    }

    @Synchronized
    fun memory(pssKb: Long, javaHeapKb: Long, nativeHeapKb: Long, foreground: Boolean = true) {
        val payload = Payload()
            .uvarint(nonNegative(pssKb))
            .uvarint(nonNegative(javaHeapKb))
            .uvarint(nonNegative(nativeHeapKb))
        record(JhlogV9.TYPE_MEMORY, foregroundFlag(foreground), payload, currentProducerContext())
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
        foreground: Boolean = true,
        evidence: Long,
    ) {
        val payload = Payload()
            .symbolRef(idFor(DICT_CLASS, className))
            .symbolRef(idFor(DICT_OWNER, holder))
            .uvarint(nonNegative(ageMs))
            .uvarint(nonNegative(count))
            .uvarint(evidence.coerceIn(RETAINED_EVIDENCE_TIME_ONLY, RETAINED_EVIDENCE_AFTER_EXPLICIT_GC))
        record(
            JhlogV9.TYPE_RETAINED,
            foregroundFlag(foreground),
            payload,
            contextIds(screen, owner, flow, step),
        )
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
        foreground: Boolean = true,
        flags: Long = 0L,
    ) {
        val safeWindowMs = nonNegative(windowMs).coerceAtLeast(1L)
        val safeFrameCount = nonNegative(frameCount)
        val safeJankCount = nonNegative(jankCount).coerceAtMost(safeFrameCount)
        val safeP50Ms = nonNegative(p50Ms)
        val safeP95Ms = nonNegative(p95Ms).coerceAtLeast(safeP50Ms)
        val safeP99Ms = nonNegative(p99Ms).coerceAtLeast(safeP95Ms)
        val payload = Payload()
            .uvarint(safeWindowMs)
            .uvarint(safeFrameCount)
            .uvarint(safeJankCount)
            .uvarint(safeP50Ms)
            .uvarint(safeP95Ms)
            .uvarint(safeP99Ms)
        val context = currentProducerContext().withScreen(screen)
        val uiFlags = flags and (FLAG_UI_PROBLEM or FLAG_UI_CLASSIFIED)
        val attributes = FLAG_THREAD_MAIN or foregroundFlag(foreground) or uiFlags
        record(JhlogV9.TYPE_UI_WINDOW, attributes, payload, context)
    }

    @Synchronized
    fun counter(name: String?, value: Long) {
        if (value < 0L) {
            quality.add(QualityCounterId.INVALID_METRIC)
            return
        }
        metric(JhlogV9.TYPE_COUNTER, name, value, 1L, value, value, MetricAggregationMode.UNKNOWN)
    }

    @Synchronized
    fun stableCounter(metricId: Long, value: Long) {
        stableCounter(metricId, null, value)
    }

    @Synchronized
    fun stableCounter(metricId: Long, metricName: String?, value: Long) {
        if (value < 0L) {
            quality.add(QualityCounterId.INVALID_METRIC)
            return
        }
        ensureStableSymbolDefinition(metricId, metricName)
        val payload = Payload()
            .stableSymbolRef(metricId)
            .uvarint(value)
            .uvarint(1L)
            .uvarint(value)
            .uvarint(value)
            .uvarint(MetricAggregationMode.UNKNOWN.wireValue)
        // Method counters are global aggregates. A string owner context would recreate the
        // dictionary pressure that the stable metric reference removes.
        record(JhlogV9.TYPE_COUNTER, 0L, payload, context = null)
    }

    @Synchronized
    fun gauge(name: String?, value: Long) {
        gauge(name, value, 1L, value, value, MetricAggregationMode.AVERAGE)
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
            quality.add(QualityCounterId.INVALID_METRIC)
            return
        }
        metric(JhlogV9.TYPE_GAUGE, name, value, count.coerceAtLeast(1L), sum, max, mode)
    }

    @Synchronized
    fun flowContext(screen: String?, owner: String?, flow: String?, step: String?) {
        val payload = Payload()
            .uvarint(JhlogV9.FLOW_PHASE_SNAPSHOT)
            .uvarint(0L)
        record(
            JhlogV9.TYPE_FLOW_TRANSITION,
            0L,
            payload,
            contextIds(screen, owner, flow, step),
        )
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
        val payload = Payload()
            .symbolRef(idFor(DICT_LOG_SOURCE, source))
            .uvarint(nonNegative(level.toLong()))
            .uvarint(nonNegative(count))
        record(JhlogV9.TYPE_LOG_SPAM, 0L, payload, contextIds(screen, owner, flow, step))
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
        foreground: Boolean = true,
    ) {
        val payload = Payload()
            .symbolRef(idFor(DICT_METRIC, kind))
            .uvarint(nonNegative(windowMs).coerceAtLeast(1L))
            .uvarint(nonNegative(count))
            .uvarint(nonNegative(maxMs))
        record(
            JhlogV9.TYPE_PROBLEM,
            foregroundFlag(foreground),
            payload,
            contextIds(screen, owner, flow, step),
        )
    }

    @Synchronized
    fun runtimeCall(
        screen: String?,
        callerId: Long,
        flow: String?,
        step: String?,
        calleeId: Long,
        count: Long,
        totalMs: Long,
        maxMs: Long,
    ) {
        runtimeCall(screen, callerId, null, flow, step, calleeId, null, count, totalMs, maxMs)
    }

    @Synchronized
    fun runtimeCall(
        screen: String?,
        callerId: Long,
        callerName: String?,
        flow: String?,
        step: String?,
        calleeId: Long,
        calleeName: String?,
        count: Long,
        totalMs: Long,
        maxMs: Long,
    ) {
        ensureStableSymbolDefinition(callerId, callerName)
        ensureStableSymbolDefinition(calleeId, calleeName)
        val payload = Payload()
            .stableSymbolRef(calleeId)
            .uvarint(nonNegative(count))
            .uvarint(nonNegative(totalMs))
            .uvarint(nonNegative(maxMs))
        val context = contextIds(screen, null, flow, step).withStableOwner(callerId)
        record(JhlogV9.TYPE_RUNTIME_CALL, 0L, payload, context)
    }

    private fun metric(
        recordType: Int,
        name: String?,
        value: Long,
        count: Long,
        sum: Long,
        max: Long,
        mode: MetricAggregationMode,
    ) {
        val normalizedCount = count.coerceAtLeast(1L)
        val normalizedSum = if (sum == 0L) value else sum
        val normalizedMax = if (max == 0L) value else max
        val payload = Payload()
            .symbolRef(idFor(DICT_METRIC, name))
            .uvarint(value)
            .uvarint(normalizedCount)
            .uvarint(normalizedSum)
            .uvarint(normalizedMax)
            .uvarint(mode.wireValue)
        record(recordType, 0L, payload, currentProducerContext())
    }

    private fun idFor(kind: Int, rawValue: String?): Long {
        ensureWritable()
        val result = dictionary.idFor(kind, rawValue)
        if (result.overflowed) quality.add(QualityCounterId.DICTIONARY_OVERFLOW_TOTAL)
        if (result.truncated) quality.add(QualityCounterId.DICTIONARY_VALUE_TRUNCATED_TOTAL)
        result.definition?.let { definition ->
            val bytes = definition.value.toByteArray(StandardCharsets.UTF_8)
            val payload = Payload()
                .uvarint(definition.kind.toLong())
                .uvarint(definition.id)
                .uvarint(DICTIONARY_ENCODING_UTF8)
                .uvarint(bytes.size.toLong())
                .bytes(bytes)
            record(
                recordType = JhlogV9.TYPE_DICTIONARY,
                attributes = 0L,
                payload = payload,
                context = null,
                producer = null,
            )
        }
        return result.id
    }

    /**
     * Stable method definitions are intentionally outside [DictionaryIds]: their IDs are already
     * assigned by ASM and must not consume the bounded regular dictionary used by runtime data.
     */
    private fun ensureStableSymbolDefinition(stableId: Long, rawName: String?) {
        val name = rawName?.takeIf(String::isNotBlank) ?: return
        val existing = stableSymbolDefinitions[stableId]
        if (existing != null) return

        val encoded = name.toByteArray(StandardCharsets.UTF_8)
        val bytes = if (encoded.size <= MAX_ENCODED_DICTIONARY_VALUE_BYTES) {
            encoded
        } else {
            quality.add(QualityCounterId.DICTIONARY_VALUE_TRUNCATED_TOTAL)
            validUtf8Prefix(name, MAX_ENCODED_DICTIONARY_VALUE_BYTES)
        }
        stableSymbolDefinitions[stableId] = name
        val payload = Payload()
            .uvarint(DICT_STABLE_SYMBOL.toLong())
            .uvarint(stableId)
            .uvarint(DICTIONARY_ENCODING_UTF8)
            .uvarint(bytes.size.toLong())
            .bytes(bytes)
        record(
            recordType = JhlogV9.TYPE_DICTIONARY,
            attributes = 0L,
            payload = payload,
            context = null,
            producer = null,
        )
    }

    private fun optionalIdFor(kind: Int, rawValue: String?): Long {
        val value = rawValue?.takeIf { it.isNotEmpty() } ?: return 0L
        return idFor(kind, value)
    }

    private fun contextIds(screen: String?, owner: String?, flow: String?, step: String?): ContextIds {
        return ContextIds(
            screenId = optionalIdFor(DICT_SCREEN, screen),
            ownerId = optionalIdFor(DICT_OWNER, owner),
            flowId = optionalIdFor(DICT_FLOW, flow),
            stepId = optionalIdFor(DICT_STEP, step),
        )
    }

    private fun currentProducerContext(): ContextIds? {
        if (!producerOverrideActive) return null
        val context = producerOverride.context ?: return null
        return contextIds(context.screen, context.owner, context.flow, context.step)
    }

    private fun ContextIds?.withOwner(owner: String?): ContextIds? {
        val ownerId = optionalIdFor(DICT_OWNER, owner)
        if (ownerId == 0L) return this
        return ContextIds(
            screenId = this?.screenId ?: 0L,
            ownerId = ownerId,
            flowId = this?.flowId ?: 0L,
            stepId = this?.stepId ?: 0L,
            stableOwnerId = 0L,
        )
    }

    private fun ContextIds?.withStableOwner(ownerId: Long): ContextIds {
        return ContextIds(
            screenId = this?.screenId ?: 0L,
            ownerId = 0L,
            flowId = this?.flowId ?: 0L,
            stepId = this?.stepId ?: 0L,
            stableOwnerId = ownerId,
            hasStableOwner = true,
        )
    }

    private fun ContextIds?.withScreen(screen: String?): ContextIds? {
        val screenId = optionalIdFor(DICT_SCREEN, screen)
        if (screenId == 0L) return this
        return ContextIds(
            screenId = screenId,
            ownerId = this?.ownerId ?: 0L,
            flowId = this?.flowId ?: 0L,
            stepId = this?.stepId ?: 0L,
            stableOwnerId = this?.stableOwnerId ?: 0L,
            hasStableOwner = this?.hasStableOwner ?: false,
        )
    }

    private fun record(
        recordType: Int,
        attributes: Long,
        payload: Payload,
        context: ContextIds?,
        producer: ProducerMetadataBuffer? = currentProducer(),
    ) {
        ensureWritable()
        var encoded = encodeRecord(recordType, attributes, payload, context, producer)
        val chunkTarget = if (terminalChunkBuilding) {
            JhlogV9.MAX_RAW_CHUNK_BYTES
        } else {
            JhlogV9.TARGET_RAW_CHUNK_BYTES
        }
        if (rawChunk.size() > 0 && rawChunk.size() + encoded.size > chunkTarget) {
            commitChunk(final = false)
            encoded = encodeRecord(recordType, attributes, payload, context, producer)
        }
        if (encoded.size > JhlogV9.MAX_RAW_CHUNK_BYTES) {
            if (recordType == JhlogV9.TYPE_DICTIONARY) {
                throw IOException("JHLOG v9 dictionary definition exceeds raw chunk limit")
            }
            quality.addRejected(recordType, QualityCounterId.REASON_OVERSIZED)
            return
        }
        rawChunk.write(encoded)
        chunkRecordCount++
        if (recordType in chunkTypeCounts.indices) chunkTypeCounts[recordType]++
        if (producer != null) lastTimedRecordUs = producer.elapsedUs
        if (context != null) lastContext = context
    }

    private fun currentProducer(): ProducerMetadataBuffer {
        return if (producerOverrideActive) producerOverride else directProducer.capture(null)
    }

    private fun encodeRecord(
        recordType: Int,
        attributes: Long,
        payload: Payload,
        context: ContextIds?,
        producer: ProducerMetadataBuffer?,
    ): ByteArray {
        var envelopeFlags = 0L
        if (producer != null) envelopeFlags = envelopeFlags or JhlogV9.ENVELOPE_HAS_TIME or JhlogV9.ENVELOPE_HAS_THREAD
        if (context != null) envelopeFlags = envelopeFlags or JhlogV9.ENVELOPE_HAS_CONTEXT
        val sameContext = context != null && lastContext == context
        if (sameContext) envelopeFlags = envelopeFlags or JhlogV9.ENVELOPE_SAME_CONTEXT
        val safeAttributes = attributes and FLAG_KNOWN_MASK
        if (safeAttributes != 0L) envelopeFlags = envelopeFlags or JhlogV9.ENVELOPE_HAS_ATTRIBUTES

        val body = Payload()
            .uvarint(recordType.toLong())
            .uvarint(envelopeFlags)
        if (producer != null) {
            body.svarint(producer.elapsedUs - lastTimedRecordUs)
            body.uvarint(producer.threadId)
        }
        if (context != null && !sameContext) {
            var presence = 0L
            if (context.screenId != 0L) presence = presence or JhlogV9.CONTEXT_SCREEN
            if (context.ownerId != 0L || context.hasStableOwner) {
                presence = presence or JhlogV9.CONTEXT_OWNER
            }
            if (context.flowId != 0L) presence = presence or JhlogV9.CONTEXT_FLOW
            if (context.stepId != 0L) presence = presence or JhlogV9.CONTEXT_STEP
            body.uvarint(presence)
            if (presence and JhlogV9.CONTEXT_SCREEN != 0L) body.symbolRef(context.screenId)
            if (presence and JhlogV9.CONTEXT_OWNER != 0L) {
                if (context.hasStableOwner) {
                    body.stableSymbolRef(context.stableOwnerId)
                } else {
                    body.symbolRef(context.ownerId)
                }
            }
            if (presence and JhlogV9.CONTEXT_FLOW != 0L) body.symbolRef(context.flowId)
            if (presence and JhlogV9.CONTEXT_STEP != 0L) body.symbolRef(context.stepId)
        }
        if (safeAttributes != 0L) body.uvarint(safeAttributes)
        body.bytes(payload.copyBytes())
        return Payload().uvarint(body.size.toLong()).bytes(body.copyBytes()).copyBytes()
    }

    private fun writeQualitySnapshot() {
        val generation = quality.generation()
        val sequence = quality.nextSnapshotSequence()
        val entries = quality.snapshot()
        val payload = Payload()
            .uvarint(sequence)
            .uvarint(nowElapsedUs())
            .uvarint(entries.size.toLong())
        entries.forEach { entry ->
            payload.uvarint(entry.counterId.toLong()).uvarint(entry.value)
        }
        record(
            recordType = JhlogV9.TYPE_QUALITY_SNAPSHOT,
            attributes = 0L,
            payload = payload,
            context = null,
            producer = null,
        )
        lastQualityGeneration = generation
        lastQualitySequence = sequence
        commitQualityPending = false
    }

    private fun commitQualityControlChunk() {
        val generation = quality.generation()
        if (generation == lastQualityGeneration && !commitQualityPending) return

        val previousGeneration = lastQualityGeneration
        val previousSequence = lastQualitySequence
        var committed = false
        quality.addHousekeeping(QualityCounterId.COMMITTED_CHUNK_TOTAL)
        try {
            writeQualitySnapshot()
            commitChunk(final = false, countCommit = false)
            committed = true
        } finally {
            if (!committed) {
                quality.subtractHousekeeping(QualityCounterId.COMMITTED_CHUNK_TOTAL)
                lastQualityGeneration = previousGeneration
                lastQualitySequence = previousSequence
                commitQualityPending = true
            }
        }
    }

    private fun writeSegmentEnd(reason: Long) {
        val payload = Payload()
            .uvarint(reason)
            .uvarint(segmentEventRecords)
            .uvarint(segmentDictionaryRecords)
            .uvarint(lastQualitySequence)
        record(
            recordType = JhlogV9.TYPE_SEGMENT_END,
            attributes = 0L,
            payload = payload,
            context = null,
            producer = null,
        )
    }

    private fun commitChunk(final: Boolean, countCommit: Boolean = true) {
        if (rawChunk.size() == 0) return
        val raw = rawChunk.toByteArray()
        val stored = gzip(raw)
        val rawCrc = crc32(raw)
        val flags = JhlogV9.CHUNK_FLAG_GZIP or if (final) JhlogV9.CHUNK_FLAG_FINAL else 0
        val header = chunkHeader(flags, stored.size, raw.size, chunkRecordCount, rawCrc)
        val trailer = commitTrailer(stored.size, raw.size, rawCrc)
        val physicalBytes = header.size.toLong() + stored.size.toLong() + trailer.size.toLong()
        ensurePhysicalCapacity(
            bytes = physicalBytes,
            reservedBytes = if (final) 0L else TERMINAL_RESERVE_BYTES,
        )
        try {
            bufferedOut.write(header)
            bufferedOut.write(stored)
            bufferedOut.write(trailer)
            bufferedOut.flush()
        } catch (error: IOException) {
            poisoned = true
            quality.add(QualityCounterId.FAILED_CHUNK_TOTAL)
            for (recordType in EVENT_RECORD_TYPES) {
                val count = chunkTypeCounts[recordType]
                if (count > 0) quality.addRejected(recordType, QualityCounterId.REASON_IO_LOST, count.toLong())
            }
            resetChunkState()
            throw error
        }
        logicalBytesWritten += physicalBytes
        var eventCount = 0L
        for (recordType in EVENT_RECORD_TYPES) {
            eventCount += chunkTypeCounts[recordType].toLong()
        }
        if (countCommit) quality.addHousekeeping(QualityCounterId.COMMITTED_CHUNK_TOTAL)
        if (eventCount > 0L) {
            quality.addHousekeeping(QualityCounterId.WRITTEN_EVENT_TOTAL, eventCount)
            segmentEventRecords += eventCount
        }
        val dictionaryCount = chunkTypeCounts[JhlogV9.TYPE_DICTIONARY].toLong()
        segmentDictionaryRecords += dictionaryCount
        if (!final && (eventCount > 0L || dictionaryCount > 0L)) commitQualityPending = true
        chunkSequence++
        resetChunkState()
    }

    private fun resetChunkState() {
        rawChunk.reset()
        chunkTypeCounts.fill(0)
        chunkRecordCount = 0
        lastTimedRecordUs = fileHeader.segmentStartElapsedUs
        lastContext = null
    }

    private fun chunkHeader(flags: Int, storedSize: Int, rawSize: Int, records: Int, rawCrc: Long): ByteArray {
        val header = ByteArray(JhlogV9.CHUNK_HEADER_BYTES)
        System.arraycopy(JhlogV9.CHUNK_MAGIC, 0, header, 0, JhlogV9.CHUNK_MAGIC.size)
        putUInt16Le(header, 4, JhlogV9.CHUNK_HEADER_BYTES)
        putUInt16Le(header, 6, flags)
        putUInt32Le(header, 8, chunkSequence)
        putUInt32Le(header, 12, storedSize.toLong())
        putUInt32Le(header, 16, rawSize.toLong())
        putUInt32Le(header, 20, records.toLong())
        putUInt32Le(header, 24, rawCrc)
        putUInt32Le(header, 28, crc32(header, 0, 28))
        return header
    }

    private fun commitTrailer(storedSize: Int, rawSize: Int, rawCrc: Long): ByteArray {
        val trailer = ByteArray(JhlogV9.COMMIT_TRAILER_BYTES)
        System.arraycopy(JhlogV9.COMMIT_MAGIC, 0, trailer, 0, JhlogV9.COMMIT_MAGIC.size)
        putUInt32Le(trailer, 4, chunkSequence)
        putUInt32Le(trailer, 8, storedSize.toLong())
        putUInt32Le(trailer, 12, rawSize.toLong())
        putUInt32Le(trailer, 16, rawCrc)
        return trailer
    }

    private fun writeFileHeader() {
        val payload = Payload()
            .uvarint(JhlogV9.HEADER_SCHEMA)
            .uvarint(JhlogV9.REQUIRED_FEATURES)
            .uvarint(JhlogV9.OPTIONAL_FEATURES)
            .fixedBytes(exactId(fileHeader.runId))
            .fixedBytes(exactId(fileHeader.processInstanceId))
            .fixedBytes(exactId(fileHeader.sessionId))
            .uvarint(nonNegative(fileHeader.segmentIndex))
            .uvarint(nonNegative(fileHeader.osPid))
            .uvarint(nonNegative(fileHeader.collectorStartElapsedUs))
            .uvarint(nonNegative(fileHeader.segmentStartElapsedUs))
            .uvarint(nonNegative(fileHeader.segmentStartUnixMs))
            .uvarint(nonNegative(fileHeader.identitySource))
            .boundedString(fileHeader.processName, MAX_HEADER_STRING_BYTES)
            .boundedBytes(fileHeader.symbolNamespace, MAX_HEADER_STRING_BYTES)
            .copyBytes()
        if (payload.size > JhlogV9.MAX_FILE_HEADER_BYTES) {
            throw IOException("JHLOG v9 file header exceeds ${JhlogV9.MAX_FILE_HEADER_BYTES} bytes")
        }
        val headerBytes = (JhlogV9.FILE_MAGIC.size + 8 + payload.size).toLong()
        ensurePhysicalCapacity(headerBytes, TERMINAL_RESERVE_BYTES)
        bufferedOut.write(JhlogV9.FILE_MAGIC)
        writeUInt32Le(bufferedOut, payload.size.toLong())
        writeUInt32Le(bufferedOut, crc32(payload))
        bufferedOut.write(payload)
        bufferedOut.flush()
        logicalBytesWritten = headerBytes
    }

    private fun ensurePhysicalCapacity(bytes: Long, reservedBytes: Long) {
        if (physicalByteLimit == Long.MAX_VALUE) return
        val remaining = physicalByteLimit - logicalBytesWritten
        if (bytes < 0L || reservedBytes < 0L || remaining < 0L || bytes > remaining - reservedBytes) {
            throw LogSizeLimitReachedException(
                "JHLOG v9 physical size limit reached: path=$path limit=$physicalByteLimit written=$logicalBytesWritten",
            )
        }
    }

    private fun ensureWritable() {
        if (closed || poisoned) throw IOException("BinaryLogWriter is not writable")
    }

    @Synchronized
    internal fun abort() {
        if (closed) return
        closed = true
        poisoned = true
        runCatching { bufferedOut.close() }
    }

    @Synchronized
    internal fun sealSizeLimit() {
        if (closed) return
        if (poisoned) {
            abort()
            return
        }
        discardPendingEvents(QualityCounterId.REASON_SIZE_LIMIT)
        finishTerminal(JhlogV9.SEGMENT_END_SIZE_LIMIT)
    }

    @Synchronized
    internal fun sealIoError(): Boolean {
        if (closed) return false
        discardPendingEvents(QualityCounterId.REASON_IO_LOST)
        if (poisoned) {
            abort()
            return false
        }
        return try {
            finishTerminal(JhlogV9.SEGMENT_END_IO_ERROR)
            true
        } catch (error: Throwable) {
            if (error is VirtualMachineError || error is ThreadDeath) throw error
            abort()
            false
        }
    }

    @Synchronized
    internal fun close(reason: Long) {
        if (closed) return
        ensureWritable()
        try {
            commitChunk(final = false)
        } catch (_: LogSizeLimitReachedException) {
            sealSizeLimit()
            return
        }
        finishTerminal(reason)
    }

    private fun finishTerminal(reason: Long) {
        var finalCommitPredicted = false
        var finalCommitted = false
        var previousGeneration = lastQualityGeneration
        var previousSequence = lastQualitySequence
        try {
            previousGeneration = lastQualityGeneration
            previousSequence = lastQualitySequence
            // A terminal snapshot is observable only when its enclosing FINAL trailer commits.
            // Predict that commit so the snapshot is exact, then roll it back on any failed write.
            quality.addHousekeeping(QualityCounterId.COMMITTED_CHUNK_TOTAL)
            finalCommitPredicted = true
            quality.freeze()
            terminalChunkBuilding = true
            writeQualitySnapshot()
            writeSegmentEnd(reason)
            commitChunk(final = true, countCommit = false)
            finalCommitted = true
        } finally {
            terminalChunkBuilding = false
            if (finalCommitPredicted && !finalCommitted) {
                quality.subtractHousekeeping(QualityCounterId.COMMITTED_CHUNK_TOTAL)
                lastQualityGeneration = previousGeneration
                lastQualitySequence = previousSequence
                commitQualityPending = true
            }
            closed = true
            bufferedOut.close()
        }
    }

    private fun discardPendingEvents(reason: Int) {
        for (recordType in EVENT_RECORD_TYPES) {
            val count = chunkTypeCounts[recordType]
            if (count > 0) quality.addRejected(recordType, reason, count.toLong())
        }
        resetChunkState()
    }

    @Synchronized
    override fun close() {
        close(JhlogV9.SEGMENT_END_NORMAL)
    }

    private data class ContextIds(
        val screenId: Long,
        val ownerId: Long,
        val flowId: Long,
        val stepId: Long,
        val stableOwnerId: Long = 0L,
        val hasStableOwner: Boolean = false,
    )

    private class ExternalBinaryOutputStream(
        private val writer: JankHunterBinaryWriter,
    ) : OutputStream() {
        override fun write(oneByte: Int) {
            writeExternal { writer.writeByte(oneByte.toByte()) }
        }

        override fun write(buffer: ByteArray, offset: Int, length: Int) {
            writeExternal { writer.writeBytes(buffer, offset, length) }
        }

        override fun flush() {
            writeExternal(writer::flush)
        }

        override fun close() {
            writeExternal(writer::close)
        }

        private inline fun writeExternal(action: () -> Unit) {
            try {
                action()
            } catch (error: IOException) {
                throw error
            } catch (error: Throwable) {
                throw IOException("External Jank Hunter binary writer failed", error)
            }
        }
    }

    private class Payload {
        private val out = ByteArrayOutputStream(64)

        val size: Int
            get() = out.size()

        fun uvarint(rawValue: Long): Payload {
            writeUvarint(out, rawValue)
            return this
        }

        fun svarint(value: Long): Payload = uvarint((value shl 1) xor (value shr 63))

        fun symbolRef(localId: Long): Payload = uvarint(if (localId <= 0L) 0L else localId shl 1)

        fun stableSymbolRef(stableId: Long): Payload {
            uvarint(1L)
            repeat(Long.SIZE_BYTES) { byteIndex ->
                out.write((stableId ushr (byteIndex * Byte.SIZE_BITS)).toInt() and 0xff)
            }
            return this
        }

        fun bytes(value: ByteArray): Payload {
            out.write(value)
            return this
        }

        fun fixedBytes(value: ByteArray): Payload = bytes(value)

        fun boundedString(value: String, maxBytes: Int): Payload {
            return boundedBytes(validUtf8Prefix(value, maxBytes), maxBytes)
        }

        fun boundedBytes(value: ByteArray, maxBytes: Int): Payload {
            val safe = if (value.size <= maxBytes) value else value.copyOf(maxBytes)
            uvarint(safe.size.toLong())
            bytes(safe)
            return this
        }

        fun copyBytes(): ByteArray = out.toByteArray()
    }

    companion object {
        const val FLAG_THREAD_MAIN: Long = 1L shl 3
        const val FLAG_APP_FOREGROUND: Long = 1L shl 4
        const val FLAG_NETWORK_METERED: Long = 1L shl 5
        const val FLAG_CONTEXT_LOW_MEMORY: Long = 1L shl 6
        const val FLAG_NETWORK_VALIDATED: Long = 1L shl 7
        const val FLAG_NETWORK_VPN: Long = 1L shl 8
        const val FLAG_DEVICE_ROOTED: Long = 1L shl 9
        const val FLAG_HTTP_SLOW: Long = 1L shl 15
        const val FLAG_UI_PROBLEM: Long = 1L shl 16
        const val FLAG_HTTP_CLASSIFIED: Long = 1L shl 17
        const val FLAG_UI_CLASSIFIED: Long = 1L shl 18

        private const val RETAINED_EVIDENCE_TIME_ONLY = 1L
        private const val RETAINED_EVIDENCE_AFTER_EXPLICIT_GC = 2L

        private const val FLAG_KNOWN_MASK: Long =
            ((1L shl 10) - 1L) or FLAG_HTTP_SLOW or FLAG_UI_PROBLEM or FLAG_HTTP_CLASSIFIED or
                FLAG_UI_CLASSIFIED
        private const val IO_BUFFER_BYTES = 32 * 1024
        private const val TERMINAL_RESERVE_BYTES = 8L * 1024L
        private const val MAX_HEADER_STRING_BYTES = 1024
        private const val MAX_ENCODED_DICTIONARY_VALUE_BYTES = JhlogV9.TARGET_RAW_CHUNK_BYTES - 1024
        private const val DICTIONARY_ENCODING_UTF8 = 0L

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
        private const val DICT_FLOW = 11
        private const val DICT_STEP = 12
        private const val DICT_LOG_SOURCE = 13
        private const val DICT_STABLE_SYMBOL = 14

        private val EVENT_RECORD_TYPES = JhlogV9.TYPE_SESSION..JhlogV9.TYPE_RUNTIME_CALL

        private fun defaultFileHeader(): BinaryLogFileHeader {
            val elapsedUs = nowElapsedUs()
            return BinaryLogFileHeader(
                runId = BinaryLogFileHeader.randomId(),
                processInstanceId = BinaryLogFileHeader.randomId(),
                sessionId = BinaryLogFileHeader.randomId(),
                segmentIndex = 1L,
                osPid = Process.myPid().toLong().coerceAtLeast(0L),
                collectorStartElapsedUs = elapsedUs,
                segmentStartElapsedUs = elapsedUs,
                segmentStartUnixMs = System.currentTimeMillis().coerceAtLeast(0L),
                identitySource = 0L,
                processName = "unknown",
                symbolNamespace = ByteArray(0),
            )
        }

        private fun nonNegative(value: Long): Long = value.coerceAtLeast(0L)

        private fun foregroundFlag(foreground: Boolean): Long = if (foreground) FLAG_APP_FOREGROUND else 0L

        private fun clampDuration(value: Long, durationMs: Long): Long = nonNegative(value).coerceAtMost(durationMs)

        private fun exactId(value: ByteArray): ByteArray {
            return if (value.size == 16) value else value.copyOf(16)
        }

        private fun validUtf8Prefix(value: String, maxBytes: Int): ByteArray {
            val limit = maxBytes.coerceAtLeast(0)
            val encoded = value.toByteArray(StandardCharsets.UTF_8)
            if (encoded.size <= limit) return encoded
            if (limit == 0) return ByteArray(0)

            val builder = StringBuilder()
            var usedBytes = 0
            var offset = 0
            while (offset < value.length) {
                val codePoint = value.codePointAt(offset)
                val charCount = Character.charCount(codePoint)
                val codePointBytes = value
                    .substring(offset, offset + charCount)
                    .toByteArray(StandardCharsets.UTF_8)
                if (usedBytes + codePointBytes.size > limit) break
                builder.appendCodePoint(codePoint)
                usedBytes += codePointBytes.size
                offset += charCount
            }
            return builder.toString().toByteArray(StandardCharsets.UTF_8)
        }

        private fun nowElapsedUs(): Long = SystemClock.elapsedRealtimeNanos().coerceAtLeast(0L) / 1_000L

        private fun gzip(raw: ByteArray): ByteArray {
            val compressed = ByteArrayOutputStream(raw.size.coerceAtLeast(64))
            GZIPOutputStream(compressed).use { gzip -> gzip.write(raw) }
            return compressed.toByteArray()
        }

        private fun crc32(bytes: ByteArray, offset: Int = 0, length: Int = bytes.size): Long {
            return CRC32().apply { update(bytes, offset, length) }.value
        }

        private fun writeUvarint(out: OutputStream, rawValue: Long) {
            // Long carries unsigned varint bits here. Semantic unsigned fields are sanitized by
            // their callers; keeping the sign bit is required for zig-zag encoded Long.MIN_VALUE.
            var value = rawValue
            while (value and 0x7fL.inv() != 0L) {
                out.write(((value and 0x7fL) or 0x80L).toInt())
                value = value ushr 7
            }
            out.write(value.toInt())
        }

        private fun writeUInt32Le(out: OutputStream, value: Long) {
            repeat(4) { shift -> out.write((value ushr (shift * 8)).toInt() and 0xff) }
        }

        private fun putUInt16Le(target: ByteArray, offset: Int, value: Int) {
            target[offset] = (value and 0xff).toByte()
            target[offset + 1] = ((value ushr 8) and 0xff).toByte()
        }

        private fun putUInt32Le(target: ByteArray, offset: Int, value: Long) {
            repeat(4) { index -> target[offset + index] = ((value ushr (index * 8)) and 0xff).toByte() }
        }
    }
}
