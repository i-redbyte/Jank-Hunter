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
        nanoTime = SystemClock::elapsedRealtimeNanos,
    )
    private val manualTransactions = ManualDatabaseTransactionTracker(
        transactionIds,
        SystemClock::elapsedRealtimeNanos,
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
        var startedId = 0L
        RuntimeHookGuard.run {
            val transactionId = transactions.begin(
                database,
                sourceId,
                sourceName,
                mode.toLong(),
                rootParentId = manualTransactions.currentTransactionId(),
            )
            if (transactionId == 0L) return@run
            startedId = transactionId
            access.ensureContextRecorded()
            access.writer?.databaseTransaction(
                sourceId = sourceId,
                sourceName = sourceName,
                transactionId = transactionId,
                parentId = transactions.currentParentId(),
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
        if (database == null || !isEnabled()) return
        transactions.markSuccessful(database)
    }

    fun endTransaction(database: Any?, throwable: Throwable?, forceFailure: Boolean = false) {
        if (database == null || !isEnabled()) return
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
        var started: JankHunterDatabaseTransactionToken? = null
        RuntimeHookGuard.run {
            val token = manualTransactions.begin(
                sourceId,
                sourceName,
                mode.toLong(),
                automaticParentId = transactions.currentTransactionId(),
            )
            started = token
            access.ensureContextRecorded()
            access.writer?.databaseTransaction(
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
        if (!isEnabled()) return
        RuntimeHookGuard.run {
            val failed = outcome == JankHunterDatabaseTransactionOutcome.FAILURE
            access.ensureContextRecorded()
            access.writer?.databaseTransaction(
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
        }
    }

    fun recordManual(
        token: JankHunterDatabaseCallToken,
        resultKind: JankHunterDatabaseResultKind?,
        resultCount: Long,
        throwable: Throwable?,
    ) {
        if (!isEnabled()) return
        RuntimeHookGuard.run {
            val durationUs = (SystemClock.elapsedRealtimeNanos() - token.startedNanos).coerceAtLeast(0L) /
                NANOS_PER_MICROSECOND
            val phaseMask = token.validatedPhaseMask(durationUs)
            val resultKnown = resultKind != null && resultCount >= 0L
            val transactionId = if (token.operation == Jhlog.DATABASE_OPERATION_STATEMENT.toInt()) {
                currentTransactionId()
            } else {
                recordTransactionStatement(token.operation.toLong())
            }
            access.ensureContextRecorded()
            access.writer?.database(
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
        }
    }

    fun enter(): Long {
        if (!isEnabled()) return 0L
        return SystemClock.elapsedRealtimeNanos().coerceAtLeast(1L)
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
        if (token <= 0L || sourceId == 0L || !isEnabled()) return
        RuntimeHookGuard.run {
            access.ensureContextRecorded()
            val validResult = succeeded && resultKnown &&
                resultKind.toLong() in Jhlog.DATABASE_RESULT_ROWS..Jhlog.DATABASE_RESULT_AFFECTED_ROWS &&
                resultCountBucket.toLong() in Jhlog.DATABASE_COUNT_ZERO..Jhlog.DATABASE_COUNT_OVER_HUNDRED
            val transactionId = if (operation == Jhlog.DATABASE_OPERATION_STATEMENT.toInt()) {
                currentTransactionId()
            } else {
                recordTransactionStatement(operation.toLong())
            }
            access.writer?.database(
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
                durationUs = (SystemClock.elapsedRealtimeNanos() - token).coerceAtLeast(0L) / NANOS_PER_MICROSECOND,
                mainThread = isMainThread(),
            )
        }
    }

    fun isEnabled(): Boolean = access.isActive() && access.config?.databaseTracingEnabled() == true

    private fun currentTransactionId(): Long {
        val automatic = transactions.currentTransactionId()
        val manual = manualTransactions.currentTransactionId()
        return if (automatic > manual) automatic else manual
    }

    private fun recordTransactionStatement(operation: Long): Long {
        val automatic = transactions.currentTransactionId()
        val manual = manualTransactions.currentTransactionId()
        return if (automatic > manual) {
            transactions.recordStatement(operation)
        } else if (manual != 0L) {
            manualTransactions.recordStatement(operation)
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
        access.ensureContextRecorded()
        access.writer?.databaseTransaction(
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
    }

    private fun isMainThread(): Boolean {
        val mainLooper = Looper.getMainLooper()
        return mainLooper != null && Looper.myLooper() === mainLooper
    }

    private companion object {
        const val NANOS_PER_MICROSECOND = 1_000L
    }
}
