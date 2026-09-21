package io.jankhunter.runtime

import android.database.sqlite.SQLiteConstraintException
import android.database.sqlite.SQLiteDatabaseCorruptException
import android.database.sqlite.SQLiteDatabaseLockedException
import android.database.sqlite.SQLiteFullException
import android.database.sqlite.SQLiteTableLockedException
import android.os.Looper
import android.os.OperationCanceledException
import android.os.SystemClock
import io.jankhunter.runtime.internal.io.Jhlog
import io.jankhunter.runtime.internal.io.QualityCounterId
import java.net.SocketTimeoutException
import java.util.concurrent.CancellationException
import java.util.concurrent.TimeoutException

internal enum class DatabaseFailureKind(val wireValue: Long) {
    CANCELLED(Jhlog.DATABASE_FAILURE_CANCELLED),
    BUSY_LOCKED(Jhlog.DATABASE_FAILURE_BUSY_LOCKED),
    CONSTRAINT(Jhlog.DATABASE_FAILURE_CONSTRAINT),
    DISK_FULL(Jhlog.DATABASE_FAILURE_DISK_FULL),
    CORRUPTION(Jhlog.DATABASE_FAILURE_CORRUPTION),
    TIMEOUT(Jhlog.DATABASE_FAILURE_TIMEOUT),
    OTHER(Jhlog.DATABASE_FAILURE_OTHER),
}

internal fun databaseFailureKind(throwable: Throwable?): DatabaseFailureKind {
    var candidate = throwable
    repeat(MAX_DATABASE_FAILURE_CAUSE_DEPTH) {
        when (candidate) {
            is OperationCanceledException,
            is CancellationException,
            -> return DatabaseFailureKind.CANCELLED
            is SQLiteDatabaseLockedException,
            is SQLiteTableLockedException,
            -> return DatabaseFailureKind.BUSY_LOCKED
            is SQLiteConstraintException -> return DatabaseFailureKind.CONSTRAINT
            is SQLiteFullException -> return DatabaseFailureKind.DISK_FULL
            is SQLiteDatabaseCorruptException -> return DatabaseFailureKind.CORRUPTION
            is TimeoutException,
            is SocketTimeoutException,
            -> return DatabaseFailureKind.TIMEOUT
        }
        candidate = candidate?.cause
    }
    return DatabaseFailureKind.OTHER
}

private const val MAX_DATABASE_FAILURE_CAUSE_DEPTH = 4

