package io.jankhunter.runtime

import android.os.Handler
import android.view.View
import io.jankhunter.runtime.internal.io.Jhlog
import java.util.concurrent.Callable

/**
 * Tiny fail-open ABI used by injected bytecode.
 *
 * Keep this facade stateless: loading it must not initialize the much heavier [JankHunter] object.
 * Every entry point owns its complete Throwable boundary and never invokes application business
 * work; wrappers are only prepared or looked up for the original bytecode call site.
 */
internal object JankHunterHooks {
    @JvmStatic
    fun enterMethod(methodId: Long, methodName: String): Long {
        return try {
            hooks().enterMethod(methodId, methodName)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
            0L
        }
    }

    @JvmStatic
    fun exitMethod(token: Long, methodId: Long) {
        try {
            hooks().exitMethod(token, methodId)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
        }
    }

    @JvmStatic
    fun enterSemantic(kind: Int): Long {
        return try {
            hooks().enterSemantic(kind)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
            0L
        }
    }

    @JvmStatic
    fun exitSemantic(token: Long, kind: Int, methodId: Long, methodName: String, outcome: Int) {
        try {
            hooks().exitSemantic(token, kind, methodId, methodName, outcome)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
        }
    }

    @JvmStatic
    fun enterDatabase(): Long {
        return try {
            hooks().enterDatabase()
        } catch (throwable: Throwable) {
            recordFailure(throwable)
            0L
        }
    }

    @JvmStatic
    fun normalizeDatabaseQuery(query: String?): String? {
        return try {
            hooks().normalizeDatabaseQuery(query)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
            null
        }
    }

    @JvmStatic
    fun databaseQueryOperation(query: String?, fallback: Int): Int {
        return try {
            hooks().databaseQueryOperation(query, fallback)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
            fallback
        }
    }

    @JvmStatic
    fun databaseStatementFingerprint(query: String?): Long {
        return try {
            hooks().databaseStatementFingerprint(query)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
            0L
        }
    }

    @JvmStatic
    fun registerPreparedStatement(statement: Any?, query: String?, fingerprint: Long) {
        try {
            hooks().registerPreparedStatement(statement, query, fingerprint)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
        }
    }

    @JvmStatic
    fun resolvePreparedStatement(statement: Any?): Any? {
        return try {
            hooks().resolvePreparedStatement(statement)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
            null
        }
    }

    @JvmStatic
    fun preparedStatementQuery(snapshot: Any?): String? {
        return try {
            hooks().preparedStatementQuery(snapshot)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
            null
        }
    }

    @JvmStatic
    fun preparedStatementFingerprint(snapshot: Any?): Long {
        return try {
            hooks().preparedStatementFingerprint(snapshot)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
            0L
        }
    }

    @JvmStatic
    fun preparedStatementToken(snapshot: Any?): Long {
        return try {
            hooks().preparedStatementToken(snapshot)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
            0L
        }
    }

    @JvmStatic
    fun databaseResultCountBucket(value: Long, capture: Int): Int {
        return try {
            hooks().databaseResultCountBucket(value, capture)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
            Jhlog.DATABASE_COUNT_UNKNOWN.toInt()
        }
    }

    @JvmStatic
    fun beginDatabaseTransaction(database: Any?, sourceId: Long, sourceName: String, mode: Int) {
        try {
            hooks().beginDatabaseTransaction(database, sourceId, sourceName, mode)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
        }
    }

    @JvmStatic
    fun markDatabaseTransactionSuccessful(database: Any?) {
        try {
            hooks().markDatabaseTransactionSuccessful(database)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
        }
    }

    @JvmStatic
    fun endDatabaseTransaction(database: Any?, throwable: Throwable?) {
        try {
            hooks().endDatabaseTransaction(database, throwable)
        } catch (hookFailure: Throwable) {
            recordFailure(hookFailure)
        }
    }

    @JvmStatic
    fun exitDatabase(
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
        try {
            hooks().exitDatabase(
                token, sourceId, sourceName, query, statementFingerprint,
                framework, operation, boundary, resultKnown, resultKind, resultCountBucket,
                statementToken, succeeded, throwable,
            )
        } catch (hookFailure: Throwable) {
            recordFailure(hookFailure)
        }
    }

    @JvmStatic
    fun workerInstanceId(value: Any?): Long {
        return try {
            if (!hooks().isWorkerTracingActive()) return 0L
            val id = value as? java.util.UUID ?: return 0L
            hooks().workerInstanceId(id.mostSignificantBits, id.leastSignificantBits)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
            0L
        }
    }

    @JvmStatic
    fun enterWorker(instanceId: Long, workerId: Long, workerName: String, runAttempt: Int): Long {
        return try {
            hooks().enterWorker(instanceId, workerId, workerName, runAttempt)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
            0L
        }
    }

    @JvmStatic
    fun exitWorker(
        token: Long,
        instanceId: Long,
        workerId: Long,
        workerName: String,
        outcome: Int,
        runAttempt: Int,
    ) {
        try {
            hooks().exitWorker(token, instanceId, workerId, workerName, outcome, runAttempt)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
        }
    }

