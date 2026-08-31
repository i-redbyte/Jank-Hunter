package io.jankhunter.runtime.internal.io

/** Minimal port used by domain encoders; the writer retains segment and dictionary ownership. */
internal interface BinaryEncodingSink {
    fun payload(): BinaryPayload

    fun optionalSymbolId(kind: Int, value: String?): Long

    fun defineStableSymbol(id: Long, name: String?)

    fun producerContext(owner: String? = null): BinaryRecordContext?

    fun emit(
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
        sink.defineStableSymbol(sourceId, sourceName)
        val payload = sink.payload()
            .symbolRef(sink.optionalSymbolId(BinaryLogWriter.DICT_GENERIC, query))
            .stableSymbolRef(sourceId)
            .uvarint(statementFingerprint)
            .uvarint(framework)
            .uvarint(operation)
            .uvarint(outcome)
            .uvarint(failureKind)
            .uvarint(boundary)
            .uvarint(if (resultKnown) 1L else 0L)
            .uvarint(resultKind)
            .uvarint(resultCountBucket)
            .uvarint(BinaryLogWriter.nonNegative(transactionId))
            .uvarint(BinaryLogWriter.nonNegative(statementToken))
            .uvarint(phaseMask)
            .uvarint(BinaryLogWriter.nonNegative(poolWaitUs))
            .uvarint(BinaryLogWriter.nonNegative(lockWaitUs))
            .uvarint(BinaryLogWriter.nonNegative(executeUs))
            .uvarint(BinaryLogWriter.nonNegative(materializeUs))
            .uvarint(BinaryLogWriter.nonNegative(durationUs))
        sink.emit(
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
        sink.defineStableSymbol(sourceId, sourceName)
        val payload = sink.payload()
            .stableSymbolRef(sourceId)
            .uvarint(transactionId)
            .uvarint(BinaryLogWriter.nonNegative(parentId))
            .uvarint(stage)
            .uvarint(mode)
            .uvarint(outcome)
            .uvarint(failureKind)
            .uvarint(BinaryLogWriter.nonNegative(durationUs))
            .uvarint(BinaryLogWriter.nonNegative(statementCount))
            .uvarint(BinaryLogWriter.nonNegative(readCount))
            .uvarint(BinaryLogWriter.nonNegative(writeCount))
        sink.emit(
            Jhlog.TYPE_DATABASE_TRANSACTION,
            if (mainThread) BinaryLogWriter.FLAG_THREAD_MAIN else 0L,
            payload,
            sink.producerContext(),
        )
    }
}
