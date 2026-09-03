package io.jankhunter.runtime.internal.io

/** Minimal port used by domain encoders; the writer retains segment and dictionary ownership. */
internal interface BinaryEncodingSink {
    fun payload(): BinaryPayload

    fun symbolId(kind: Int, value: String?): Long = optionalSymbolId(kind, value)

    fun optionalSymbolId(kind: Int, value: String?): Long

    fun defineStableSymbol(id: Long, name: String?): Long

    fun producerContext(owner: String? = null): BinaryRecordContext?

    fun producerContextWithScreen(screen: String?): BinaryRecordContext? = producerContext()

    fun context(screen: String?, owner: String?, operationId: Long = 0L): BinaryRecordContext =
        BinaryRecordContext().set(screenId = 0L, ownerId = 0L, operationId = operationId)

    fun recordInvalidMetric() = Unit

    /** Emits segment dictionary state without semantic producer metadata. */
    fun emitDictionaryDefinition(payload: BinaryPayload)

    /** Emits writer-owned control state without semantic producer metadata. */
    fun emitControl(recordType: Int, payload: BinaryPayload)

    fun emitSemantic(
        recordType: Int,
        attributes: Long,
        payload: BinaryPayload,
        context: BinaryRecordContext?,
        semanticEventCount: Long = 1L,
    )
}