    @JvmStatic
    fun classifyWorkerOutcome(result: Any?): Int {
        return try {
            hooks().classifyWorkerOutcome(result)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
            JankHunterWorkerOutcome.UNKNOWN.code
        }
    }

    @JvmStatic
    fun recordMethodCall(methodId: Long, methodName: String) {
        try {
            hooks().recordMethodCall(methodId, methodName)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
        }
    }

    @JvmStatic
    fun recordCounter(name: String?, value: Long) {
        try {
            hooks().recordCounter(name, value)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
        }
    }

    @JvmStatic
    fun recordLogSpam(ownerName: String?, source: String?, level: Int) {
        try {
            hooks().recordLogSpam(ownerName, source, level)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
        }
    }

    @JvmStatic
    fun wrapRunnable(runnable: Runnable?, ownerName: String?): Runnable? {
        return try {
            hooks().wrapRunnable(runnable, ownerName)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
            runnable
        }
    }

    @JvmStatic
    fun <T> wrapCallable(callable: Callable<T>?, ownerName: String?): Callable<T>? {
        return try {
            hooks().wrapCallable(callable, ownerName)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
            callable
        }
    }

    @JvmStatic
    fun wrapCoroutineBlock(block: Function2<*, *, *>?, ownerName: String?): Function2<*, *, *>? {
        return try {
            hooks().wrapCoroutineBlock(block, ownerName)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
            block
        }
    }

    @JvmStatic
    fun wrapClickListener(listener: View.OnClickListener?, ownerName: String?): View.OnClickListener? {
        return try {
            hooks().wrapClickListener(listener, ownerName)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
            listener
        }
    }

    @JvmStatic
    fun wrapHandlerRunnable(
        handler: Handler?,
        runnable: Runnable?,
        token: Any?,
        ownerName: String?,
    ): Runnable? {
        return try {
            if (handler == null || runnable == null) runnable else hooks().wrapHandlerRunnable(
                handler,
                runnable,
                token,
                ownerName,
            )
        } catch (throwable: Throwable) {
            recordFailure(throwable)
            runnable
        }
    }

    @JvmStatic
    fun onHandlerPostResult(original: Runnable?, wrapped: Runnable?, posted: Boolean) {
        try {
            if (original != null && wrapped != null) {
                hooks().onHandlerPostResult(original, wrapped, posted)
            }
        } catch (throwable: Throwable) {
            recordFailure(throwable)
        }
    }

    @JvmStatic
    fun handlerWrappers(handler: Handler?, runnable: Runnable?, token: Any?): Array<Runnable> {
        return try {
            if (handler == null || runnable == null) emptyArray() else hooks().handlerWrappers(
                handler,
                runnable,
                token,
            )
        } catch (throwable: Throwable) {
            recordFailure(throwable)
            emptyArray()
        }
    }

    @JvmStatic
    fun clearHandlerWrappers(handler: Handler?, runnable: Runnable?, token: Any?) {
        try {
            if (handler != null && runnable != null) {
                hooks().clearHandlerWrappers(handler, runnable, token)
            }
        } catch (throwable: Throwable) {
            recordFailure(throwable)
        }
    }

    @JvmStatic
    fun clearHandlerWrappers(handler: Handler?, token: Any?) {
        try {
            if (handler != null) hooks().clearHandlerWrappers(handler, token)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
        }
    }

    @JvmStatic
    fun enterAnnotatedContext(
        screenName: String?,
        ownerName: String?,
    ): Any? {
        return try {
            hooks().enterAnnotatedContext(screenName, ownerName)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
            null
        }
    }

    @JvmStatic
    fun exitAnnotatedContext(token: Any?) {
        try {
            hooks().exitAnnotatedContext(token)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
        }
    }

    @JvmStatic
    fun startAnnotatedOperation(name: String?, kind: Int, budgetMs: Long): Any? {
        if (name.isNullOrBlank()) return null
        return try {
            hooks().startOperation(name, operationKind(kind), budgetMs.coerceAtLeast(0L))
        } catch (throwable: Throwable) {
            recordFailure(throwable)
            null
        }
    }

    @JvmStatic
    fun finishAnnotatedOperation(token: Any?, failed: Boolean) {
        if (token !is JankHunterOperation) return
        try {
            if (failed) token.failure() else token.success()
        } catch (throwable: Throwable) {
            recordFailure(throwable)
        }
    }

    private fun operationKind(value: Int): JankHunterOperationKind {
        return when (value) {
            2 -> JankHunterOperationKind.SCREEN
            3 -> JankHunterOperationKind.BACKGROUND
            4 -> JankHunterOperationKind.SYSTEM
            5 -> JankHunterOperationKind.STAGE
            else -> JankHunterOperationKind.USER
        }
    }

    @JvmStatic
    fun watchLifecycleObject(instance: Any?, lifecycleEvent: String?, ownerHint: String?) {
        try {
            hooks().watchLifecycleObject(instance, lifecycleEvent, ownerHint)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
        }
    }

    private fun hooks(): RuntimeInstrumentationHooks = JankHunter.instrumentationHooks()

    private fun recordFailure(throwable: Throwable) {
        RuntimeHookGuard.rethrowFatal(throwable)
        RuntimeHookFailureTracker.record(RuntimeHookFailureReason.INSTRUMENTATION_HOOK)
    }
}
