package io.jankhunter.runtime.internal.io

import android.os.Process
import android.os.SystemClock
import io.jankhunter.runtime.JankHunterHttpEvent
import io.jankhunter.runtime.JankHunterBinaryWriter
import io.jankhunter.runtime.JankHunterOperationAttributes
import io.jankhunter.runtime.JankHunterWebSocketEvent
import io.jankhunter.runtime.internal.saturatingAdd
import java.io.Closeable
import java.io.File
import java.io.IOException
import java.nio.charset.StandardCharsets
import java.security.MessageDigest
import java.util.TimeZone

private typealias Payload = BinaryPayload

internal class LogSizeLimitReachedException(message: String) : IOException(message)

internal class BinaryLogWriter private constructor(
    private val container: BinaryLogContainer,
    maxDictionaryEntries: Int,
    maxDictionaryValueBytes: Int,
    private val fileHeader: BinaryLogFileHeader,
    private val quality: LogQualityCounters,
    private val logGrowth: LogGrowthSessionBinding?,
) : Closeable, BinaryEncodingSink {
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
    private val dictionaryLookupResult = DictionaryLookupResult()
    private val stableSymbolDefinitions = StableSymbolRegistry()
    private val rawChunk = ReusableByteArrayOutput(Jhlog.TARGET_RAW_CHUNK_BYTES)
    private val gzipEncoder = ReusableGzipEncoder()
    private val eventPayload = BinaryPayload(256)
    private val dictionaryPayload = BinaryPayload(128)
    private val controlPayload = BinaryPayload(256)
    private val recordEncoder = BinaryRecordEncoder(fileHeader.segmentStartElapsedUs)
    private val recordContext = BinaryRecordContext()
    private val chunkTypeCounts = LongArray(Jhlog.TYPE_BINDER_TRANSACTION + 1)
    private var chunkRecordCount = 0
    private var chunkSequence = 0L
    private val producerOverride = ProducerMetadataBuffer()
    private val directProducer = ProducerMetadataBuffer()
    private val databaseRecords = DatabaseBinaryRecordEncoder(this)
    private val networkRecords = NetworkBinaryRecordEncoder(this)
    private val androidComponentRecords = AndroidComponentBinaryRecordEncoder(this)
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
            runCatching { gzipEncoder.close() }
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
        val payload = eventPayload.clear()
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
        val payload = eventPayload.clear()
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
    fun http(owner: String?, route: String?, event: JankHunterHttpEvent, flags: Long) {
        networkRecords.http(owner, route, event, flags)
    }

    @Synchronized
    fun webSocket(owner: String?, event: JankHunterWebSocketEvent) {
        networkRecords.webSocket(owner, event)
    }

    @Synchronized
    fun database(
        sourceId: Long,
        sourceName: String?,
        query: String?,
        framework: Long,
        operation: Long,
        outcome: Long,
        durationUs: Long,
        mainThread: Boolean,
        failureKind: Long = if (outcome == Jhlog.DATABASE_OUTCOME_FAILURE) Jhlog.DATABASE_FAILURE_OTHER else Jhlog.DATABASE_FAILURE_NONE,
        boundary: Long = Jhlog.DATABASE_BOUNDARY_EXECUTE,
        statementFingerprint: Long = 0L,
        resultKnown: Boolean = false,
        resultKind: Long = Jhlog.DATABASE_RESULT_UNKNOWN,
        resultCountBucket: Long = Jhlog.DATABASE_COUNT_UNKNOWN,
        transactionId: Long = 0L,
        statementToken: Long = 0L,
        phaseMask: Long = 0L,
        poolWaitUs: Long = 0L,
        lockWaitUs: Long = 0L,
        executeUs: Long = 0L,
        materializeUs: Long = 0L,
    ) {
        databaseRecords.database(
            sourceId,
            sourceName,
            query,
            framework,
            operation,
            outcome,
            durationUs,
            mainThread,
            failureKind,
            boundary,
            statementFingerprint,
            resultKnown,
            resultKind,
            resultCountBucket,
            transactionId,
            statementToken,
            phaseMask,
            poolWaitUs,
            lockWaitUs,
            executeUs,
            materializeUs,
        )
    }

    @Synchronized
    fun databaseTransaction(
        sourceId: Long,
        sourceName: String?,
        transactionId: Long,
        parentId: Long,
        stage: Long,
        mode: Long,
        outcome: Long,
        failureKind: Long,
        durationUs: Long,
        statementCount: Long,
        readCount: Long,
        writeCount: Long,
        mainThread: Boolean,
    ) {
        databaseRecords.databaseTransaction(
            sourceId,
            sourceName,
            transactionId,
            parentId,
            stage,
            mode,
            outcome,
            failureKind,
            durationUs,
            statementCount,
            readCount,
            writeCount,
            mainThread,
        )
    }

    @Synchronized
    fun processState(
        uiVisibility: Long,
        processImportance: Long,
        androidImportance: Long,
        reason: Long,
    ) {
        androidComponentRecords.processState(uiVisibility, processImportance, androidImportance, reason)
    }

    @Synchronized
    fun androidComponent(
        componentId: Long,
        componentName: String?,
        action: String?,
        instanceId: Long,
        flowId: Long,
        kind: Long,
        stage: Long,
        outcome: Long,
        durationUs: Long,
        componentFlags: Long,
    ) {
        androidComponentRecords.androidComponent(
            componentId,
            componentName,
            action,
            instanceId,
            flowId,
            kind,
            stage,
            outcome,
            durationUs,
            componentFlags,
        )
    }

    @Synchronized
    fun binderTransaction(
        descriptor: String?,
        method: String?,
        callId: Long,
        direction: Long,
        transactionCode: Long,
        outcome: Long,
        failureKind: Long,
        durationUs: Long,
        binderFlags: Long,
        mainThread: Boolean,
    ) {
        androidComponentRecords.binderTransaction(
            descriptor,
            method,
            callId,
            direction,
            transactionCode,
            outcome,
            failureKind,
            durationUs,
            binderFlags,
            mainThread,
        )
    }

    @Synchronized
    fun stall(
        screen: String?,
        owner: String?,
        stackHint: String?,
        durationMs: Long,
        foreground: Boolean = true,
    ) {
        val payload = eventPayload.clear()
            .symbolRef(idFor(DICT_STACK, stackHint))
            .uvarint(nonNegative(durationMs))
        val context = contextIds(screen, owner)
        record(Jhlog.TYPE_STALL, FLAG_THREAD_MAIN or foregroundFlag(foreground), payload, context)
    }

    @Synchronized
    fun memory(pssKb: Long, javaHeapKb: Long, nativeHeapKb: Long, foreground: Boolean = true) {
        val payload = eventPayload.clear()
            .uvarint(nonNegative(pssKb))
            .uvarint(nonNegative(javaHeapKb))
            .uvarint(nonNegative(nativeHeapKb))
        record(Jhlog.TYPE_MEMORY, foregroundFlag(foreground), payload, currentProducerContext())
    }

    @Synchronized
    fun retained(
        screen: String?,
        owner: String?,
        className: String?,
        holder: String?,
        ageMs: Long,
        count: Long,
        foreground: Boolean = true,
        evidence: Long,
    ) {
        val payload = eventPayload.clear()
            .symbolRef(idFor(DICT_CLASS, className))
            .symbolRef(idFor(DICT_OWNER, holder))
            .uvarint(nonNegative(ageMs))
            .uvarint(nonNegative(count))
            .uvarint(evidence.coerceIn(RETAINED_EVIDENCE_TIME_ONLY, RETAINED_EVIDENCE_AFTER_EXPLICIT_GC))
        record(
            Jhlog.TYPE_RETAINED,
            foregroundFlag(foreground),
            payload,
            contextIds(screen, owner),
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
        val payload = eventPayload.clear()
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
        val payload = eventPayload.clear()
            .uvarint(nonNegative(reason))
            .uvarint(timestampUnixMs)
            .uvarint(nonNegative(importance))
            .uvarint(nonNegative(pssKb))
            .uvarint(nonNegative(rssKb))
            .symbolRef(optionalIdFor(DICT_PROCESS, processName))
        record(Jhlog.TYPE_PROCESS_EXIT, 0L, payload, currentProducerContext())
    }

    @Synchronized
    fun io(
        operation: Long,
        durationUs: Long,
        bytes: Long,
        mainThread: Boolean,
        sourceId: Long,
        sourceName: String?,
        outcome: Long,
        bytesKnown: Boolean,
    ) {
        require(
            operation in Jhlog.IO_FILE_READ..Jhlog.IO_FILE_SYNC ||
                operation in Jhlog.IO_CONTENT_READ..Jhlog.IO_CONTENT_WRITE,
        )
        require(outcome in Jhlog.IO_OUTCOME_SUCCESS..Jhlog.IO_OUTCOME_FAILURE)
        require(bytesKnown || bytes == 0L)
        if (sourceId != 0L) ensureStableSymbolDefinition(sourceId, sourceName)
        val payload = eventPayload.clear()
        if (sourceId != 0L) payload.stableSymbolRef(sourceId) else payload.symbolRef(0L)
        payload
            .uvarint(operation)
            .uvarint(outcome)
            .uvarint(nonNegative(durationUs))
            .uvarint(nonNegative(bytes))
        var flags = if (mainThread) FLAG_THREAD_MAIN else 0L
        if (bytesKnown) flags = flags or Jhlog.FLAG_IO_BYTES_KNOWN
        record(Jhlog.TYPE_IO, flags, payload, currentProducerContext())
    }

    @Synchronized
    fun worker(
        workerId: Long,
        workerName: String?,
        instanceId: Long,
        stage: Long,
        outcome: Long,
        durationMs: Long,
        runAttempt: Long,
        generation: Long,
        stopReason: Long,
        flags: Long,
    ) {
        require(instanceId != 0L)
        require(stage in Jhlog.WORKER_STAGE_ENQUEUED..Jhlog.WORKER_STAGE_FINISHED)
        require(stage == Jhlog.WORKER_STAGE_ENQUEUED || workerId != 0L)
        require(outcome in Jhlog.WORKER_OUTCOME_UNKNOWN..Jhlog.WORKER_OUTCOME_CANCELLED)
        val finished = stage == Jhlog.WORKER_STAGE_FINISHED
        require(finished || outcome == Jhlog.WORKER_OUTCOME_UNKNOWN)
        require(finished || durationMs == 0L)
        require(!finished || outcome != Jhlog.WORKER_OUTCOME_UNKNOWN)
        require(runAttempt in 0L..MAX_UINT32)
        require(generation in 0L..MAX_UINT32)
        require(stopReason in 0L..MAX_UINT32)
        val stopReasonKnown = flags and Jhlog.FLAG_WORKER_STOP_REASON_KNOWN != 0L
        require(finished || !stopReasonKnown)
        require(stopReasonKnown || stopReason == 0L)
        if (workerId != 0L) ensureStableSymbolDefinition(workerId, workerName)
        val payload = eventPayload.clear()
        if (workerId != 0L) payload.stableSymbolRef(workerId) else payload.symbolRef(0L)
        payload
            .uvarint(instanceId)
            .uvarint(stage)
            .uvarint(outcome)
            .uvarint(nonNegative(durationMs))
            .uvarint(runAttempt)
            .uvarint(generation)
            .uvarint(stopReason)
        record(Jhlog.TYPE_WORKER, flags and FLAG_KNOWN_MASK, payload, currentProducerContext())
    }

    @Synchronized
    fun operation(
        name: String,
        operationId: Long,
        parentId: Long,
        phase: Long,
        kind: Long,
        outcome: Long,
        durationUs: Long,
        budgetUs: Long,
        attributes: JankHunterOperationAttributes,
    ) {
        require(operationId > 0L) { "Operation ID must be positive" }
        require(parentId >= 0L && parentId != operationId) { "Operation parent must be different" }
        require(kind in 1L..5L) { "Unsupported operation kind $kind" }
        when (phase) {
            Jhlog.OPERATION_PHASE_STARTED -> require(outcome == 0L && durationUs == 0L) {
                "Started operation cannot have outcome or duration"
            }
            Jhlog.OPERATION_PHASE_FINISHED -> require(outcome in 1L..4L) {
                "Finished operation requires a supported outcome"
            }
            else -> error("Unsupported operation phase $phase")
        }
        require(attributes.size <= Jhlog.OPERATION_MAX_ATTRIBUTES)
        val payload = eventPayload.clear()
            .symbolRef(idFor(DICT_OPERATION, name))
            .uvarint(operationId)
            .uvarint(parentId)
            .uvarint(phase)
            .uvarint(kind)
            .uvarint(outcome)
            .uvarint(nonNegative(durationUs))
            .uvarint(nonNegative(budgetUs))
            .uvarint(attributes.size.toLong())
        for (index in 0 until attributes.size) {
            payload
                .symbolRef(idFor(DICT_ATTRIBUTE_KEY, attributes.key(index)))
                .symbolRef(idFor(DICT_ATTRIBUTE_VALUE, attributes.value(index)))
        }
        record(Jhlog.TYPE_OPERATION, 0L, payload, currentProducerContext())
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
    fun stableCounter(metricId: Long, metricName: String, value: Long) {
        if (value < 0L) {
            quality.add(QualityCounterId.INVALID_METRIC)
            return
        }
        ensureStableSymbolDefinition(metricId, metricName)
        val payload = eventPayload.clear()
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
        operationId: Long,
        source: String?,
        level: Int,
        count: Long,
    ) {
        val payload = eventPayload.clear()
            .symbolRef(idFor(DICT_LOG_SOURCE, source))
            .uvarint(nonNegative(level.toLong()))
            .uvarint(nonNegative(count))
        record(
            Jhlog.TYPE_LOG_SPAM,
            0L,
            payload,
            contextIds(screen, owner).also { context -> context.operationId = nonNegative(operationId) },
        )
    }

    @Synchronized
    fun problemWindow(
        screen: String?,
        owner: String?,
        kind: String?,
        windowMs: Long,
        count: Long,
        maxMs: Long,
        foreground: Boolean = true,
    ) {
        val payload = eventPayload.clear()
            .symbolRef(idFor(DICT_METRIC, kind))
            .uvarint(nonNegative(windowMs).coerceAtLeast(1L))
            .uvarint(nonNegative(count))
            .uvarint(nonNegative(maxMs))
        record(
            Jhlog.TYPE_PROBLEM,
            foregroundFlag(foreground),
            payload,
            contextIds(screen, owner),
        )
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
        val payload = eventPayload.clear().uvarint(batch.size.toLong())
        for (index in 0 until batch.size) payload.symbolRef(optionalIdFor(DICT_SCREEN, batch.screen(index)))
        for (index in 0 until batch.size) payload.stableSymbolRef(batch.callerId(index))
        for (index in 0 until batch.size) payload.uvarint(nonNegative(batch.operationId(index)))
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
        val payload = eventPayload.clear()
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
        dictionary.resolve(kind, rawValue, dictionaryLookupResult)
        val result = dictionaryLookupResult
        if (result.overflowed) quality.add(QualityCounterId.DICTIONARY_OVERFLOW_TOTAL)
        if (result.truncated) quality.add(QualityCounterId.DICTIONARY_VALUE_TRUNCATED_TOTAL)
        val definition = result.definition
        if (definition != null) writeDictionaryDefinition(definition)
        return result.id
    }

    private fun writeDictionaryDefinition(definition: DictionaryIds.Definition) {
        val bytes = definition.value.toByteArray(StandardCharsets.UTF_8)
        val payload = dictionaryPayload.clear()
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
        val existing = stableSymbolDefinitions.get(stableId)
        if (existing != null) {
            require(existing == rawName) {
                "stable symbol $stableId changed from '$existing' to '$rawName'"
            }
            return
        }
        val name = requireNotNull(rawName?.takeIf(String::isNotBlank)) {
            "stable symbol $stableId requires a readable embedded name"
        }

        val encoded = name.toByteArray(StandardCharsets.UTF_8)
        val bytes = if (encoded.size <= MAX_ENCODED_DICTIONARY_VALUE_BYTES) {
            encoded
        } else {
            quality.add(QualityCounterId.DICTIONARY_VALUE_TRUNCATED_TOTAL)
            validUtf8Prefix(name, MAX_ENCODED_DICTIONARY_VALUE_BYTES)
        }
        stableSymbolDefinitions.put(stableId, name)
        val payload = dictionaryPayload.clear()
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

    override fun payload(): BinaryPayload = eventPayload.clear()

    override fun optionalSymbolId(kind: Int, value: String?): Long = optionalIdFor(kind, value)

    override fun defineStableSymbol(id: Long, name: String?) = ensureStableSymbolDefinition(id, name)

    override fun producerContext(owner: String?): BinaryRecordContext? = currentProducerContext().withOwner(owner)

    override fun emit(
        recordType: Int,
        attributes: Long,
        payload: BinaryPayload,
        context: BinaryRecordContext?,
        semanticEventCount: Long,
    ) = record(recordType, attributes, payload, context, semanticEventCount = semanticEventCount)

    private fun contextIds(screen: String?, owner: String?): BinaryRecordContext {
        return recordContext.set(
            screenId = optionalIdFor(DICT_SCREEN, screen),
            ownerId = optionalIdFor(DICT_OWNER, owner),
        )
    }

    private fun currentProducerContext(): BinaryRecordContext? {
        if (!producerOverrideActive) return null
        val context = producerOverride.context ?: return null
        return contextIds(context.screen, context.owner).also { resolved ->
            resolved.operationId = context.operationId
        }
    }

    private fun BinaryRecordContext?.withOwner(owner: String?): BinaryRecordContext? {
        val ownerId = optionalIdFor(DICT_OWNER, owner)
        if (ownerId == 0L) return this
        val context = this ?: recordContext.clear()
        context.ownerId = ownerId
        context.stableOwnerId = 0L
        context.hasStableOwner = false
        return context
    }

    private fun BinaryRecordContext?.withStableOwner(ownerId: Long): BinaryRecordContext {
        val context = this ?: recordContext.clear()
        context.ownerId = 0L
        context.stableOwnerId = ownerId
        context.hasStableOwner = true
        return context
    }

    private fun BinaryRecordContext?.withScreen(screen: String?): BinaryRecordContext? {
        val screenId = optionalIdFor(DICT_SCREEN, screen)
        if (screenId == 0L) return this
        val context = this ?: recordContext.clear()
        context.screenId = screenId
        return context
    }

    private fun record(
        recordType: Int,
        attributes: Long,
        payload: Payload,
        context: BinaryRecordContext?,
        producer: ProducerMetadataBuffer? = currentProducer(),
        semanticEventCount: Long = 1L,
    ) {
        ensureWritable()
        val safeAttributes = attributes and FLAG_KNOWN_MASK
        var encoded = recordEncoder.encode(recordType, safeAttributes, payload, context, producer)
        val chunkTarget = if (terminalChunkBuilding) {
            Jhlog.MAX_RAW_CHUNK_BYTES
        } else {
            Jhlog.TARGET_RAW_CHUNK_BYTES
        }
        val projectedRawBytes = rawChunk.size().toLong() + encoded.size.toLong()
        if (
            rawChunk.size() > 0 &&
            (rawChunk.size() + encoded.size > chunkTarget || projectedRawBytes > chunkTarget)
        ) {
            commitChunk(final = false)
            encoded = recordEncoder.encode(recordType, safeAttributes, payload, context, producer)
        }
        var rawBytes = rawChunk.size().toLong() + encoded.size.toLong()
        val reservedBytes = if (terminalChunkBuilding) 0L else TERMINAL_RESERVE_BYTES
        if (!container.canCommitRaw(rawBytes.coerceAtMost(Int.MAX_VALUE.toLong()).toInt(), reservedBytes)) {
            if (rawChunk.size() > 0) {
                commitChunk(final = false)
                encoded = recordEncoder.encode(recordType, safeAttributes, payload, context, producer)
                rawBytes = encoded.size.toLong()
            }
            if (!container.canCommitRaw(rawBytes.coerceAtMost(Int.MAX_VALUE.toLong()).toInt(), reservedBytes)) {
                throw LogSizeLimitReachedException(
                    "JHLOG ${Jhlog.FORMAT_VERSION} segment has no capacity for record type $recordType",
                )
            }
        }
        if (encoded.size > Jhlog.MAX_RAW_CHUNK_BYTES) {
            if (recordType == Jhlog.TYPE_DICTIONARY) {
                throw IOException("JHLOG ${Jhlog.FORMAT_VERSION} dictionary definition exceeds raw chunk limit")
            }
            quality.addRejected(recordType, QualityCounterId.REASON_OVERSIZED, semanticEventCount)
            return
        }
        encoded.writeTo(rawChunk)
        chunkRecordCount++
        if (recordType in chunkTypeCounts.indices) {
            chunkTypeCounts[recordType] = saturatingAdd(chunkTypeCounts[recordType], semanticEventCount)
        }
        recordEncoder.commit(producer, context)
    }

    private fun currentProducer(): ProducerMetadataBuffer {
        return if (producerOverrideActive) producerOverride else directProducer.capture(null)
    }

    private fun writeQualitySnapshot() {
        val generation = quality.generation()
        val sequence = quality.nextSnapshotSequence()
        val entries = quality.snapshot()
        val payload = controlPayload.clear()
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
        val payload = controlPayload.clear()
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
        val rawSize = rawChunk.length
        gzipEncoder.encode(rawChunk.buffer, rawSize)
        val flags = Jhlog.CHUNK_FLAG_GZIP or if (final) Jhlog.CHUNK_FLAG_FINAL else 0
        try {
            container.commitChunk(
                flags = flags,
                sequence = chunkSequence,
                stored = gzipEncoder.buffer,
                storedSize = gzipEncoder.size,
                rawSize = rawSize,
                recordCount = chunkRecordCount,
                rawCrc = gzipEncoder.rawCrc,
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
        val dictionaryCount = chunkTypeCounts[Jhlog.TYPE_DICTIONARY]
        segmentDictionaryRecords = saturatingAdd(segmentDictionaryRecords, dictionaryCount)
        if (!final && (eventCount > 0L || dictionaryCount > 0L)) commitQualityPending = true
        chunkSequence++
        resetChunkState()
    }

    private fun resetChunkState() {
        rawChunk.reset()
        chunkTypeCounts.fill(0)
        chunkRecordCount = 0
        recordEncoder.reset()
    }

    private fun writeFileHeader() {
        validateProcessScope()
        val payload = controlPayload.clear()
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
            .svarint(fileHeader.timezoneOffsetMinutes.coerceIn(-MAX_TIMEZONE_OFFSET_MINUTES, MAX_TIMEZONE_OFFSET_MINUTES))
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
                "Unsupported JHLOG ${Jhlog.FORMAT_VERSION} required feature contract " +
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
        val payload = controlPayload.clear()
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
        gzipEncoder.close()
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
            gzipEncoder.close()
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
            ((1L shl 14) - 1L) or FLAG_HTTP_SLOW or FLAG_UI_PROBLEM or FLAG_HTTP_CLASSIFIED or
                FLAG_UI_CLASSIFIED or Jhlog.FLAG_WORKER_PERIODIC or Jhlog.FLAG_WORKER_STOP_REASON_KNOWN or
                Jhlog.FLAG_IO_BYTES_KNOWN
        private const val TERMINAL_RESERVE_BYTES = 8L * 1024L
        private const val DEFAULT_LOCAL_FILE_LIMIT_BYTES = 16L * 1024L * 1024L
        private const val MAX_HEADER_STRING_BYTES = 1024
        private const val PROCESS_SCOPE_FINGERPRINT_BYTES = 32
        private const val SEGMENT_DIGEST_BYTES = 32
        private const val PROCESS_ROSTER_FINGERPRINT_BYTES = 32
        private const val MAX_ENCODED_DICTIONARY_VALUE_BYTES = Jhlog.MAX_RAW_CHUNK_BYTES - 1024
        private const val DICTIONARY_ENCODING_UTF8 = 0L

        internal const val DICT_GENERIC = 0
        private const val DICT_OWNER = 1
        internal const val DICT_ROUTE = 2
        private const val DICT_SCREEN = 3
        private const val DICT_CLASS = 4
        private const val DICT_STACK = 5
        private const val DICT_METRIC = 6
        private const val DICT_DEVICE = 7
        private const val DICT_APP_VERSION = 8
        private const val DICT_BUILD = 9
        private const val DICT_PROCESS = 10
        private const val DICT_LOG_SOURCE = 11
        private const val DICT_STABLE_SYMBOL = 12
        private const val DICT_OPERATION = 13
        private const val DICT_ATTRIBUTE_KEY = 14
        private const val DICT_ATTRIBUTE_VALUE = 15

        private const val MAX_TIMEZONE_OFFSET_MINUTES = 14L * 60L

        private const val MAX_UINT32 = 0xffff_ffffL

        private val EVENT_RECORD_TYPES = intArrayOf(
            *IntArray(Jhlog.TYPE_GAUGE - Jhlog.TYPE_SESSION + 1) { Jhlog.TYPE_SESSION + it },
            Jhlog.TYPE_OPERATION,
            *IntArray(Jhlog.TYPE_RUNTIME_CALL - Jhlog.TYPE_LOG_SPAM + 1) { Jhlog.TYPE_LOG_SPAM + it },
            Jhlog.TYPE_PROCESS_EXIT,
            Jhlog.TYPE_IO,
            Jhlog.TYPE_WORKER,
            Jhlog.TYPE_WEBSOCKET,
            Jhlog.TYPE_DATABASE,
            Jhlog.TYPE_DATABASE_TRANSACTION,
            Jhlog.TYPE_PROCESS_STATE,
            Jhlog.TYPE_ANDROID_COMPONENT,
            Jhlog.TYPE_BINDER_TRANSACTION,
        )

        private fun defaultFileHeader(): BinaryLogFileHeader {
            val elapsedUs = nowElapsedUs()
            val unixMs = System.currentTimeMillis().coerceAtLeast(0L)
            return BinaryLogFileHeader(
                runId = BinaryLogFileHeader.randomId(),
                processInstanceId = BinaryLogFileHeader.randomId(),
                sessionId = BinaryLogFileHeader.randomId(),
                segmentIndex = 0L,
                osPid = Process.myPid().toLong().coerceAtLeast(0L),
                collectorStartElapsedUs = elapsedUs,
                segmentStartElapsedUs = elapsedUs,
                segmentStartUnixMs = unixMs,
                timezoneOffsetMinutes = TimeZone.getDefault().getOffset(unixMs) / 60_000L,
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

        internal fun nonNegative(value: Long): Long = value.coerceAtLeast(0L)

        private fun saturatingSum(values: LongArray): Long {
            var total = 0L
            for (value in values) {
                val safe = nonNegative(value)
                total = if (Long.MAX_VALUE - total < safe) Long.MAX_VALUE else total + safe
            }
            return total
        }

        private fun foregroundFlag(foreground: Boolean): Long = if (foreground) FLAG_APP_FOREGROUND else 0L


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

    }
}
