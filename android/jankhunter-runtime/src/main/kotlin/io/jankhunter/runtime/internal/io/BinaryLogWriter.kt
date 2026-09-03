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

/** Mutable encoder owned exclusively by one writer thread after construction. */
internal class BinaryLogWriter private constructor(
    private val container: BinaryLogContainer,
    maxDictionaryEntries: Int,
    maxDictionaryValueBytes: Int,
    private val fileHeader: BinaryLogFileHeader,
    private val quality: LogQualityCounters,
    logGrowth: LogGrowthSessionBinding?,
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

    private val dictionaryRecords = BinaryDictionaryEncoder(
        sink = this,
        quality = quality,
        maxDictionaryEntries = maxDictionaryEntries,
        maxDictionaryValueBytes = maxDictionaryValueBytes,
    )
    private val logGrowthRecords = BinaryLogGrowthEncoder(this, container, quality, logGrowth)
    private val rawChunk = ReusableByteArrayOutput(Jhlog.TARGET_RAW_CHUNK_BYTES)
    private val gzipEncoder = ReusableGzipEncoder()
    private val eventPayload = BinaryPayload(256)
    private val controlPayload = BinaryPayload(256)
    private val recordEncoder = BinaryRecordEncoder(fileHeader.segmentStartElapsedUs)
    private val recordContext = BinaryRecordContext()
    private val microPage = ColumnarMicroPage()
    private val microPagePayload = BinaryPayload(Jhlog.TARGET_RAW_CHUNK_BYTES)
    private val chunkTypeCounts = LongArray(Jhlog.TYPE_BINDER_TRANSACTION + 1)
    private var chunkRecordCount = 0
    private var chunkSequence = 0L
    private val producerOverride = ProducerMetadataBuffer()
    private val directProducer = ProducerMetadataBuffer()
    private val databaseRecords = DatabaseBinaryRecordEncoder(this)
    private val networkRecords = NetworkBinaryRecordEncoder(this)
    private val androidComponentRecords = AndroidComponentBinaryRecordEncoder(this)
    private val runtimeCallRecords = RuntimeCallBinaryRecordEncoder(this)
    private val sessionRecords = SessionBinaryRecordEncoder(this)
    private val executionRecords = ExecutionBinaryRecordEncoder(this)
    private val metricRecords = MetricBinaryRecordEncoder(this)
    private var qualityCounterIds = IntArray(INITIAL_QUALITY_SNAPSHOT_ENTRIES)
    private var qualitySnapshotValues = LongArray(INITIAL_QUALITY_SNAPSHOT_ENTRIES)
    private val qualityPreviousValues = LongArray(quality.counterCapacity())
    private var qualitySnapshotCount = 0
    private var pendingQualityCapturedUs = 0L
    private var lastQualityCapturedUs = 0L
    private var producerOverrideActive = false
    private var segmentEventRecords = 0L
    private var segmentDictionaryRecords = 0L
    private var lastQualityGeneration = -1L
    private var lastQualitySequence = 0L
    private var commitQualityPending = false
    private var terminalChunkBuilding = false
    private var microPageFlushing = false
    private var closed = false
    private var poisoned = false
    @Volatile
    private var storageBudgetExhausted = false
    private var sealedSegmentDigest: ByteArray? = null

    init {
        try {
            databaseRecords.resetSegmentState()
            writeFileHeader()
            logGrowthRecords.begin(fileHeader, storageBudgetExhausted)
        } catch (error: Throwable) {
            runCatching { gzipEncoder.close() }
            runCatching { container.close() }
            throw error
        }
    }

    fun bytesWritten(): Long = container.retainedBytes()

    internal fun logGrowthStats(): LogContainerStats = logGrowthRecords.stats(storageBudgetExhausted)

    fun flush() {
        ensureWritable()
        // Commit data first, then publish an exact snapshot in its own committed control chunk.
        commitChunk(final = false)
        commitQualityControlChunk()
        container.flush()
    }

    internal fun writeLogGrowthSummary(): Boolean {
        ensureWritable()
        commitChunk(final = false)
        commitQualityControlChunk()
        container.flush()
        if (!logGrowthRecords.enabled) return false
        return try {
            if (!logGrowthRecords.checkpoint(storageBudgetExhausted)) return false
            commitChunk(final = false)
            container.flush()
            true
        } catch (error: Throwable) {
            if (error is VirtualMachineError || error is ThreadDeath) throw error
            false
        }
    }

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
        sessionRecords.session(
            appVersion, build, device, sdkInt, androidRelease, securityPatch, primaryAbi,
            supportedAbis, manufacturer, brand, hardware, board, product, deviceRooted,
            collectorFlags, appForeground,
        )
    }

    fun screen(screen: String?) {
        symbolId(DICT_SCREEN, screen)
    }

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
        sessionRecords.deviceContext(
            networkKind, batteryPct, availMemoryKb, batteryState, batteryTempDeciC, lowMemory,
            networkMetered, networkValidated, rxBytes, txBytes, totalMemoryKb, freeStorageKb,
            totalStorageKb, networkVpn, foreground,
        )
    }

    fun http(owner: String?, route: String?, event: JankHunterHttpEvent, flags: Long) {
        networkRecords.http(owner, route, event, flags)
    }

    fun webSocket(owner: String?, event: JankHunterWebSocketEvent) {
        networkRecords.webSocket(owner, event)
    }

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

    fun processState(
        uiVisibility: Long,
        processImportance: Long,
        androidImportance: Long,
        reason: Long,
    ) {
        androidComponentRecords.processState(uiVisibility, processImportance, androidImportance, reason)
    }

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

    fun stall(
        screen: String?,
        owner: String?,
        stackHint: String?,
        durationMs: Long,
        foreground: Boolean = true,
    ) {
        sessionRecords.stall(screen, owner, stackHint, durationMs, foreground)
    }

    fun memory(pssKb: Long, javaHeapKb: Long, nativeHeapKb: Long, foreground: Boolean = true) {
        sessionRecords.memory(pssKb, javaHeapKb, nativeHeapKb, foreground)
    }

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
        sessionRecords.retained(screen, owner, className, holder, ageMs, count, foreground, evidence)
    }

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
        sessionRecords.uiWindow(
            screen, windowMs, frameCount, jankCount, source, frameDeadlineUs,
            frameDurationBuckets, foreground, flags,
        )
    }

    fun processExit(
        reason: Long,
        timestampUnixMs: Long,
        importance: Long,
        pssKb: Long,
        rssKb: Long,
        processName: String?,
    ) {
        sessionRecords.processExit(reason, timestampUnixMs, importance, pssKb, rssKb, processName)
    }

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
        executionRecords.io(operation, durationUs, bytes, mainThread, sourceId, sourceName, outcome, bytesKnown)
    }

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
        executionRecords.worker(
            workerId, workerName, instanceId, stage, outcome, durationMs, runAttempt,
            generation, stopReason, flags,
        )
    }

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
        executionRecords.operation(
            name, operationId, parentId, phase, kind, outcome, durationUs, budgetUs, attributes,
        )
    }

    fun counter(name: String?, value: Long) {
        metricRecords.counter(name, value)
    }

    fun stableCounter(metricId: Long, metricName: String, value: Long) {
        metricRecords.stableCounter(metricId, metricName, value)
    }

    fun gauge(name: String?, value: Long) {
        metricRecords.gauge(name, value)
    }

    fun gauge(
        name: String?,
        value: Long,
        count: Long,
        sum: Long,
        max: Long,
        mode: MetricAggregationMode,
    ) {
        metricRecords.gauge(name, value, count, sum, max, mode)
    }

    fun logSpam(
        screen: String?,
        owner: String?,
        operationId: Long,
        source: String?,
        level: Int,
        count: Long,
    ) {
        metricRecords.logSpam(screen, owner, operationId, source, level, count)
    }

    fun problemWindow(
        screen: String?,
        owner: String?,
        kind: String?,
        windowMs: Long,
        count: Long,
        maxMs: Long,
        foreground: Boolean = true,
    ) {
        metricRecords.problemWindow(screen, owner, kind, windowMs, count, maxMs, foreground)
    }

    /** Writes one bounded structure-of-arrays runtime-call record to the wire. */
    fun runtimeCalls(batch: RuntimeCallBatch) {
        runtimeCallRecords.runtimeCalls(batch)
    }

    override fun payload(): BinaryPayload = eventPayload.clear()

    override fun symbolId(kind: Int, value: String?): Long {
        ensureWritable()
        return dictionaryRecords.symbolId(kind, value)
    }

    override fun optionalSymbolId(kind: Int, value: String?): Long {
        val present = value?.takeIf { it.isNotEmpty() } ?: return 0L
        ensureWritable()
        return dictionaryRecords.symbolId(kind, present)
    }

    override fun defineStableSymbol(id: Long, name: String?): Long {
        ensureWritable()
        return dictionaryRecords.defineStableSymbol(id, name)
    }

    override fun producerContext(owner: String?): BinaryRecordContext? = currentProducerContext().withOwner(owner)

    override fun producerContextWithScreen(screen: String?): BinaryRecordContext? =
        currentProducerContext().withScreen(screen)

    override fun context(screen: String?, owner: String?, operationId: Long): BinaryRecordContext =
        contextIds(screen, owner).also { context -> context.operationId = nonNegative(operationId) }

    override fun recordInvalidMetric() {
        quality.add(QualityCounterId.INVALID_METRIC)
    }

    override fun emitDictionaryDefinition(payload: BinaryPayload) = recordDictionary(payload)

    override fun emitControl(recordType: Int, payload: BinaryPayload) = recordControl(recordType, payload)

    override fun emitSemantic(
        recordType: Int,
        attributes: Long,
        payload: BinaryPayload,
        context: BinaryRecordContext?,
        semanticEventCount: Long,
    ) = recordSemantic(recordType, attributes, payload, context, semanticEventCount)

    private fun contextIds(screen: String?, owner: String?): BinaryRecordContext {
        return recordContext.set(
            screenId = optionalSymbolId(DICT_SCREEN, screen),
            ownerId = optionalSymbolId(DICT_OWNER, owner),
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
        val ownerId = optionalSymbolId(DICT_OWNER, owner)
        if (ownerId == 0L) return this
        val context = this ?: recordContext.clear()
        context.ownerId = ownerId
        context.stableOwnerAlias = 0L
        context.hasStableOwner = false
        return context
    }

    private fun BinaryRecordContext?.withStableOwner(ownerAlias: Long): BinaryRecordContext {
        val context = this ?: recordContext.clear()
        context.ownerId = 0L
        context.stableOwnerAlias = ownerAlias
        context.hasStableOwner = true
        return context
    }

    private fun BinaryRecordContext?.withScreen(screen: String?): BinaryRecordContext? {
        val screenId = optionalSymbolId(DICT_SCREEN, screen)
        if (screenId == 0L) return this
        val context = this ?: recordContext.clear()
        context.screenId = screenId
        return context
    }

    private fun recordDictionary(payload: Payload) {
        ensureWritable()
        flushMicroPage()
        writeDirectRecord(
            recordType = Jhlog.TYPE_DICTIONARY,
            safeAttributes = 0L,
            payload = payload,
            context = null,
            producer = null,
            semanticEventCount = 1L,
        )
    }

    private fun recordControl(recordType: Int, payload: Payload) {
        require(isControlRecord(recordType)) { "Record type $recordType is not control state" }
        ensureWritable()
        flushMicroPage()
        writeDirectRecord(
            recordType = recordType,
            safeAttributes = 0L,
            payload = payload,
            context = null,
            producer = null,
            semanticEventCount = 0L,
        )
    }

    private fun recordSemantic(
        recordType: Int,
        attributes: Long,
        payload: Payload,
        context: BinaryRecordContext?,
        semanticEventCount: Long = 1L,
    ) {
        require(isSemanticRecord(recordType)) { "Record type $recordType is not semantic data" }
        ensureWritable()
        val safeAttributes = attributes and FLAG_KNOWN_MASK
        val producer = currentProducer()
        if (isSemanticRecord(recordType) && payload.size <= Jhlog.MAX_MICRO_PAGE_PAYLOAD_BYTES) {
            prepareMicroPage(payload.size)
            if (microPage.canAppend(payload.size)) {
                microPage.append(recordType, safeAttributes, payload, context, producer, semanticEventCount)
                if (microPage.size == Jhlog.MAX_MICRO_PAGE_ROWS) flushMicroPage()
                return
            }
        }
        flushMicroPage()
        writeDirectRecord(recordType, safeAttributes, payload, context, producer, semanticEventCount)
    }

    private fun prepareMicroPage(payloadSize: Int) {
        val chunkTarget = if (terminalChunkBuilding) Jhlog.MAX_RAW_CHUNK_BYTES else Jhlog.TARGET_RAW_CHUNK_BYTES
        val reservedBytes = if (terminalChunkBuilding) 0L else TERMINAL_RESERVE_BYTES
        if (microPage.size > 0) {
            val projectedPageBytes = microPage.projectedRecordBytes(payloadSize)
            val projectedChunkBytes = rawChunk.size().toLong() + projectedPageBytes.toLong()
            if (
                !microPage.canAppend(payloadSize) ||
                projectedChunkBytes > chunkTarget ||
                !container.canCommitRaw(projectedChunkBytes.coerceAtMost(Int.MAX_VALUE.toLong()).toInt(), reservedBytes)
            ) {
                flushMicroPage()
            }
        }
        if (microPage.size != 0) return

        val singlePageBytes = microPage.projectedRecordBytes(payloadSize)
        var projectedChunkBytes = rawChunk.size().toLong() + singlePageBytes.toLong()
        if (
            rawChunk.size() > 0 &&
            (
                projectedChunkBytes > chunkTarget ||
                    !container.canCommitRaw(
                        projectedChunkBytes.coerceAtMost(Int.MAX_VALUE.toLong()).toInt(),
                        reservedBytes,
                    )
                )
        ) {
            commitChunk(final = false)
            projectedChunkBytes = singlePageBytes.toLong()
        }
        if (
            !container.canCommitRaw(
                projectedChunkBytes.coerceAtMost(Int.MAX_VALUE.toLong()).toInt(),
                reservedBytes,
            )
        ) {
            throw LogSizeLimitReachedException(
                "JHLOG ${Jhlog.FORMAT_VERSION} segment has no capacity for a micro-page row",
            )
        }
    }

    private fun writeDirectRecord(
        recordType: Int,
        safeAttributes: Long,
        payload: Payload,
        context: BinaryRecordContext?,
        producer: ProducerMetadataBuffer?,
        semanticEventCount: Long,
    ) {
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

    private fun flushMicroPage() {
        if (microPage.size == 0 || microPageFlushing) return
        microPage.encodeTo(
            microPagePayload,
            entropyEnabled = JhlogCompressionPolicy.useRansSections(
                optionalFeatures = Jhlog.OPTIONAL_FEATURES,
                chunkFlags = Jhlog.CHUNK_FLAG_GZIP,
            ),
        )
        microPageFlushing = true
        try {
            writeDirectRecord(
                recordType = Jhlog.TYPE_MICRO_PAGE,
                safeAttributes = 0L,
                payload = microPagePayload,
                context = null,
                producer = null,
                semanticEventCount = 0L,
            )
        } finally {
            microPageFlushing = false
        }
        microPage.addSemanticCountsTo(chunkTypeCounts)
        microPage.reset()
    }

    private fun currentProducer(): ProducerMetadataBuffer {
        return if (producerOverrideActive) producerOverride else directProducer.capture(null)
    }

    private fun writeQualitySnapshot() {
        val generation = quality.generation()
        val sequence = quality.nextSnapshotSequence()
        val capturedUs = nowElapsedUs()
        var entries = quality.snapshotInto(qualityCounterIds, qualitySnapshotValues)
        while (entries < 0) {
            val required = maxOf(-entries, qualityCounterIds.size shl 1)
            qualityCounterIds = IntArray(required)
            qualitySnapshotValues = LongArray(required)
            entries = quality.snapshotInto(qualityCounterIds, qualitySnapshotValues)
        }
        var changedEntries = 0
        for (index in 0 until entries) {
            val counterId = qualityCounterIds[index]
            val value = qualitySnapshotValues[index]
            require(value >= qualityPreviousValues[counterId]) { "Quality counter $counterId regressed" }
            if (value != qualityPreviousValues[counterId]) changedEntries++
        }
        val payload = controlPayload.clear()
            .uvarint(sequence - lastQualitySequence)
            .svarint(capturedUs - lastQualityCapturedUs)
            .uvarint(changedEntries.toLong())
        var previousCounterId = 0
        for (index in 0 until entries) {
            val counterId = qualityCounterIds[index]
            val value = qualitySnapshotValues[index]
            val previousValue = qualityPreviousValues[counterId]
            if (value == previousValue) continue
            payload
                .uvarint((counterId - previousCounterId).toLong())
                .uvarint(value - previousValue)
            previousCounterId = counterId
        }
        recordControl(Jhlog.TYPE_QUALITY_SNAPSHOT, payload)
        lastQualityGeneration = generation
        lastQualitySequence = sequence
        qualitySnapshotCount = entries
        pendingQualityCapturedUs = capturedUs
        commitQualityPending = false
    }

    private fun commitQualityDeltaState() {
        for (index in 0 until qualitySnapshotCount) {
            qualityPreviousValues[qualityCounterIds[index]] = qualitySnapshotValues[index]
        }
        lastQualityCapturedUs = pendingQualityCapturedUs
        qualitySnapshotCount = 0
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
            commitQualityDeltaState()
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
        recordControl(Jhlog.TYPE_SEGMENT_END, payload)
    }

    private fun commitChunk(final: Boolean, countCommit: Boolean = true) {
        flushMicroPage()
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
        if (!microPageFlushing) microPage.reset()
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
            .boundedBytes(fileHeader.processScopeFingerprint, Jhlog.PROCESS_SCOPE_FINGERPRINT_BYTES)
            .boundedBytes(fileHeader.previousSegmentDigest, Jhlog.SEGMENT_DIGEST_BYTES)
            .uvarint(nonNegative(fileHeader.expectedProcessCount))
            .boundedBytes(fileHeader.expectedProcessFingerprint, Jhlog.PROCESS_ROSTER_FINGERPRINT_BYTES)
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
                fileHeader.processScopeFingerprint.size != Jhlog.PROCESS_SCOPE_FINGERPRINT_BYTES
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
        } else if (fileHeader.previousSegmentDigest.size != Jhlog.SEGMENT_DIGEST_BYTES) {
            throw IOException("Segment ${fileHeader.segmentIndex} must declare a full predecessor digest")
        }
        if (
            fileHeader.expectedProcessCount <= 0L ||
            fileHeader.expectedProcessFingerprint.size != Jhlog.PROCESS_ROSTER_FINGERPRINT_BYTES
        ) {
            throw IOException("Process roster must contain at least one process and a full fingerprint")
        }
    }

    private fun publishLogGrowthCompletion() {
        try {
            if (!logGrowthRecords.complete(storageBudgetExhausted)) return
            commitChunk(final = false)
        } catch (error: Throwable) {
            if (error is VirtualMachineError || error is ThreadDeath) throw error
        }
    }

    private fun ensureWritable() {
        if (closed || poisoned) throw IOException("BinaryLogWriter is not writable")
    }

    internal fun abort() {
        if (closed) return
        closed = true
        poisoned = true
        gzipEncoder.close()
        runCatching { container.close() }
    }

    internal fun sealSizeLimit() {
        if (closed) return
        if (poisoned) {
            abort()
            return
        }
        discardPendingEvents(QualityCounterId.REASON_SIZE_LIMIT)
        finishTerminal(Jhlog.SEGMENT_END_SIZE_LIMIT)
    }

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

    internal fun sealRotation() {
        if (closed) return
        ensureWritable()
        finishTerminal(Jhlog.SEGMENT_END_ROTATION, completeGrowth = false, freezeQuality = false)
    }

    internal fun sealedDigest(): ByteArray? = sealedSegmentDigest?.copyOf()

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
            commitQualityDeltaState()
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
        microPage.rejectSemanticCounts(quality, reason)
        resetChunkState()
    }

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
        private const val FLAG_KNOWN_MASK: Long =
            ((1L shl 14) - 1L) or FLAG_HTTP_SLOW or FLAG_UI_PROBLEM or FLAG_HTTP_CLASSIFIED or
                FLAG_UI_CLASSIFIED or Jhlog.FLAG_WORKER_PERIODIC or Jhlog.FLAG_WORKER_STOP_REASON_KNOWN or
                Jhlog.FLAG_IO_BYTES_KNOWN
        private const val TERMINAL_RESERVE_BYTES = 8L * 1024L
        private const val DEFAULT_LOCAL_FILE_LIMIT_BYTES = 16L * 1024L * 1024L
        private const val MAX_HEADER_STRING_BYTES = 1024
        private const val INITIAL_QUALITY_SNAPSHOT_ENTRIES = 256

        internal const val DICT_GENERIC = 0
        internal const val DICT_OWNER = 1
        internal const val DICT_ROUTE = 2
        internal const val DICT_SCREEN = 3
        internal const val DICT_CLASS = 4
        internal const val DICT_STACK = 5
        internal const val DICT_METRIC = 6
        internal const val DICT_DEVICE = 7
        internal const val DICT_APP_VERSION = 8
        internal const val DICT_BUILD = 9
        internal const val DICT_PROCESS = 10
        internal const val DICT_LOG_SOURCE = 11
        internal const val DICT_STABLE_SYMBOL = 12
        internal const val DICT_OPERATION = 13
        internal const val DICT_ATTRIBUTE_KEY = 14
        internal const val DICT_ATTRIBUTE_VALUE = 15

        private const val MAX_TIMEZONE_OFFSET_MINUTES = 14L * 60L

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

        private fun isSemanticRecord(recordType: Int): Boolean {
            return recordType in Jhlog.TYPE_SESSION..Jhlog.TYPE_GAUGE ||
                recordType == Jhlog.TYPE_OPERATION ||
                recordType in Jhlog.TYPE_LOG_SPAM..Jhlog.TYPE_RUNTIME_CALL ||
                recordType in Jhlog.TYPE_PROCESS_EXIT..Jhlog.TYPE_BINDER_TRANSACTION
        }

        private fun isControlRecord(recordType: Int): Boolean {
            return recordType == Jhlog.TYPE_QUALITY_SNAPSHOT ||
                recordType == Jhlog.TYPE_SEGMENT_END ||
                recordType == Jhlog.TYPE_LOG_GROWTH
        }

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

        private fun exactId(value: ByteArray): ByteArray {
            return if (value.size == 16) value else value.copyOf(16)
        }

        private fun nowElapsedUs(): Long = SystemClock.elapsedRealtimeNanos().coerceAtLeast(0L) / 1_000L

    }
}