internal class RuntimeDatabaseTelemetry(
    private val access: RuntimeTelemetryAccess,
) {
    private val preparedStatements = PreparedStatementRegistry(
        onEviction = { access.writer?.recordQuality(QualityCounterId.PREPARED_STATEMENT_REGISTRY_EVICTION) },
        onResolutionMissAfterEviction = {
            access.writer?.recordQuality(QualityCounterId.PREPARED_STATEMENT_RESOLUTION_MISS_AFTER_EVICTION)
        },
    )
    private val transactionIds = DatabaseTransactionIdGenerator()
    private val transactions = DatabaseTransactionTracker(
        ids = transactionIds,
        completionSink = DatabaseTransactionCompletionSink(::recordCompletedTransaction),
        epochId = RuntimeLongSource { access.collectionEpoch?.id ?: 0L },
        nanoTime = SystemClock::elapsedRealtimeNanos,
    )
    private val manualTransactions = ManualDatabaseTransactionTracker(
        transactionIds,
        SystemClock::elapsedRealtimeNanos,
        RuntimeLongSource { access.collectionEpoch?.id ?: 0L },
    )

    fun normalizeQuery(query: String?): String? {
        if (!isEnabled()) return null
        return RuntimeSqlNormalizer.normalize(query)
    }

    fun queryOperation(normalizedQuery: String?, fallback: Int): Int {
        return RuntimeSqlNormalizer.operation(normalizedQuery, fallback)
    }

    fun registerPreparedStatement(statement: Any?, query: String?, fingerprint: Long) {
        if (statement == null || !isEnabled()) return
        preparedStatements.register(statement, query, fingerprint)
    }

    fun resolvePreparedStatement(statement: Any?): PreparedStatementSnapshot? {
        if (!isEnabled()) return null
        return preparedStatements.resolve(statement)
    }

    fun beginTransaction(database: Any?, sourceId: Long, sourceName: String, mode: Int): Long {
        if (database == null || sourceId == 0L || !isEnabled()) return 0L
        val epoch = access.collectionEpoch ?: return 0L
        var startedId = 0L
        RuntimeHookGuard.run {
            val token = epoch.tokens.beginTransaction()
            if (token == 0L) return@run
            val transactionId = transactions.begin(
                database,
                sourceId,
                sourceName,
                mode.toLong(),
                rootParentId = manualTransactions.currentTransactionId(epoch.id),
                transactionId = token,
                expectedEpochId = epoch.id,
            )
            if (transactionId == 0L) {
                if (epoch.tokens.claimTransaction(token)) epoch.tokens.releaseTransaction()
                return@run
            }
            startedId = transactionId
            access.ensureContextRecorded(activeWriter = epoch.writer)
            epoch.writer.databaseTransaction(
                sourceId = sourceId,
                sourceName = sourceName,
                transactionId = transactionId,
                parentId = transactions.currentParentId(epoch.id),
                stage = Jhlog.DATABASE_TRANSACTION_BEGIN,
                mode = mode.toLong(),
                outcome = Jhlog.DATABASE_TRANSACTION_OUTCOME_UNKNOWN,
                failureKind = Jhlog.DATABASE_FAILURE_NONE,
                durationUs = 0L,
                statementCount = 0L,
                readCount = 0L,
                writeCount = 0L,
                mainThread = isMainThread(),
            )
        }
        return startedId
    }

    fun markTransactionSuccessful(database: Any?) {
        if (database == null) return
        transactions.markSuccessful(database)
    }

    fun endTransaction(database: Any?, throwable: Throwable?, forceFailure: Boolean = false) {
        if (database == null) return
        RuntimeHookGuard.run {
            val failed = forceFailure || throwable != null
            val failureKind = if (failed) databaseFailureKind(throwable) else DatabaseFailureKind.OTHER
            transactions.finish(database, failureKind, failed)
        }
    }

    fun beginManualTransaction(
        sourceId: Long,
        sourceName: String,
        mode: Int,
    ): JankHunterDatabaseTransactionToken? {
        if (sourceId == 0L || !isEnabled()) return null
        val epoch = access.collectionEpoch ?: return null
        var started: JankHunterDatabaseTransactionToken? = null
        RuntimeHookGuard.run {
            val operationToken = epoch.tokens.beginTransaction()
            if (operationToken == 0L) return@run
            val token = manualTransactions.begin(
                sourceId,
                sourceName,
                mode.toLong(),
                automaticParentId = transactions.currentTransactionId(epoch.id),
                transactionId = operationToken,
                expectedEpochId = epoch.id,
            )
            started = token
            access.ensureContextRecorded(activeWriter = epoch.writer)
            epoch.writer.databaseTransaction(
                sourceId = sourceId,
                sourceName = sourceName,
                transactionId = token.id,
                parentId = token.parentId,
                stage = Jhlog.DATABASE_TRANSACTION_BEGIN,
                mode = token.mode,
                outcome = Jhlog.DATABASE_TRANSACTION_OUTCOME_UNKNOWN,
                failureKind = Jhlog.DATABASE_FAILURE_NONE,
                durationUs = 0L,
                statementCount = 0L,
                readCount = 0L,
                writeCount = 0L,
                mainThread = isMainThread(),
            )
        }
        return started
    }

    fun endManualTransaction(
        token: JankHunterDatabaseTransactionToken,
        outcome: JankHunterDatabaseTransactionOutcome,
        failure: Throwable?,
    ) {
        manualTransactions.release(token)
        RuntimeHookGuard.run {
            val epoch = access.epochForCompletion(token.id) ?: return@run
            if (!epoch.tokens.claimTransaction(token.id)) return@run
            try {
                if (!isEnabled()) {
                    access.rejectAsyncCompletion(RuntimeCollectionEpochs.REJECT_FEATURE_DISABLED)
                    return@run
                }
                val failed = outcome == JankHunterDatabaseTransactionOutcome.FAILURE
                access.ensureContextRecorded(activeWriter = epoch.writer)
                epoch.writer.databaseTransaction(
                    sourceId = token.sourceId,
                    sourceName = token.sourceName,
                    transactionId = token.id,
                    parentId = token.parentId,
                    stage = Jhlog.DATABASE_TRANSACTION_TERMINAL,
                    mode = token.mode,
                    outcome = when (outcome) {
                        JankHunterDatabaseTransactionOutcome.SUCCESS -> Jhlog.DATABASE_TRANSACTION_SUCCESS
                        JankHunterDatabaseTransactionOutcome.ROLLBACK -> Jhlog.DATABASE_TRANSACTION_ROLLBACK
                        JankHunterDatabaseTransactionOutcome.FAILURE -> Jhlog.DATABASE_TRANSACTION_FAILURE
                    },
                    failureKind = if (failed) databaseFailureKind(failure).wireValue else Jhlog.DATABASE_FAILURE_NONE,
                    durationUs = (SystemClock.elapsedRealtimeNanos() - token.startedNanos).coerceAtLeast(0L) /
                        NANOS_PER_MICROSECOND,
                    statementCount = token.statementCount,
                    readCount = token.readCount,
                    writeCount = token.writeCount,
                    mainThread = isMainThread(),
                )
            } finally {
                epoch.tokens.releaseTransaction()
            }
        }
    }

    fun recordManual(
        token: JankHunterDatabaseCallToken,
        resultKind: JankHunterDatabaseResultKind?,
        resultCount: Long,
        throwable: Throwable?,
    ) {
        RuntimeHookGuard.run {
            val epoch = access.epochForCompletion(token.operationToken) ?: return@run
            val started = access.claimAsync(epoch, token.operationToken, RuntimeAsyncTokenTable.DATABASE,
                JankHunterRuntimeFeature.SQLITE)
            if (started < 0L) return@run
            try {
                val durationUs = (SystemClock.elapsedRealtimeNanos() - started).coerceAtLeast(0L) /
                    NANOS_PER_MICROSECOND
                val phaseMask = token.validatedPhaseMask(durationUs)
                val resultKnown = resultKind != null && resultCount >= 0L
                val transactionId = if (token.operation == Jhlog.DATABASE_OPERATION_STATEMENT.toInt()) {
                    currentTransactionId(epoch.id)
                } else {
                    recordTransactionStatement(token.operation.toLong(), epoch.id)
                }
                access.ensureContextRecorded(activeWriter = epoch.writer)
                epoch.writer.database(
                    sourceId = token.sourceId,
                    sourceName = token.sourceName,
                    query = token.query,
                    framework = Jhlog.DATABASE_FRAMEWORK_CUSTOM,
                    operation = token.operation.toLong(),
                    outcome = if (throwable == null) Jhlog.DATABASE_OUTCOME_SUCCESS else Jhlog.DATABASE_OUTCOME_FAILURE,
                    failureKind = if (throwable == null) Jhlog.DATABASE_FAILURE_NONE else databaseFailureKind(throwable).wireValue,
                    boundary = token.boundary.toLong(),
                    statementFingerprint = token.fingerprint,
                    resultKnown = resultKnown,
                    resultKind = resultKind?.wireValue?.toLong() ?: Jhlog.DATABASE_RESULT_UNKNOWN,
                    resultCountBucket = if (resultKnown) {
                        databaseResultCountBucket(resultCount, DATABASE_CAPTURE_AFFECTED_ROWS).toLong()
                    } else {
                        Jhlog.DATABASE_COUNT_UNKNOWN
                    },
                    transactionId = transactionId,
                    phaseMask = phaseMask,
                    poolWaitUs = if (phaseMask != 0L) {
                        token.phaseDurationUs(JankHunterDatabasePhase.POOL_WAIT)
                    } else {
                        0L
                    },
                    lockWaitUs = if (phaseMask != 0L) {
                        token.phaseDurationUs(JankHunterDatabasePhase.LOCK_WAIT)
                    } else {
                        0L
                    },
                    executeUs = if (phaseMask != 0L) {
                        token.phaseDurationUs(JankHunterDatabasePhase.EXECUTE)
                    } else {
                        0L
                    },
                    materializeUs = if (phaseMask != 0L) {
                        token.phaseDurationUs(JankHunterDatabasePhase.MATERIALIZE)
                    } else {
                        0L
                    },
                    durationUs = durationUs,
                    mainThread = isMainThread(),
                )
            } finally {
                epoch.tokens.release(token.operationToken)
            }
        }
    }

    fun enter(): Long {
        if (!isEnabled()) return 0L
        val epoch = access.collectionEpoch ?: return 0L
        return epoch.tokens.begin(RuntimeAsyncTokenTable.DATABASE, SystemClock.elapsedRealtimeNanos())
    }

    fun enterAt(startedNanos: Long): Long {
        if (!isEnabled()) return 0L
        val epoch = access.collectionEpoch ?: return 0L
        return epoch.tokens.begin(RuntimeAsyncTokenTable.DATABASE, startedNanos)
    }

    fun exit(
        token: Long,
        sourceId: Long,
        sourceName: String,
        query: String?,
        statementFingerprint: Long,
        framework: Int,
        operation: Int,
        boundary: Int,
        resultKnown: Boolean,
        resultKind: Int,
        resultCountBucket: Int,
        statementToken: Long,
        succeeded: Boolean,
        throwable: Throwable?,
    ) {
        if (token <= 0L) return
        RuntimeHookGuard.run {
            val epoch = access.epochForCompletion(token) ?: return@run
            val started = access.claimAsync(epoch, token, RuntimeAsyncTokenTable.DATABASE, JankHunterRuntimeFeature.SQLITE)
            if (started < 0L) return@run
            try {
                if (sourceId == 0L) {
                    access.rejectAsyncCompletion(RuntimeAsyncTokenTable.REJECT_INVALID)
                    return@run
                }
                access.ensureContextRecorded(activeWriter = epoch.writer)
                val validResult = succeeded && resultKnown &&
                    resultKind.toLong() in Jhlog.DATABASE_RESULT_ROWS..Jhlog.DATABASE_RESULT_AFFECTED_ROWS &&
                    resultCountBucket.toLong() in Jhlog.DATABASE_COUNT_ZERO..Jhlog.DATABASE_COUNT_OVER_HUNDRED
                val transactionId = if (operation == Jhlog.DATABASE_OPERATION_STATEMENT.toInt()) {
                    currentTransactionId(epoch.id)
                } else {
                    recordTransactionStatement(operation.toLong(), epoch.id)
                }
                epoch.writer.database(
                    sourceId = sourceId,
                    sourceName = sourceName,
                    query = query,
                    framework = framework.toLong(),
                    operation = operation.toLong(),
                    outcome = if (succeeded) Jhlog.DATABASE_OUTCOME_SUCCESS else Jhlog.DATABASE_OUTCOME_FAILURE,
                    failureKind = if (succeeded) Jhlog.DATABASE_FAILURE_NONE else databaseFailureKind(throwable).wireValue,
                    boundary = boundary.toLong(),
                    statementFingerprint = statementFingerprint,
                    resultKnown = validResult,
                    resultKind = if (validResult) resultKind.toLong() else Jhlog.DATABASE_RESULT_UNKNOWN,
                    resultCountBucket = if (validResult) {
                        resultCountBucket.toLong()
                    } else {
                        Jhlog.DATABASE_COUNT_UNKNOWN
                    },
                    transactionId = transactionId,
                    statementToken = statementToken,
                    durationUs = (SystemClock.elapsedRealtimeNanos() - started).coerceAtLeast(0L) / NANOS_PER_MICROSECOND,
                    mainThread = isMainThread(),
                )
            } finally {
                epoch.tokens.release(token)
            }
        }
    }

    fun rejectDuplicateCompletion() = access.rejectAsyncCompletion(RuntimeAsyncTokenTable.REJECT_CONSUMED)

    fun isEnabled(): Boolean = access.isFeatureActive(JankHunterRuntimeFeature.SQLITE)

    private fun currentTransactionId(epochId: Long): Long {
        val automatic = transactions.currentTransactionId(epochId)
        val manual = manualTransactions.currentTransactionId(epochId)
        return if (automatic > manual) automatic else manual
    }

    private fun recordTransactionStatement(operation: Long, epochId: Long): Long {
        val automatic = transactions.currentTransactionId(epochId)
        val manual = manualTransactions.currentTransactionId(epochId)
        return if (automatic > manual) {
            transactions.recordStatement(operation, epochId)
        } else if (manual != 0L) {
            manualTransactions.recordStatement(operation, epochId)
        } else {
            0L
        }
    }

    private fun recordCompletedTransaction(
        transactionId: Long,
        parentId: Long,
        sourceId: Long,
        sourceName: String,
        mode: Long,
        outcome: Long,
        failureKind: Long,
        durationNanos: Long,
        statementCount: Long,
        readCount: Long,
        writeCount: Long,
    ) {
        val epoch = access.epochForCompletion(transactionId) ?: return
        if (!epoch.tokens.claimTransaction(transactionId)) return
        try {
            if (!isEnabled()) {
                access.rejectAsyncCompletion(RuntimeCollectionEpochs.REJECT_FEATURE_DISABLED)
                return
            }
            access.ensureContextRecorded(activeWriter = epoch.writer)
            epoch.writer.databaseTransaction(
                sourceId = sourceId,
                sourceName = sourceName,
                transactionId = transactionId,
                parentId = parentId,
                stage = Jhlog.DATABASE_TRANSACTION_TERMINAL,
                mode = mode,
                outcome = outcome,
                failureKind = failureKind,
                durationUs = durationNanos / NANOS_PER_MICROSECOND,
                statementCount = statementCount,
                readCount = readCount,
                writeCount = writeCount,
                mainThread = isMainThread(),
            )
        } finally {
            epoch.tokens.releaseTransaction()
        }
    }

    private fun isMainThread(): Boolean {
        val mainLooper = Looper.getMainLooper()
        return mainLooper != null && Looper.myLooper() === mainLooper
    }

    private companion object {
        const val NANOS_PER_MICROSECOND = 1_000L
    }
}