/** Validates and encodes the database domain without owning transport or segment state. */
internal class DatabaseBinaryRecordEncoder(
    private val sink: BinaryEncodingSink,
) {
    private val descriptors = DatabaseDescriptorRegistry()
    private val descriptorResolution = DatabaseDescriptorRegistry.Resolution()
    private var lastTransactionId = 0L

    /** Starts the segment-local transaction delta stream from its canonical zero base. */
    fun resetSegmentState() {
        lastTransactionId = 0L
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
        failureKind: Long,
        boundary: Long,
        statementFingerprint: Long,
        resultKnown: Boolean,
        resultKind: Long,
        resultCountBucket: Long,
        transactionId: Long,
        statementToken: Long,
        phaseMask: Long,
        poolWaitUs: Long,
        lockWaitUs: Long,
        executeUs: Long,
        materializeUs: Long,
    ) {
        require(sourceId != 0L)
        require(framework in Jhlog.DATABASE_FRAMEWORK_SQLITE..Jhlog.DATABASE_FRAMEWORK_CUSTOM)
        require(operation in Jhlog.DATABASE_OPERATION_QUERY..Jhlog.DATABASE_OPERATION_STATEMENT)
        require(outcome in Jhlog.DATABASE_OUTCOME_SUCCESS..Jhlog.DATABASE_OUTCOME_FAILURE)
        require(boundary in Jhlog.DATABASE_BOUNDARY_DISPATCH..Jhlog.DATABASE_BOUNDARY_MANUAL)
        require(failureKind in Jhlog.DATABASE_FAILURE_NONE..Jhlog.DATABASE_FAILURE_OTHER)
        require(outcome != Jhlog.DATABASE_OUTCOME_SUCCESS || failureKind == Jhlog.DATABASE_FAILURE_NONE)
        require(outcome != Jhlog.DATABASE_OUTCOME_FAILURE || failureKind != Jhlog.DATABASE_FAILURE_NONE)
        require(phaseMask and Jhlog.DATABASE_PHASE_KNOWN_MASK.inv() == 0L)
        require(resultKnown || resultKind == Jhlog.DATABASE_RESULT_UNKNOWN && resultCountBucket == Jhlog.DATABASE_COUNT_UNKNOWN)
        require(!resultKnown || resultKind in Jhlog.DATABASE_RESULT_ROWS..Jhlog.DATABASE_RESULT_AFFECTED_ROWS)
        require(!resultKnown || resultCountBucket in Jhlog.DATABASE_COUNT_ZERO..Jhlog.DATABASE_COUNT_OVER_HUNDRED)
        require(phaseMask and Jhlog.DATABASE_PHASE_POOL_WAIT != 0L || poolWaitUs == 0L)
        require(phaseMask and Jhlog.DATABASE_PHASE_LOCK_WAIT != 0L || lockWaitUs == 0L)
        require(phaseMask and Jhlog.DATABASE_PHASE_EXECUTE != 0L || executeUs == 0L)
        require(phaseMask and Jhlog.DATABASE_PHASE_MATERIALIZE != 0L || materializeUs == 0L)
        require(poolWaitUs <= durationUs)
        require(lockWaitUs <= durationUs - poolWaitUs)
        require(executeUs <= durationUs - poolWaitUs - lockWaitUs)
        require(materializeUs <= durationUs - poolWaitUs - lockWaitUs - executeUs)
        val sourceAlias = sink.defineStableSymbol(sourceId, sourceName)
        val queryId = sink.optionalSymbolId(BinaryLogWriter.DICT_GENERIC, query)
        descriptors.resolve(
            sourceId = sourceId,
            queryId = queryId,
            statementFingerprint = statementFingerprint,
            framework = framework,
            operation = operation,
            boundary = boundary,
            result = descriptorResolution,
        )
        val descriptorToken = (descriptorResolution.id shl 1) or if (descriptorResolution.definition) 1L else 0L
        val payload = sink.payload()
            .uvarint(descriptorToken)
        if (descriptorResolution.definition) {
            payload
                .symbolRef(queryId)
                .stableSymbolAlias(sourceAlias)
                .uvarint(statementFingerprint)
                .uvarint(framework)
                .uvarint(operation)
                .uvarint(boundary)
        }
        var presence = 0L
        if (failureKind != Jhlog.DATABASE_FAILURE_NONE) presence = presence or DATABASE_PRESENCE_FAILURE
        if (resultKnown) presence = presence or DATABASE_PRESENCE_RESULT
        if (transactionId != 0L) presence = presence or DATABASE_PRESENCE_TRANSACTION
        if (statementToken != 0L) presence = presence or DATABASE_PRESENCE_STATEMENT_TOKEN
        if (phaseMask != 0L) presence = presence or DATABASE_PRESENCE_PHASES
        payload
            .uvarint(outcome)
            .uvarint(BinaryLogWriter.nonNegative(durationUs))
            .uvarint(presence)
        if (presence and DATABASE_PRESENCE_FAILURE != 0L) payload.uvarint(failureKind)
        if (presence and DATABASE_PRESENCE_RESULT != 0L) {
            payload.uvarint(resultKind).uvarint(resultCountBucket)
        }
        if (presence and DATABASE_PRESENCE_TRANSACTION != 0L) {
            payload.transactionIdDelta(transactionId)
        }
        if (presence and DATABASE_PRESENCE_STATEMENT_TOKEN != 0L) {
            payload.uvarint(BinaryLogWriter.nonNegative(statementToken))
        }
        if (presence and DATABASE_PRESENCE_PHASES != 0L) {
            payload.uvarint(phaseMask)
            if (phaseMask and Jhlog.DATABASE_PHASE_POOL_WAIT != 0L) {
                payload.uvarint(BinaryLogWriter.nonNegative(poolWaitUs))
            }
            if (phaseMask and Jhlog.DATABASE_PHASE_LOCK_WAIT != 0L) {
                payload.uvarint(BinaryLogWriter.nonNegative(lockWaitUs))
            }
            if (phaseMask and Jhlog.DATABASE_PHASE_EXECUTE != 0L) {
                payload.uvarint(BinaryLogWriter.nonNegative(executeUs))
            }
            if (phaseMask and Jhlog.DATABASE_PHASE_MATERIALIZE != 0L) {
                payload.uvarint(BinaryLogWriter.nonNegative(materializeUs))
            }
        }
        sink.emitSemantic(
            Jhlog.TYPE_DATABASE,
            if (mainThread) BinaryLogWriter.FLAG_THREAD_MAIN else 0L,
            payload,
            sink.producerContext(),
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
        require(sourceId != 0L)
        require(transactionId > 0L)
        require(parentId != transactionId)
        require(stage in Jhlog.DATABASE_TRANSACTION_BEGIN..Jhlog.DATABASE_TRANSACTION_TERMINAL)
        require(mode in Jhlog.DATABASE_TRANSACTION_MODE_UNKNOWN..Jhlog.DATABASE_TRANSACTION_READ_ONLY)
        require(failureKind in Jhlog.DATABASE_FAILURE_NONE..Jhlog.DATABASE_FAILURE_OTHER)
        if (stage == Jhlog.DATABASE_TRANSACTION_BEGIN) {
            require(outcome == Jhlog.DATABASE_TRANSACTION_OUTCOME_UNKNOWN)
            require(failureKind == Jhlog.DATABASE_FAILURE_NONE)
            require(durationUs == 0L && statementCount == 0L && readCount == 0L && writeCount == 0L)
        } else {
            require(outcome in Jhlog.DATABASE_TRANSACTION_SUCCESS..Jhlog.DATABASE_TRANSACTION_FAILURE)
            require(outcome != Jhlog.DATABASE_TRANSACTION_FAILURE || failureKind != Jhlog.DATABASE_FAILURE_NONE)
            require(outcome == Jhlog.DATABASE_TRANSACTION_FAILURE || failureKind == Jhlog.DATABASE_FAILURE_NONE)
            require(readCount <= statementCount && writeCount <= statementCount - readCount)
        }
        val sourceAlias = sink.defineStableSymbol(sourceId, sourceName)
        var presence = 0L
        if (parentId != 0L) presence = presence or TRANSACTION_PRESENCE_PARENT
        if (mode != Jhlog.DATABASE_TRANSACTION_MODE_UNKNOWN) presence = presence or TRANSACTION_PRESENCE_MODE
        if (outcome != Jhlog.DATABASE_TRANSACTION_OUTCOME_UNKNOWN) presence = presence or TRANSACTION_PRESENCE_OUTCOME
        if (failureKind != Jhlog.DATABASE_FAILURE_NONE) presence = presence or TRANSACTION_PRESENCE_FAILURE
        if (durationUs != 0L) presence = presence or TRANSACTION_PRESENCE_DURATION
        if (statementCount != 0L) presence = presence or TRANSACTION_PRESENCE_STATEMENTS
        if (readCount != 0L) presence = presence or TRANSACTION_PRESENCE_READS
        if (writeCount != 0L) presence = presence or TRANSACTION_PRESENCE_WRITES
        val payload = sink.payload()
            .stableSymbolAlias(sourceAlias)
            .transactionIdDelta(transactionId)
            .uvarint(stage)
            .uvarint(presence)
        if (presence and TRANSACTION_PRESENCE_PARENT != 0L) payload.svarint(parentId - transactionId)
        if (presence and TRANSACTION_PRESENCE_MODE != 0L) payload.uvarint(mode)
        if (presence and TRANSACTION_PRESENCE_OUTCOME != 0L) payload.uvarint(outcome)
        if (presence and TRANSACTION_PRESENCE_FAILURE != 0L) payload.uvarint(failureKind)
        if (presence and TRANSACTION_PRESENCE_DURATION != 0L) payload.uvarint(BinaryLogWriter.nonNegative(durationUs))
        if (presence and TRANSACTION_PRESENCE_STATEMENTS != 0L) payload.uvarint(BinaryLogWriter.nonNegative(statementCount))
        if (presence and TRANSACTION_PRESENCE_READS != 0L) payload.uvarint(BinaryLogWriter.nonNegative(readCount))
        if (presence and TRANSACTION_PRESENCE_WRITES != 0L) payload.uvarint(BinaryLogWriter.nonNegative(writeCount))
        sink.emitSemantic(
            Jhlog.TYPE_DATABASE_TRANSACTION,
            if (mainThread) BinaryLogWriter.FLAG_THREAD_MAIN else 0L,
            payload,
            sink.producerContext(),
        )
    }

    private fun BinaryPayload.transactionIdDelta(transactionId: Long): BinaryPayload {
        require(transactionId > 0L)
        svarint(transactionId - lastTransactionId)
        lastTransactionId = transactionId
        return this
    }

    private companion object {
        const val DATABASE_PRESENCE_FAILURE = 1L shl 0
        const val DATABASE_PRESENCE_RESULT = 1L shl 1
        const val DATABASE_PRESENCE_TRANSACTION = 1L shl 2
        const val DATABASE_PRESENCE_STATEMENT_TOKEN = 1L shl 3
        const val DATABASE_PRESENCE_PHASES = 1L shl 4

        const val TRANSACTION_PRESENCE_PARENT = 1L shl 0
        const val TRANSACTION_PRESENCE_MODE = 1L shl 1
        const val TRANSACTION_PRESENCE_OUTCOME = 1L shl 2
        const val TRANSACTION_PRESENCE_FAILURE = 1L shl 3
        const val TRANSACTION_PRESENCE_DURATION = 1L shl 4
        const val TRANSACTION_PRESENCE_STATEMENTS = 1L shl 5
        const val TRANSACTION_PRESENCE_READS = 1L shl 6
        const val TRANSACTION_PRESENCE_WRITES = 1L shl 7
    }
}

/** Bounded primitive interner for the static half of database observations. */
internal class DatabaseDescriptorRegistry(
    private val maxEntries: Int = DEFAULT_MAX_ENTRIES,
) {
    class Resolution {
        var id: Long = 0L
        var definition: Boolean = false
    }

    private var descriptorIds = LongArray(INITIAL_CAPACITY)
    private var sourceIds = LongArray(INITIAL_CAPACITY)
    private var queryIds = LongArray(INITIAL_CAPACITY)
    private var fingerprints = LongArray(INITIAL_CAPACITY)
    private var frameworks = LongArray(INITIAL_CAPACITY)
    private var operations = LongArray(INITIAL_CAPACITY)
    private var boundaries = LongArray(INITIAL_CAPACITY)
    private var size = 0

    fun resolve(
        sourceId: Long,
        queryId: Long,
        statementFingerprint: Long,
        framework: Long,
        operation: Long,
        boundary: Long,
        result: Resolution,
    ) {
        var index = find(sourceId, queryId, statementFingerprint, framework, operation, boundary)
        if (index >= 0) {
            result.id = descriptorIds[index]
            result.definition = false
            return
        }
        if (size >= maxEntries.coerceAtLeast(0)) {
            result.id = 0L
            result.definition = true
            return
        }
        if ((size + 1) * 2 > descriptorIds.size) {
            grow()
            index = find(sourceId, queryId, statementFingerprint, framework, operation, boundary)
        }
        index = index.inv()
        val descriptorId = size.toLong() + 1L
        descriptorIds[index] = descriptorId
        sourceIds[index] = sourceId
        queryIds[index] = queryId
        fingerprints[index] = statementFingerprint
        frameworks[index] = framework
        operations[index] = operation
        boundaries[index] = boundary
        size++
        result.id = descriptorId
        result.definition = true
    }

    private fun find(
        sourceId: Long,
        queryId: Long,
        statementFingerprint: Long,
        framework: Long,
        operation: Long,
        boundary: Long,
    ): Int {
        var index = hash(sourceId, queryId, statementFingerprint, framework, operation, boundary) and
            descriptorIds.lastIndex
        while (descriptorIds[index] != 0L) {
            if (
                sourceIds[index] == sourceId &&
                queryIds[index] == queryId &&
                fingerprints[index] == statementFingerprint &&
                frameworks[index] == framework &&
                operations[index] == operation &&
                boundaries[index] == boundary
            ) {
                return index
            }
            index = (index + 1) and descriptorIds.lastIndex
        }
        return index.inv()
    }

    private fun grow() {
        val oldDescriptorIds = descriptorIds
        val oldSourceIds = sourceIds
        val oldQueryIds = queryIds
        val oldFingerprints = fingerprints
        val oldFrameworks = frameworks
        val oldOperations = operations
        val oldBoundaries = boundaries
        val newCapacity = descriptorIds.size shl 1
        descriptorIds = LongArray(newCapacity)
        sourceIds = LongArray(newCapacity)
        queryIds = LongArray(newCapacity)
        fingerprints = LongArray(newCapacity)
        frameworks = LongArray(newCapacity)
        operations = LongArray(newCapacity)
        boundaries = LongArray(newCapacity)
        for (oldIndex in oldDescriptorIds.indices) {
            val descriptorId = oldDescriptorIds[oldIndex]
            if (descriptorId == 0L) continue
            val index = find(
                oldSourceIds[oldIndex],
                oldQueryIds[oldIndex],
                oldFingerprints[oldIndex],
                oldFrameworks[oldIndex],
                oldOperations[oldIndex],
                oldBoundaries[oldIndex],
            ).inv()
            descriptorIds[index] = descriptorId
            sourceIds[index] = oldSourceIds[oldIndex]
            queryIds[index] = oldQueryIds[oldIndex]
            fingerprints[index] = oldFingerprints[oldIndex]
            frameworks[index] = oldFrameworks[oldIndex]
            operations[index] = oldOperations[oldIndex]
            boundaries[index] = oldBoundaries[oldIndex]
        }
    }

    private fun hash(
        sourceId: Long,
        queryId: Long,
        statementFingerprint: Long,
        framework: Long,
        operation: Long,
        boundary: Long,
    ): Int {
        var value = sourceId
        value = mix(value xor queryId.rotateLeft(11))
        value = mix(value xor statementFingerprint.rotateLeft(23))
        value = mix(value xor framework shl 3 xor operation shl 11 xor boundary shl 19)
        return value.toInt()
    }

    private fun mix(raw: Long): Long {
        var value = raw
        value = (value xor (value ushr 33)) * -49064778989728563L
        value = (value xor (value ushr 33)) * -4265267296055464877L
        return value xor (value ushr 33)
    }

    private fun Long.rotateLeft(distance: Int): Long =
        (this shl distance) or (this ushr (Long.SIZE_BITS - distance))

    private companion object {
        const val INITIAL_CAPACITY = 32
        const val DEFAULT_MAX_ENTRIES = 4_096
    }
}
