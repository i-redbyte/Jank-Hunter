package io.jankhunter.runtime.internal.io

import android.os.Process
import android.os.SystemClock
import io.jankhunter.runtime.JankHunterBinaryWriter
import io.jankhunter.runtime.internal.saturatingAdd
import java.io.ByteArrayOutputStream
import java.io.Closeable
import java.io.File
import java.io.IOException
import java.nio.charset.StandardCharsets
import java.security.MessageDigest
import java.util.zip.GZIPOutputStream

internal class LogSizeLimitReachedException(message: String) : IOException(message)

internal class BinaryLogWriter private constructor(
    private val container: BinaryLogContainer,
    maxDictionaryEntries: Int,
    maxDictionaryValueBytes: Int,
    private val fileHeader: BinaryLogFileHeader,
    private val quality: LogQualityCounters,
    private val logGrowth: LogGrowthSessionBinding?,
) : Closeable {
    internal val file: File?
        get() = container.file
    internal val path: String
        get() = container.path

    constructor(
        file: File,
        maxDictionaryEntries: Int = DictionaryIds.DEFAULT_MAX_REGULAR_ENTRIES,
        maxDictionaryValueBytes: Int = DictionaryIds.DEFAULT_MAX_VALUE_BYTES,
        maxPhysicalBytes: Long = DEFAULT_LOCAL_FILE_LIMIT_BYTES,
        archiveBudget: RunArchiveBudget? = null,
    ) : this(
        container = SequentialJhlogContainer(file, maxPhysicalBytes, archiveBudget),
        maxDictionaryEntries = maxDictionaryEntries,
        maxDictionaryValueBytes = maxDictionaryValueBytes,
        fileHeader = defaultFileHeader(),
        quality = LogQualityCounters(),
        logGrowth = null,
    )

    internal constructor(
        file: File,
        maxDictionaryEntries: Int,
        maxDictionaryValueBytes: Int,
        fileHeader: BinaryLogFileHeader,
        quality: LogQualityCounters,
        maxPhysicalBytes: Long = DEFAULT_LOCAL_FILE_LIMIT_BYTES,
        logGrowth: LogGrowthSessionBinding? = null,
        archiveBudget: RunArchiveBudget? = null,
    ) : this(
        container = SequentialJhlogContainer(file, maxPhysicalBytes, archiveBudget),
        maxDictionaryEntries = maxDictionaryEntries,
        maxDictionaryValueBytes = maxDictionaryValueBytes,
        fileHeader = fileHeader,
        quality = quality,
        logGrowth = logGrowth,
    )

    internal constructor(
        writer: JankHunterBinaryWriter,
        maxDictionaryEntries: Int = DictionaryIds.DEFAULT_MAX_REGULAR_ENTRIES,
        maxDictionaryValueBytes: Int = DictionaryIds.DEFAULT_MAX_VALUE_BYTES,
        maxPhysicalBytes: Long = 0L,
    ) : this(
        container = SequentialJhlogContainer(writer, maxPhysicalBytes),
        maxDictionaryEntries = maxDictionaryEntries,
        maxDictionaryValueBytes = maxDictionaryValueBytes,
        fileHeader = defaultFileHeader(),
        quality = LogQualityCounters(),
        logGrowth = null,
    )

    internal constructor(
        writer: JankHunterBinaryWriter,
        maxDictionaryEntries: Int,
        maxDictionaryValueBytes: Int,
        fileHeader: BinaryLogFileHeader,
        quality: LogQualityCounters,
        maxPhysicalBytes: Long = 0L,
        logGrowth: LogGrowthSessionBinding? = null,
        archiveBudget: RunArchiveBudget? = null,
    ) : this(
        container = SequentialJhlogContainer(writer, maxPhysicalBytes, archiveBudget),
        maxDictionaryEntries = maxDictionaryEntries,
        maxDictionaryValueBytes = maxDictionaryValueBytes,
        fileHeader = fileHeader,
        quality = quality,
        logGrowth = logGrowth,
    )

    private val dictionary = DictionaryIds(
        maxDictionaryEntries,
        minOf(maxDictionaryValueBytes, MAX_ENCODED_DICTIONARY_VALUE_BYTES),
    )
    private val stableSymbolDefinitions = HashMap<Long, String>()
    private val chunkDictionaryUsage = ChunkDictionaryUsage(container.usesChunkLocalDictionary)
    private val rawChunk = ByteArrayOutputStream(Jhlog.TARGET_RAW_CHUNK_BYTES)
    private val chunkTypeCounts = LongArray(Jhlog.TYPE_IO + 1)
    private var chunkRecordCount = 0
    private var chunkSequence = 0L
    private var lastTimedRecordUs = fileHeader.segmentStartElapsedUs
    private var lastContext: ContextIds? = null
    private val producerOverride = ProducerMetadataBuffer()
    private val directProducer = ProducerMetadataBuffer()
    private var producerOverrideActive = false
    private var segmentEventRecords = 0L
    private var segmentDictionaryRecords = 0L
    private var lastQualityGeneration = -1L
    private var lastQualitySequence = 0L
    private var commitQualityPending = false
    private var terminalChunkBuilding = false
    private var closed = false
    private var poisoned = false
    private var storageBudgetExhausted = false
    private var sealedSegmentDigest: ByteArray? = null

    init {
        try {
            writeFileHeader()
            beginLogGrowth()
        } catch (error: Throwable) {
            runCatching { container.close() }
            throw error
        }
    }

    @Synchronized
    fun bytesWritten(): Long = container.retainedBytes()

    @Synchronized
    internal fun logGrowthStats(): LogContainerStats = combinedLogGrowthStats()

    @Synchronized
    fun flush() {
        ensureWritable()
        // Commit data first, then publish an exact snapshot in its own committed control chunk.
        commitChunk(final = false)
        commitQualityControlChunk()
        container.flush()
    }

    @Synchronized
    internal fun writeLogGrowthSummary(): Boolean {
        ensureWritable()
        commitChunk(final = false)
        commitQualityControlChunk()
        container.flush()
        val binding = logGrowth ?: return false
        return try {
            val stats = combinedLogGrowthStats()
            val live = binding.manager.checkpoint(stats) ?: return false
            writeLogGrowth(Jhlog.LOG_GROWTH_LIVE, live)
            commitChunk(final = false)
            container.flush()
            true
        } catch (error: Throwable) {
            if (error is VirtualMachineError || error is ThreadDeath) throw error
            false
        }
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
    fun session(
        appVersion: String?,
        build: String?,
        device: String?,
        sdkInt: Int,
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
        collectorFlags: Long,
        appForeground: Boolean = true,
    ) {
        require(collectorFlags and Jhlog.COLLECTOR_KNOWN_MASK.inv() == 0L)
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
            .uvarint(nonNegative(collectorFlags))
        var attributes = foregroundFlag(appForeground)
        if (deviceRooted) attributes = attributes or FLAG_DEVICE_ROOTED
        record(Jhlog.TYPE_SESSION, attributes, payload, currentProducerContext())
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
        record(Jhlog.TYPE_DEVICE_CONTEXT, attributes, payload, currentProducerContext())
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
        record(Jhlog.TYPE_HTTP, flags and FLAG_KNOWN_MASK, payload, context)
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
        record(Jhlog.TYPE_STALL, FLAG_THREAD_MAIN or foregroundFlag(foreground), payload, context)
    }

    @Synchronized
    fun memory(pssKb: Long, javaHeapKb: Long, nativeHeapKb: Long, foreground: Boolean = true) {
        val payload = Payload()
            .uvarint(nonNegative(pssKb))
            .uvarint(nonNegative(javaHeapKb))
            .uvarint(nonNegative(nativeHeapKb))
        record(Jhlog.TYPE_MEMORY, foregroundFlag(foreground), payload, currentProducerContext())
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
            Jhlog.TYPE_RETAINED,
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
        source: Long,
        frameDeadlineUs: Long,
        frameDurationBuckets: LongArray,
        foreground: Boolean = true,
        flags: Long = 0L,
    ) {
        val safeWindowMs = nonNegative(windowMs).coerceAtLeast(1L)
        val safeFrameCount = nonNegative(frameCount)
        val safeJankCount = nonNegative(jankCount).coerceAtMost(safeFrameCount)
        require(source == Jhlog.UI_SOURCE_JANKSTATS || source == Jhlog.UI_SOURCE_CHOREOGRAPHER)
        require(frameDeadlineUs > 0L)
        require(frameDurationBuckets.size == Jhlog.UI_FRAME_HISTOGRAM_BUCKET_COUNT)
        require(frameDurationBuckets.all { it >= 0L })
        require(saturatingSum(frameDurationBuckets) == safeFrameCount)
        val payload = Payload()
            .uvarint(safeWindowMs)
            .uvarint(safeFrameCount)
            .uvarint(safeJankCount)
            .uvarint(source)
            .uvarint(frameDeadlineUs)
        frameDurationBuckets.forEach { count -> payload.uvarint(nonNegative(count)) }
        val context = currentProducerContext().withScreen(screen)
        val uiFlags = flags and (FLAG_UI_PROBLEM or FLAG_UI_CLASSIFIED)
        val attributes = FLAG_THREAD_MAIN or foregroundFlag(foreground) or uiFlags
        record(Jhlog.TYPE_UI_WINDOW, attributes, payload, context)
    }

    @Synchronized
    fun processExit(
        reason: Long,
        timestampUnixMs: Long,
        importance: Long,
        pssKb: Long,
        rssKb: Long,
        processName: String?,
    ) {
        require(timestampUnixMs > 0L)
        val payload = Payload()
            .uvarint(nonNegative(reason))
            .uvarint(timestampUnixMs)
            .uvarint(nonNegative(importance))
            .uvarint(nonNegative(pssKb))
            .uvarint(nonNegative(rssKb))
            .symbolRef(optionalIdFor(DICT_PROCESS, processName))
        record(Jhlog.TYPE_PROCESS_EXIT, 0L, payload, currentProducerContext())
    }

    @Synchronized
    fun io(operation: Long, durationUs: Long, bytes: Long, mainThread: Boolean) {
        require(operation in Jhlog.IO_FILE_READ..Jhlog.IO_CONTENT_WRITE)
        val payload = Payload()
            .uvarint(operation)
            .uvarint(nonNegative(durationUs))
            .uvarint(nonNegative(bytes))
        record(Jhlog.TYPE_IO, if (mainThread) FLAG_THREAD_MAIN else 0L, payload, currentProducerContext())
    }

    @Synchronized
    fun counter(name: String?, value: Long) {
        if (value < 0L) {
            quality.add(QualityCounterId.INVALID_METRIC)
            return
        }
        metric(Jhlog.TYPE_COUNTER, name, value, 1L, value, value, MetricAggregationMode.UNKNOWN)
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
        record(Jhlog.TYPE_COUNTER, 0L, payload, context = null)
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
        metric(Jhlog.TYPE_GAUGE, name, value, count.coerceAtLeast(1L), sum, max, mode)
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
        record(Jhlog.TYPE_LOG_SPAM, 0L, payload, contextIds(screen, owner, flow, step))
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
            Jhlog.TYPE_PROBLEM,
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
            .uvarint(1L)
            .symbolRef(optionalIdFor(DICT_SCREEN, screen))
            .stableSymbolRef(callerId)
            .symbolRef(optionalIdFor(DICT_FLOW, flow))
            .symbolRef(optionalIdFor(DICT_STEP, step))
            .stableSymbolRef(calleeId)
            .uvarint(nonNegative(count))
            .uvarint(nonNegative(totalMs))
            .uvarint(nonNegative(maxMs))
        record(Jhlog.TYPE_RUNTIME_CALL, 0L, payload, null)
    }

    /** Writes one bounded structure-of-arrays runtime-call record to the wire. */
    @Synchronized
    fun runtimeCalls(batch: RuntimeCallBatch) {
        if (batch.size == 0) return
        require(batch.size <= Jhlog.MAX_RUNTIME_CALL_BLOCK_ROWS) {
            "Runtime call block has ${batch.size} rows; maximum is ${Jhlog.MAX_RUNTIME_CALL_BLOCK_ROWS}"
        }
        for (index in 0 until batch.size) {
            ensureStableSymbolDefinition(batch.callerId(index), batch.callerName(index))
            ensureStableSymbolDefinition(batch.calleeId(index), batch.calleeName(index))
        }
        val payload = Payload().uvarint(batch.size.toLong())
        for (index in 0 until batch.size) payload.symbolRef(optionalIdFor(DICT_SCREEN, batch.screen(index)))
        for (index in 0 until batch.size) payload.stableSymbolRef(batch.callerId(index))
        for (index in 0 until batch.size) payload.symbolRef(optionalIdFor(DICT_FLOW, batch.flow(index)))
        for (index in 0 until batch.size) payload.symbolRef(optionalIdFor(DICT_STEP, batch.step(index)))
        for (index in 0 until batch.size) payload.stableSymbolRef(batch.calleeId(index))
        for (index in 0 until batch.size) payload.uvarint(nonNegative(batch.count(index)))
        for (index in 0 until batch.size) payload.uvarint(nonNegative(batch.totalMs(index)))
        for (index in 0 until batch.size) payload.uvarint(nonNegative(batch.maxMs(index)))
        record(
            recordType = Jhlog.TYPE_RUNTIME_CALL,
            attributes = 0L,
            payload = payload,
            context = null,
            semanticEventCount = batch.size.toLong(),
        )
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
        if (container.usesChunkLocalDictionary) {
            val definition = dictionary.definition(result.id)
                ?: throw IOException("Missing dictionary definition ${result.id}")
            chunkDictionaryUsage.markLocal(
                result.id,
                dictionaryDefinitionRecordSize(definition.kind, definition.id, definition.value),
            )
        }
        result.definition?.let { definition ->
            if (!container.usesChunkLocalDictionary) writeDictionaryDefinition(definition)
        }
        return result.id
    }

    private fun writeDictionaryDefinition(definition: DictionaryIds.Definition) {
        val bytes = definition.value.toByteArray(StandardCharsets.UTF_8)
        val payload = Payload()
            .uvarint(definition.kind.toLong())
            .uvarint(definition.id)
            .uvarint(DICTIONARY_ENCODING_UTF8)
            .uvarint(bytes.size.toLong())
            .bytes(bytes)
        record(
            recordType = Jhlog.TYPE_DICTIONARY,
            attributes = 0L,
            payload = payload,
            context = null,
            producer = null,
        )
    }

    /**
     * Stable method definitions are intentionally outside [DictionaryIds]: their IDs are already
     * assigned by ASM and must not consume the bounded regular dictionary used by runtime data.
     */
    private fun ensureStableSymbolDefinition(stableId: Long, rawName: String?) {
        val existing = stableSymbolDefinitions[stableId]
        if (existing != null) {
            chunkDictionaryUsage.markStable(
                stableId,
                dictionaryDefinitionRecordSize(DICT_STABLE_SYMBOL, stableId, existing),
            )
            return
        }
        val name = rawName?.takeIf(String::isNotBlank) ?: return

        val encoded = name.toByteArray(StandardCharsets.UTF_8)
        val bytes = if (encoded.size <= MAX_ENCODED_DICTIONARY_VALUE_BYTES) {
            encoded
        } else {
            quality.add(QualityCounterId.DICTIONARY_VALUE_TRUNCATED_TOTAL)
            validUtf8Prefix(name, MAX_ENCODED_DICTIONARY_VALUE_BYTES)
        }
        stableSymbolDefinitions[stableId] = name
        chunkDictionaryUsage.markStable(
            stableId,
            dictionaryDefinitionRecordSize(DICT_STABLE_SYMBOL, stableId, name),
        )
        if (container.usesChunkLocalDictionary) return
        val payload = Payload()
            .uvarint(DICT_STABLE_SYMBOL.toLong())
            .uvarint(stableId)
            .uvarint(DICTIONARY_ENCODING_UTF8)
            .uvarint(bytes.size.toLong())
            .bytes(bytes)
        record(
            recordType = Jhlog.TYPE_DICTIONARY,
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
        semanticEventCount: Long = 1L,
    ) {
        ensureWritable()
        var encoded = encodeRecord(recordType, attributes, payload, context, producer)
        val chunkTarget = if (terminalChunkBuilding) {
            Jhlog.MAX_RAW_CHUNK_BYTES
        } else {
            Jhlog.TARGET_RAW_CHUNK_BYTES
        }
        val projectedRawBytes = rawChunk.size().toLong() + encoded.size.toLong() +
            chunkDictionaryUsage.projectedDefinitionBytes().toLong()
        if (
            rawChunk.size() > 0 &&
            (rawChunk.size() + encoded.size > chunkTarget || projectedRawBytes > chunkTarget)
        ) {
            commitChunk(final = false)
            encoded = encodeRecord(recordType, attributes, payload, context, producer)
        }
        var rawBytes = rawChunk.size().toLong() + encoded.size.toLong() +
            chunkDictionaryUsage.projectedDefinitionBytes().toLong()
        val reservedBytes = if (terminalChunkBuilding) 0L else TERMINAL_RESERVE_BYTES
        if (!container.canCommitRaw(rawBytes.coerceAtMost(Int.MAX_VALUE.toLong()).toInt(), reservedBytes)) {
            if (rawChunk.size() > 0) {
                commitChunk(final = false)
                encoded = encodeRecord(recordType, attributes, payload, context, producer)
                rawBytes = encoded.size.toLong() + chunkDictionaryUsage.projectedDefinitionBytes().toLong()
            }
            if (!container.canCommitRaw(rawBytes.coerceAtMost(Int.MAX_VALUE.toLong()).toInt(), reservedBytes)) {
                throw LogSizeLimitReachedException(
                    "JHLOG ${Jhlog.FORMAT_VERSION} segment has no capacity for record type $recordType",
                )
            }
        }
        chunkDictionaryUsage.commitPending()
        if (encoded.size > Jhlog.MAX_RAW_CHUNK_BYTES) {
            if (recordType == Jhlog.TYPE_DICTIONARY) {
                throw IOException("JHLOG ${Jhlog.FORMAT_VERSION} dictionary definition exceeds raw chunk limit")
            }
            quality.addRejected(recordType, QualityCounterId.REASON_OVERSIZED, semanticEventCount)
            return
        }
        rawChunk.write(encoded)
        chunkRecordCount++
        if (recordType in chunkTypeCounts.indices) {
            chunkTypeCounts[recordType] = saturatingAdd(chunkTypeCounts[recordType], semanticEventCount)
        }
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
        if (producer != null) envelopeFlags = envelopeFlags or Jhlog.ENVELOPE_HAS_TIME or Jhlog.ENVELOPE_HAS_THREAD
        if (context != null) envelopeFlags = envelopeFlags or Jhlog.ENVELOPE_HAS_CONTEXT
        val sameContext = context != null && lastContext == context
        if (sameContext) envelopeFlags = envelopeFlags or Jhlog.ENVELOPE_SAME_CONTEXT
        val safeAttributes = attributes and FLAG_KNOWN_MASK
        if (safeAttributes != 0L) envelopeFlags = envelopeFlags or Jhlog.ENVELOPE_HAS_ATTRIBUTES

        val body = Payload()
            .uvarint(recordType.toLong())
            .uvarint(envelopeFlags)
        if (producer != null) {
            body.svarint(producer.elapsedUs - lastTimedRecordUs)
            body.uvarint(producer.threadId)
        }
        if (context != null && !sameContext) {
            var presence = 0L
            if (context.screenId != 0L) presence = presence or Jhlog.CONTEXT_SCREEN
            if (context.ownerId != 0L || context.hasStableOwner) {
                presence = presence or Jhlog.CONTEXT_OWNER
            }
            if (context.flowId != 0L) presence = presence or Jhlog.CONTEXT_FLOW
            if (context.stepId != 0L) presence = presence or Jhlog.CONTEXT_STEP
            body.uvarint(presence)
            if (presence and Jhlog.CONTEXT_SCREEN != 0L) body.symbolRef(context.screenId)
            if (presence and Jhlog.CONTEXT_OWNER != 0L) {
                if (context.hasStableOwner) {
                    body.stableSymbolRef(context.stableOwnerId)
                } else {
                    body.symbolRef(context.ownerId)
                }
            }
            if (presence and Jhlog.CONTEXT_FLOW != 0L) body.symbolRef(context.flowId)
            if (presence and Jhlog.CONTEXT_STEP != 0L) body.symbolRef(context.stepId)
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
            recordType = Jhlog.TYPE_QUALITY_SNAPSHOT,
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
            recordType = Jhlog.TYPE_SEGMENT_END,
            attributes = 0L,
            payload = payload,
            context = null,
            producer = null,
        )
    }

    private fun commitChunk(final: Boolean, countCommit: Boolean = true) {
        if (rawChunk.size() == 0) return
        val encodedChunk = encodedChunk()
        val raw = encodedChunk.raw
        val stored = gzip(raw)
        val rawCrc = crc32(raw)
        val flags = Jhlog.CHUNK_FLAG_GZIP or if (final) Jhlog.CHUNK_FLAG_FINAL else 0
        try {
            container.commitChunk(
                flags = flags,
                sequence = chunkSequence,
                stored = stored,
                rawSize = raw.size,
                recordCount = chunkRecordCount + encodedChunk.dictionaryRecords,
                rawCrc = rawCrc,
                terminalReserveBytes = if (final) 0L else TERMINAL_RESERVE_BYTES,
            )
        } catch (error: StorageBudgetExhaustedException) {
            throw error
        } catch (error: LogSizeLimitReachedException) {
            throw error
        } catch (error: IOException) {
            poisoned = true
            quality.add(QualityCounterId.FAILED_CHUNK_TOTAL)
            for (recordType in EVENT_RECORD_TYPES) {
                val count = chunkTypeCounts[recordType]
                if (count > 0) quality.addRejected(recordType, QualityCounterId.REASON_IO_LOST, count)
            }
            resetChunkState()
            throw error
        }
        var eventCount = 0L
        for (recordType in EVENT_RECORD_TYPES) {
            eventCount = saturatingAdd(eventCount, chunkTypeCounts[recordType])
        }
        if (countCommit) quality.addHousekeeping(QualityCounterId.COMMITTED_CHUNK_TOTAL)
        if (eventCount > 0L) {
            quality.add(QualityCounterId.WRITTEN_EVENT_TOTAL, eventCount)
            segmentEventRecords = saturatingAdd(segmentEventRecords, eventCount)
        }
        val dictionaryCount = chunkTypeCounts[Jhlog.TYPE_DICTIONARY] +
            encodedChunk.dictionaryRecords.toLong()
        segmentDictionaryRecords = saturatingAdd(segmentDictionaryRecords, dictionaryCount)
        if (!final && (eventCount > 0L || dictionaryCount > 0L)) commitQualityPending = true
        chunkSequence++
        resetChunkState()
    }

    private fun resetChunkState() {
        rawChunk.reset()
        chunkDictionaryUsage.clearChunk()
        chunkTypeCounts.fill(0)
        chunkRecordCount = 0
        lastTimedRecordUs = fileHeader.segmentStartElapsedUs
        lastContext = null
    }

    private fun encodedChunk(): EncodedChunk {
        val body = rawChunk.toByteArray()
        if (!container.usesChunkLocalDictionary) return EncodedChunk(body, 0)
        val dictionaryRecords = chunkDictionaryUsage.chunkLocalCount() + chunkDictionaryUsage.chunkStableCount()
        if (dictionaryRecords == 0) return EncodedChunk(body, 0)

        val output = ByteArrayOutputStream(body.size + dictionaryRecords * 32)
        chunkDictionaryUsage.forEachChunkLocal { id ->
            val definition = dictionary.definition(id)
                ?: throw IOException("Missing chunk-local dictionary definition $id")
            output.write(encodedDictionaryDefinition(definition.kind, definition.id, definition.value))
        }
        chunkDictionaryUsage.forEachChunkStable { id ->
            val value = stableSymbolDefinitions[id]
                ?: throw IOException("Missing stable symbol definition $id")
            output.write(encodedDictionaryDefinition(DICT_STABLE_SYMBOL, id, value))
        }
        output.write(body)
        if (output.size() > Jhlog.MAX_RAW_CHUNK_BYTES) {
            throw IOException(
                "JHLOG ${Jhlog.FORMAT_VERSION} self-contained chunk exceeds ${Jhlog.MAX_RAW_CHUNK_BYTES} raw bytes",
            )
        }
        return EncodedChunk(output.toByteArray(), dictionaryRecords)
    }

    private fun encodedDictionaryDefinition(kind: Int, id: Long, value: String): ByteArray {
        val bytes = value.toByteArray(StandardCharsets.UTF_8)
        val payload = Payload()
            .uvarint(kind.toLong())
            .uvarint(id)
            .uvarint(DICTIONARY_ENCODING_UTF8)
            .uvarint(bytes.size.toLong())
            .bytes(bytes)
        return encodeRecord(
            recordType = Jhlog.TYPE_DICTIONARY,
            attributes = 0L,
            payload = payload,
            context = null,
            producer = null,
        )
    }

    private fun dictionaryDefinitionRecordSize(kind: Int, id: Long, value: String): Int {
        val valueBytes = utf8Length(value)
        val payloadBytes = uvarintSize(kind.toLong()) + uvarintSize(id) +
            uvarintSize(DICTIONARY_ENCODING_UTF8) + uvarintSize(valueBytes.toLong()) + valueBytes
        val bodyBytes = uvarintSize(Jhlog.TYPE_DICTIONARY.toLong()) + uvarintSize(0L) + payloadBytes
        return uvarintSize(bodyBytes.toLong()) + bodyBytes
    }

    private fun utf8Length(value: String): Int {
        var bytes = 0
        var offset = 0
        while (offset < value.length) {
            val first = value[offset]
            if (
                Character.isHighSurrogate(first) &&
                offset + 1 < value.length &&
                Character.isLowSurrogate(value[offset + 1])
            ) {
                bytes += 4
                offset += 2
            } else {
                bytes += when {
                    Character.isSurrogate(first) || first.code <= 0x7f -> 1
                    first.code <= 0x7ff -> 2
                    else -> 3
                }
                offset++
            }
        }
        return bytes
    }

    private fun uvarintSize(rawValue: Long): Int {
        var value = rawValue
        var bytes = 1
        while (value and 0x7fL.inv() != 0L) {
            bytes++
            value = value ushr 7
        }
        return bytes
    }

    private fun writeFileHeader() {
        validateProcessScope()
        val payload = Payload()
            .uvarint(Jhlog.HEADER_SCHEMA)
            .uvarint(fileHeader.requiredFeatures)
            .uvarint(Jhlog.OPTIONAL_FEATURES)
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
            .uvarint(nonNegative(fileHeader.processScope))
            .uvarint(nonNegative(fileHeader.allowedProcessCount))
            .boundedBytes(fileHeader.processScopeFingerprint, PROCESS_SCOPE_FINGERPRINT_BYTES)
            .boundedBytes(fileHeader.previousSegmentDigest, SEGMENT_DIGEST_BYTES)
            .uvarint(nonNegative(fileHeader.expectedProcessCount))
            .boundedBytes(fileHeader.expectedProcessFingerprint, PROCESS_ROSTER_FINGERPRINT_BYTES)
            .uvarint(if (fileHeader.processRosterDeclarationComplete) 1L else 0L)
            .copyBytes()
        container.writeFileHeader(payload)
    }

    private fun validateProcessScope() {
        if (
            fileHeader.requiredFeatures != Jhlog.REQUIRED_FEATURES &&
            fileHeader.requiredFeatures != Jhlog.BEST_EFFORT_FEATURES
        ) {
            throw IOException(
                "Unsupported JHLOG 2.0.0 required feature contract " +
                    "0x${fileHeader.requiredFeatures.toString(16)}",
            )
        }
        when (fileHeader.processScope) {
            Jhlog.PROCESS_SCOPE_ALL,
            Jhlog.PROCESS_SCOPE_MAIN_ONLY,
            -> if (fileHeader.allowedProcessCount != 0L || fileHeader.processScopeFingerprint.isNotEmpty()) {
                throw IOException("Non-allowlist process scope cannot declare an allowlist identity")
            }
            Jhlog.PROCESS_SCOPE_ALLOWLIST -> if (
                fileHeader.allowedProcessCount <= 0L ||
                fileHeader.processScopeFingerprint.size != PROCESS_SCOPE_FINGERPRINT_BYTES
            ) {
                throw IOException(
                    "Allowlist process scope must declare at least one process and a full fingerprint",
                )
            }
            else -> throw IOException("Unknown process scope ${fileHeader.processScope}")
        }
        if (fileHeader.segmentIndex == 0L) {
            if (fileHeader.previousSegmentDigest.isNotEmpty()) {
                throw IOException("First segment cannot declare a predecessor digest")
            }
        } else if (fileHeader.previousSegmentDigest.size != SEGMENT_DIGEST_BYTES) {
            throw IOException("Segment ${fileHeader.segmentIndex} must declare a full predecessor digest")
        }
        if (
            fileHeader.expectedProcessCount <= 0L ||
            fileHeader.expectedProcessFingerprint.size != PROCESS_ROSTER_FINGERPRINT_BYTES
        ) {
            throw IOException("Process roster must contain at least one process and a full fingerprint")
        }
    }

    private fun beginLogGrowth() {
        val binding = logGrowth ?: return
        try {
            if (fileHeader.segmentIndex == 0L) {
                val started = binding.manager.beginSession(
                    sessionId = fileHeader.sessionId,
                    localDate = binding.localDate,
                    startedAtMs = fileHeader.segmentStartUnixMs,
                    configuredLimitBytes = binding.configuredLimitBytes,
                    stats = combinedLogGrowthStats(),
                )
                writeLogGrowth(Jhlog.LOG_GROWTH_HISTORY, started.history)
                writeLogGrowth(Jhlog.LOG_GROWTH_LIVE, started.live)
            } else {
                binding.manager.checkpoint(combinedLogGrowthStats())?.let { live ->
                    writeLogGrowth(Jhlog.LOG_GROWTH_LIVE, live)
                }
            }
        } catch (error: Throwable) {
            if (error is VirtualMachineError || error is ThreadDeath) throw error
        }
    }

    private fun publishLogGrowthCompletion() {
        val binding = logGrowth ?: return
        try {
            val live = binding.manager.complete(combinedLogGrowthStats()) ?: return
            writeLogGrowth(Jhlog.LOG_GROWTH_LIVE, live)
            commitChunk(final = false)
        } catch (error: Throwable) {
            if (error is VirtualMachineError || error is ThreadDeath) throw error
        }
    }

    private fun writeLogGrowth(kind: Long, raw: ByteArray) {
        val payload = Payload()
            .uvarint(kind)
            .uvarint(raw.size.toLong())
            .bytes(raw)
        record(
            recordType = Jhlog.TYPE_LOG_GROWTH,
            attributes = 0L,
            payload = payload,
            context = null,
            producer = null,
        )
    }

    private fun combinedLogGrowthStats(): LogContainerStats {
        val current = container.stats()
        val base = logGrowth?.baseStats ?: return current
        return LogContainerStats(
            retainedBytes = saturatedAdd(base.retainedBytes, current.retainedBytes),
            generatedBytes = saturatedAdd(base.generatedBytes, current.generatedBytes),
            limitReachedCount = maxOf(base.limitReachedCount, if (storageBudgetExhausted) 1L else 0L),
            segmentRotationCount = saturatedAdd(base.segmentRotationCount, current.segmentRotationCount),
            archiveEvictedBytes = maxOf(
                base.archiveEvictedBytes,
                current.archiveEvictedBytes,
                quality.value(QualityCounterId.ARCHIVE_EVICTED_BYTES_TOTAL),
            ),
        )
    }

    private fun ensureWritable() {
        if (closed || poisoned) throw IOException("BinaryLogWriter is not writable")
    }

    @Synchronized
    internal fun abort() {
        if (closed) return
        closed = true
        poisoned = true
        runCatching { container.close() }
    }

    @Synchronized
    internal fun sealSizeLimit() {
        if (closed) return
        if (poisoned) {
            abort()
            return
        }
        discardPendingEvents(QualityCounterId.REASON_SIZE_LIMIT)
        finishTerminal(Jhlog.SEGMENT_END_SIZE_LIMIT)
    }

    @Synchronized
    internal fun sealStorageBudget() {
        if (closed) return
        if (poisoned) {
            abort()
            return
        }
        storageBudgetExhausted = true
        discardPendingEvents(QualityCounterId.REASON_STORAGE_BUDGET)
        finishTerminal(Jhlog.SEGMENT_END_STORAGE_BUDGET)
    }

    @Synchronized
    internal fun sealRotation() {
        if (closed) return
        ensureWritable()
        finishTerminal(Jhlog.SEGMENT_END_ROTATION, completeGrowth = false, freezeQuality = false)
    }

    @Synchronized
    internal fun sealedDigest(): ByteArray? = sealedSegmentDigest?.copyOf()

    @Synchronized
    internal fun sealIoError(): Boolean {
        if (closed) return false
        discardPendingEvents(QualityCounterId.REASON_IO_LOST)
        if (poisoned) {
            abort()
            return false
        }
        return try {
            finishTerminal(Jhlog.SEGMENT_END_IO_ERROR)
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

    private fun finishTerminal(
        reason: Long,
        completeGrowth: Boolean = true,
        freezeQuality: Boolean = true,
    ) {
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
            if (freezeQuality) quality.freeze()
            terminalChunkBuilding = true
            if (completeGrowth) publishLogGrowthCompletion()
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
            container.close()
            if (finalCommitted) sealedSegmentDigest = container.finishDigest()
        }
    }

    private fun discardPendingEvents(reason: Int) {
        for (recordType in EVENT_RECORD_TYPES) {
            val count = chunkTypeCounts[recordType]
            if (count > 0) quality.addRejected(recordType, reason, count)
        }
        resetChunkState()
    }

    @Synchronized
    override fun close() {
        close(Jhlog.SEGMENT_END_NORMAL)
    }

    private data class ContextIds(
        val screenId: Long,
        val ownerId: Long,
        val flowId: Long,
        val stepId: Long,
        val stableOwnerId: Long = 0L,
        val hasStableOwner: Boolean = false,
    )

    private data class EncodedChunk(
        val raw: ByteArray,
        val dictionaryRecords: Int,
    )

    private class ChunkDictionaryUsage(
        private val enabled: Boolean,
    ) {
        private val pendingLocal = LongSetBuffer(16)
        private val chunkLocal = LongSetBuffer(128)
        private val pendingStable = LongSetBuffer(8)
        private val chunkStable = LongSetBuffer(64)

        private var pendingDefinitionBytes = 0
        private var chunkDefinitionBytes = 0

        fun markLocal(id: Long, encodedBytes: Int) {
            if (enabled && id > 0L && pendingLocal.add(id, encodedBytes)) {
                pendingDefinitionBytes += encodedBytes
            }
        }

        fun markStable(id: Long, encodedBytes: Int) {
            if (enabled && pendingStable.add(id, encodedBytes)) {
                pendingDefinitionBytes += encodedBytes
            }
        }

        fun commitPending() {
            if (!enabled) return
            pendingLocal.forEach { value, encodedBytes ->
                if (chunkLocal.add(value, encodedBytes)) chunkDefinitionBytes += encodedBytes
            }
            pendingStable.forEach { value, encodedBytes ->
                if (chunkStable.add(value, encodedBytes)) chunkDefinitionBytes += encodedBytes
            }
            pendingLocal.clear()
            pendingStable.clear()
            pendingDefinitionBytes = 0
        }

        fun projectedDefinitionBytes(): Int = if (enabled) chunkDefinitionBytes + pendingDefinitionBytes else 0

        fun chunkLocalCount(): Int = if (enabled) chunkLocal.size else 0

        fun chunkStableCount(): Int = if (enabled) chunkStable.size else 0

        inline fun forEachChunkLocal(action: (Long) -> Unit) {
            chunkLocal.forEach { value, _ -> action(value) }
        }

        inline fun forEachChunkStable(action: (Long) -> Unit) {
            chunkStable.forEach { value, _ -> action(value) }
        }

        fun clearChunk() {
            if (!enabled) return
            chunkLocal.clear()
            chunkStable.clear()
            chunkDefinitionBytes = 0
        }
    }

    private class LongSetBuffer(initialCapacity: Int) {
        private var keys = LongArray(tableCapacity(initialCapacity))
        private var generations = IntArray(keys.size)
        private var values = LongArray(initialCapacity.coerceAtLeast(1))
        private var encodedSizes = IntArray(values.size)
        private var generation = 1
        var size = 0
            private set

        fun add(value: Long, encodedBytes: Int): Boolean {
            if (size * 2 >= keys.size) growTable()
            var index = hash(value) and (keys.size - 1)
            while (generations[index] == generation) {
                if (keys[index] == value) return false
                index = (index + 1) and (keys.size - 1)
            }
            generations[index] = generation
            keys[index] = value
            if (size == values.size) {
                values = values.copyOf(values.size * 2)
                encodedSizes = encodedSizes.copyOf(values.size)
            }
            values[size] = value
            encodedSizes[size] = encodedBytes
            size++
            return true
        }

        inline fun forEach(action: (Long, Int) -> Unit) {
            for (index in 0 until size) action(values[index], encodedSizes[index])
        }

        fun clear() {
            size = 0
            if (generation == Int.MAX_VALUE) {
                generations.fill(0)
                generation = 1
            } else {
                generation++
            }
        }

        private fun growTable() {
            keys = LongArray(keys.size * 2)
            generations = IntArray(keys.size)
            generation = 1
            for (index in 0 until size) insertExisting(values[index])
        }

        private fun insertExisting(value: Long) {
            var index = hash(value) and (keys.size - 1)
            while (generations[index] == generation) index = (index + 1) and (keys.size - 1)
            generations[index] = generation
            keys[index] = value
        }

        private fun hash(value: Long): Int {
            var mixed = value
            mixed = (mixed xor (mixed ushr 33)) * -49064778989728563L
            mixed = (mixed xor (mixed ushr 33)) * -4265267296055464877L
            return (mixed xor (mixed ushr 33)).toInt()
        }

        private companion object {
            fun tableCapacity(requested: Int): Int {
                var capacity = 2
                val minimum = requested.coerceAtLeast(1) * 2
                while (capacity < minimum) capacity = capacity shl 1
                return capacity
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
        private const val TERMINAL_RESERVE_BYTES = 8L * 1024L
        private const val DEFAULT_LOCAL_FILE_LIMIT_BYTES = 16L * 1024L * 1024L
        private const val MAX_HEADER_STRING_BYTES = 1024
        private const val PROCESS_SCOPE_FINGERPRINT_BYTES = 32
        private const val SEGMENT_DIGEST_BYTES = 32
        private const val PROCESS_ROSTER_FINGERPRINT_BYTES = 32
        private const val MAX_ENCODED_DICTIONARY_VALUE_BYTES = Jhlog.MAX_RAW_CHUNK_BYTES - 1024
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
        private const val DICT_PROCESS = 10
        private const val DICT_FLOW = 11
        private const val DICT_STEP = 12
        private const val DICT_LOG_SOURCE = 13
        private const val DICT_STABLE_SYMBOL = 14

        private val EVENT_RECORD_TYPES = intArrayOf(
            *IntArray(Jhlog.TYPE_GAUGE - Jhlog.TYPE_SESSION + 1) { Jhlog.TYPE_SESSION + it },
            *IntArray(Jhlog.TYPE_RUNTIME_CALL - Jhlog.TYPE_LOG_SPAM + 1) { Jhlog.TYPE_LOG_SPAM + it },
            Jhlog.TYPE_PROCESS_EXIT,
            Jhlog.TYPE_IO,
        )

        private fun defaultFileHeader(): BinaryLogFileHeader {
            val elapsedUs = nowElapsedUs()
            return BinaryLogFileHeader(
                runId = BinaryLogFileHeader.randomId(),
                processInstanceId = BinaryLogFileHeader.randomId(),
                sessionId = BinaryLogFileHeader.randomId(),
                segmentIndex = 0L,
                osPid = Process.myPid().toLong().coerceAtLeast(0L),
                collectorStartElapsedUs = elapsedUs,
                segmentStartElapsedUs = elapsedUs,
                segmentStartUnixMs = System.currentTimeMillis().coerceAtLeast(0L),
                identitySource = 0L,
                processName = "unknown",
                symbolNamespace = ByteArray(0),
                expectedProcessFingerprint = processRosterFingerprint(setOf("unknown")),
            )
        }

        private fun processRosterFingerprint(processes: Set<String>): ByteArray {
            val digest = MessageDigest.getInstance("SHA-256")
            val length = ByteArray(Int.SIZE_BYTES)
            processes.sorted().forEach { processName ->
                val bytes = processName.toByteArray(StandardCharsets.UTF_8)
                length[0] = (bytes.size ushr 24).toByte()
                length[1] = (bytes.size ushr 16).toByte()
                length[2] = (bytes.size ushr 8).toByte()
                length[3] = bytes.size.toByte()
                digest.update(length)
                digest.update(bytes)
            }
            return digest.digest()
        }

        private fun nonNegative(value: Long): Long = value.coerceAtLeast(0L)

        private fun saturatingSum(values: LongArray): Long {
            var total = 0L
            for (value in values) {
                val safe = nonNegative(value)
                total = if (Long.MAX_VALUE - total < safe) Long.MAX_VALUE else total + safe
            }
            return total
        }

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

        private fun writeUvarint(out: ByteArrayOutputStream, rawValue: Long) {
            // Long carries unsigned varint bits here. Semantic unsigned fields are sanitized by
            // their callers; keeping the sign bit is required for zig-zag encoded Long.MIN_VALUE.
            var value = rawValue
            while (value and 0x7fL.inv() != 0L) {
                out.write(((value and 0x7fL) or 0x80L).toInt())
                value = value ushr 7
            }
            out.write(value.toInt())
        }

    }
}
