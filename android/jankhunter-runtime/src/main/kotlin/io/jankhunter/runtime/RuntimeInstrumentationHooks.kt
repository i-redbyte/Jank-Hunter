package io.jankhunter.runtime

import android.os.Handler
import android.view.View
import java.util.concurrent.Callable

/**
 * Internal application-service port for injected bytecode.
 *
 * It deliberately exposes no lifecycle or storage controls. The static fail-open ABI lives in
 * [JankHunterHooks], while this class owns only dispatch to the process-scoped runtime services.
 */
internal class RuntimeInstrumentationHooks(
    private val telemetryAccess: RuntimeTelemetryAccess,
    private val runtimeCallGraph: RuntimeCallGraph,
    private val runtimeHookEvents: RuntimeHookEventTransport,
    private val semanticTelemetry: RuntimeSemanticTelemetry,
    private val workerTelemetry: RuntimeWorkerTelemetry,
    private val databaseTelemetry: RuntimeDatabaseTelemetry,
    private val androidComponentTelemetry: RuntimeAndroidComponentTelemetry,
    private val binderTelemetry: RuntimeBinderTelemetry,
    private val systemTelemetry: RuntimeSystemTelemetry,
    private val asyncTelemetry: RuntimeAsyncTelemetry,
    private val handlerHooks: RuntimeHandlerHooks,
    private val contextTelemetry: RuntimeContextTelemetry,
    private val operationTelemetry: RuntimeOperationTelemetry,
    private val retentionTelemetry: RuntimeRetentionTelemetry,
    private val config: () -> JankHunterConfig?,
) {
    fun enterMethod(methodId: Long, methodName: String): Long {
        return runtimeCallGraph.enter(
            methodId,
            methodName,
            telemetryAccess.isActive() && config()?.runtimeCallGraphEnabled() == true,
        )
    }

    fun exitMethod(token: Long, methodId: Long) = runtimeCallGraph.exit(token, methodId)

    fun enterSemantic(kind: Int): Long = semanticTelemetry.enter(kind)

    fun exitSemantic(token: Long, kind: Int, methodId: Long, methodName: String, outcome: Int) {
        semanticTelemetry.exit(token, kind, methodId, methodName, outcome)
    }

    fun enterDatabase(): Long = databaseTelemetry.enter()

    fun normalizeDatabaseQuery(query: String?): String? = databaseTelemetry.normalizeQuery(query)

    fun databaseQueryOperation(query: String?, fallback: Int): Int {
        return databaseTelemetry.queryOperation(query, fallback)
    }

    fun databaseStatementFingerprint(query: String?): Long = RuntimeSqlNormalizer.fingerprint(query)

    fun registerPreparedStatement(statement: Any?, query: String?, fingerprint: Long) {
        databaseTelemetry.registerPreparedStatement(statement, query, fingerprint)
    }

    fun resolvePreparedStatement(statement: Any?): Any? = databaseTelemetry.resolvePreparedStatement(statement)

    fun preparedStatementQuery(snapshot: Any?): String? = (snapshot as? PreparedStatementSnapshot)?.query

    fun preparedStatementFingerprint(snapshot: Any?): Long {
        return (snapshot as? PreparedStatementSnapshot)?.fingerprint ?: 0L
    }

    fun preparedStatementToken(snapshot: Any?): Long = (snapshot as? PreparedStatementSnapshot)?.token ?: 0L

    fun databaseResultCountBucket(value: Long, capture: Int): Int {
        return io.jankhunter.runtime.databaseResultCountBucket(value, capture)
    }

    fun beginDatabaseTransaction(database: Any?, sourceId: Long, sourceName: String, mode: Int) {
        databaseTelemetry.beginTransaction(database, sourceId, sourceName, mode)
    }

    fun markDatabaseTransactionSuccessful(database: Any?) = databaseTelemetry.markTransactionSuccessful(database)

    fun endDatabaseTransaction(database: Any?, throwable: Throwable?) {
        databaseTelemetry.endTransaction(database, throwable)
    }

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
        databaseTelemetry.exit(
            token, sourceId, sourceName, query, statementFingerprint,
            framework, operation, boundary, resultKnown, resultKind, resultCountBucket,
            statementToken, succeeded, throwable,
        )
    }

    fun enterServiceCallback(): Long = androidComponentTelemetry.enterServiceCallback()

    fun exitServiceCallback(
        token: Long,
        service: Any?,
        intent: Any?,
        componentId: Long,
        componentName: String,
        stage: Int,
        resultCode: Int,
        resultObject: Any?,
        failed: Boolean,
    ) {
        androidComponentTelemetry.exitServiceCallback(
            token,
            service,
            intent,
            componentId,
            componentName,
            stage,
            resultCode,
            resultObject,
            failed,
        )
    }

    fun recordServiceForegroundTransition(
        service: Any?,
        componentId: Long,
        componentName: String,
        stage: Int,
    ) {
        androidComponentTelemetry.recordServiceForegroundTransition(service, componentId, componentName, stage)
    }

    fun enterReceiverCallback(
        receiver: Any?,
        intent: Any?,
        componentId: Long,
        componentName: String,
    ): Long {
        return androidComponentTelemetry.enterReceiverCallback(receiver, intent, componentId, componentName)
    }

    fun registerReceiverAsync(
        pendingResult: Any?,
        receiver: Any?,
        intent: Any?,
        token: Long,
        componentId: Long,
        componentName: String,
    ): Boolean {
        return androidComponentTelemetry.registerReceiverAsync(
            pendingResult,
            receiver,
            intent,
            token,
            componentId,
            componentName,
        )
    }

    fun exitReceiverCallback(
        token: Long,
        receiver: Any?,
        intent: Any?,
        componentId: Long,
        componentName: String,
        asyncStarted: Boolean,
        pendingResult: Any?,
        failed: Boolean,
    ) {
        androidComponentTelemetry.exitReceiverCallback(
            token,
            receiver,
            intent,
            componentId,
            componentName,
            asyncStarted,
            pendingResult,
            failed,
        )
    }

    fun finishReceiverAsync(pendingResult: Any?) {
        androidComponentTelemetry.finishReceiverAsync(pendingResult)
    }

    fun enterBinderClient(): Long = binderTelemetry.enter()

    fun exitBinderClient(
        token: Long,
        descriptor: String?,
        method: String?,
        code: Int,
        flags: Int,
        handled: Boolean,
        throwable: Throwable?,
    ) {
        binderTelemetry.exitClient(token, descriptor, method, code, flags, handled, throwable)
    }

    fun enterBinderServer(): Long = binderTelemetry.enter()

    fun exitBinderServer(
        token: Long,
        descriptor: String?,
        method: String?,
        code: Int,
        flags: Int,
        handled: Boolean,
        throwable: Throwable?,
    ) {
        binderTelemetry.exitServer(token, descriptor, method, code, flags, handled, throwable)
    }

    fun isWorkerTracingActive(): Boolean = workerTelemetry.isEnabled()

    fun workerInstanceId(mostSignificantBits: Long, leastSignificantBits: Long): Long {
        return workerTelemetry.instanceId(mostSignificantBits, leastSignificantBits)
    }

    fun enterWorker(instanceId: Long, workerId: Long, workerName: String, runAttempt: Int): Long {
        return workerTelemetry.started(instanceId, workerId, workerName, runAttempt, generation = 0)
    }

    fun exitWorker(
        token: Long,
        instanceId: Long,
        workerId: Long,
        workerName: String,
        outcome: Int,
        runAttempt: Int,
    ) {
        workerTelemetry.finished(
            token = token,
            instanceId = instanceId,
            workerId = workerId,
            workerName = workerName,
            outcome = workerOutcomeFromCode(outcome),
            runAttempt = runAttempt,
            generation = 0,
            stopReason = 0,
            stopReasonKnown = false,
        )
    }

    fun classifyWorkerOutcome(result: Any?): Int = workerTelemetry.classifyOutcome(result)

    fun recordMethodCall(methodId: Long, methodName: String) {
        if (telemetryAccess.isActive()) runtimeHookEvents.recordMethod(methodId, methodName)
    }

    fun recordCounter(name: String?, value: Long) = systemTelemetry.recordCounter(name, value)

    fun recordLogSpam(ownerName: String?, source: String?, level: Int) {
        systemTelemetry.recordLogSpam(ownerName, source, level)
    }

    fun wrapRunnable(runnable: Runnable?, ownerName: String?): Runnable? {
        return asyncTelemetry.wrapRunnable(runnable, ownerName)
    }

    fun <T> wrapCallable(callable: Callable<T>?, ownerName: String?): Callable<T>? {
        return asyncTelemetry.wrapCallable(callable, ownerName)
    }

    fun wrapCoroutineBlock(block: Function2<*, *, *>?, ownerName: String?): Function2<*, *, *>? {
        return asyncTelemetry.wrapCoroutineBlock(block, ownerName)
    }

    fun wrapClickListener(listener: View.OnClickListener?, ownerName: String?): View.OnClickListener? {
        return asyncTelemetry.wrapClickListener(listener, ownerName)
    }

    fun wrapHandlerRunnable(handler: Handler, runnable: Runnable, token: Any?, ownerName: String?): Runnable {
        return handlerHooks.wrap(handler, runnable, token, ownerName)
    }

    fun onHandlerPostResult(original: Runnable, wrapped: Runnable, posted: Boolean) {
        handlerHooks.onPostResult(original, wrapped, posted)
    }

    fun handlerWrappers(handler: Handler, runnable: Runnable, token: Any?): Array<Runnable> {
        return handlerHooks.wrappers(handler, runnable, token)
    }

    fun clearHandlerWrappers(handler: Handler, runnable: Runnable, token: Any?) {
        handlerHooks.clear(handler, runnable, token)
    }

    fun clearHandlerWrappers(handler: Handler, token: Any?) = handlerHooks.clear(handler, token)

    fun enterAnnotatedContext(screenName: String?, ownerName: String?): Any? {
        return contextTelemetry.enterAnnotated(screenName, ownerName)
    }

    fun exitAnnotatedContext(token: Any?) = contextTelemetry.exitAnnotated(token)

    fun startOperation(name: String, kind: JankHunterOperationKind, budgetMs: Long): JankHunterOperation {
        return operationTelemetry.start(name, kind, budgetMs, JankHunterOperationAttributes.EMPTY)
    }

    fun watchLifecycleObject(instance: Any?, lifecycleEvent: String?, ownerHint: String?) {
        retentionTelemetry.watchLifecycleObject(instance, lifecycleEvent, ownerHint)
    }
}
