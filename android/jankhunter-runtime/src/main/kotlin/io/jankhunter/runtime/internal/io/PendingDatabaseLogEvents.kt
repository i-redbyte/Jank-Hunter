package io.jankhunter.runtime.internal.io

internal class PendingDatabaseEvent internal constructor(
    private val pool: PendingDatabaseEventPool,
) : PendingLogEvent(Jhlog.TYPE_DATABASE, null, captureProducer = false) {
    private var sourceId = 0L
    private var sourceName: String? = null
    private var query: String? = null
    private var framework = 0L
    private var operation = 0L
    private var outcome = 0L
    private var durationUs = 0L
    private var mainThread = false
    private var failureKind = 0L
    private var boundary = 0L
    private var statementFingerprint = 0L
    private var resultKnown = false
    private var resultKind = 0L
    private var resultCountBucket = 0L
    private var transactionId = 0L
    private var statementToken = 0L
    private var phaseMask = 0L
    private var poolWaitUs = 0L
    private var lockWaitUs = 0L
    private var executeUs = 0L
    private var materializeUs = 0L

    internal fun initialize(
        producerContext: LogEventContext?,
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
    ): PendingDatabaseEvent {
        captureProducer(producerContext)
        this.sourceId = sourceId
        this.sourceName = sourceName
        this.query = query
        this.framework = framework
        this.operation = operation
        this.outcome = outcome
        this.durationUs = durationUs
        this.mainThread = mainThread
        this.failureKind = failureKind
        this.boundary = boundary
        this.statementFingerprint = statementFingerprint
        this.resultKnown = resultKnown
        this.resultKind = resultKind
        this.resultCountBucket = resultCountBucket
        this.transactionId = transactionId
        this.statementToken = statementToken
        this.phaseMask = phaseMask
        this.poolWaitUs = poolWaitUs
        this.lockWaitUs = lockWaitUs
        this.executeUs = executeUs
        this.materializeUs = materializeUs
        return this
    }

    internal fun clearForRecycle() {
        clearProducer()
        sourceName = null
        query = null
    }

    override fun recycle() = pool.release(this)

    override fun writePayload(writer: BinaryLogWriter) {
        writer.database(
            sourceId, sourceName, query, framework, operation, outcome, durationUs, mainThread,
            failureKind, boundary, statementFingerprint, resultKnown, resultKind, resultCountBucket,
            transactionId, statementToken, phaseMask, poolWaitUs, lockWaitUs, executeUs, materializeUs,
        )
    }
}

internal class PendingDatabaseTransactionEvent internal constructor(
    private val pool: PendingDatabaseTransactionEventPool,
) : PendingLogEvent(Jhlog.TYPE_DATABASE_TRANSACTION, null, captureProducer = false) {
    private var sourceId = 0L
    private var sourceName: String? = null
    private var transactionId = 0L
    private var parentId = 0L
    private var stage = 0L
    private var mode = 0L
    private var outcome = 0L
    private var failureKind = 0L
    private var durationUs = 0L
    private var statementCount = 0L
    private var readCount = 0L
    private var writeCount = 0L
    private var mainThread = false

    internal fun initialize(
        producerContext: LogEventContext?,
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
    ): PendingDatabaseTransactionEvent {
        captureProducer(producerContext)
        this.sourceId = sourceId
        this.sourceName = sourceName
        this.transactionId = transactionId
        this.parentId = parentId
        this.stage = stage
        this.mode = mode
        this.outcome = outcome
        this.failureKind = failureKind
        this.durationUs = durationUs
        this.statementCount = statementCount
        this.readCount = readCount
        this.writeCount = writeCount
        this.mainThread = mainThread
        return this
    }

    internal fun clearForRecycle() {
        clearProducer()
        sourceName = null
    }

    override fun recycle() = pool.release(this)

    override fun writePayload(writer: BinaryLogWriter) {
        writer.databaseTransaction(
            sourceId, sourceName, transactionId, parentId, stage, mode, outcome, failureKind,
            durationUs, statementCount, readCount, writeCount, mainThread,
        )
    }
}
