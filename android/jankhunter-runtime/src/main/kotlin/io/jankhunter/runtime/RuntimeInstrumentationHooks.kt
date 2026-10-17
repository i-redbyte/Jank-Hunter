package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.ThreadLifecycleObservation
import io.jankhunter.runtime.internal.io.ThreadLifecycleStage
import io.jankhunter.runtime.internal.system.ThreadIdentityRegistry
import android.os.Handler
import android.view.View
import java.util.concurrent.Callable

/**
 * Internal application-service port for injected bytecode.
 *
 * It deliberately exposes no lifecycle or storage controls. The static fail-open ABI lives in
 * [JankHunterHooks], while this class owns only dispatch to the process-scoped runtime services.
 */
internal interface InstrumentationCompatibilityHints {
    fun observeThreadFactoryReturned(thread: Thread?, factory: Any?, siteId: Long, source: String, line: Int, origin: Int)
    fun beginExecutorShutdown(executor: Any?, now: Boolean, siteId: Long, source: String, line: Int, origin: Int): Long
    fun beginFutureCancel(future: Any?, mayInterrupt: Boolean, siteId: Long, source: String, line: Int, origin: Int): Long
}

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
    private val concurrencyTelemetry: RuntimeConcurrencyTelemetry,
    private val handlerHooks: RuntimeHandlerHooks,
    private val contextTelemetry: RuntimeContextTelemetry,
    private val operationTelemetry: RuntimeOperationTelemetry,
    private val retentionTelemetry: RuntimeRetentionTelemetry,
) : InstrumentationCompatibilityHints {
    private val executingThreadIdentity = ThreadIdentityRegistration(ownedByJankHunter = false)
    private val threadLifecycleEpoch = ThreadLocal<Long>()
    private val ownedThreadIdentity = ThreadIdentityRegistration(ownedByJankHunter = true)

    fun observeThreadConstructed(thread: Thread?, siteId: Long, source: String, line: Int, origin: Int) =
        concurrencyTelemetry.observeThreadConstructed(thread, siteId, source, line, origin)

    fun prepareThreadTarget(target: Runnable?, siteId: Long, source: String, line: Int, origin: Int): Runnable? =
        concurrencyTelemetry.prepareThreadTarget(target, siteId, source, line, origin)

    fun bindThreadTarget(thread: Thread?, prepared: Runnable?) =
        concurrencyTelemetry.bindThreadTarget(thread, prepared)

    override fun observeThreadFactoryReturned(thread: Thread?, factory: Any?, siteId: Long, source: String, line: Int, origin: Int) {
        concurrencyTelemetry.observeThreadFactoryReturned(thread, siteId, source, line, origin)
    }

    fun beginThreadStart(thread: Thread?, siteId: Long, source: String, line: Int, origin: Int): Long =
        concurrencyTelemetry.beginThreadStart(thread, siteId, source, line, origin)

    fun threadStartReturned(thread: Thread?, token: Long) = concurrencyTelemetry.threadStartReturned(thread, token)

    fun threadStartThrew(thread: Thread?, token: Long) = concurrencyTelemetry.threadStartThrew(thread, token)

    fun beginThreadInterrupt(thread: Thread?, siteId: Long, source: String, line: Int, origin: Int): Long =
        concurrencyTelemetry.beginThreadInterrupt(thread, siteId, source, line, origin)

    fun threadInterruptReturned(thread: Thread?, token: Long) = concurrencyTelemetry.threadInterruptReturned(thread, token)

    fun threadInterruptThrew(thread: Thread?, token: Long) = concurrencyTelemetry.threadInterruptThrew(thread, token)

    fun observeThreadEnd(thread: Thread?) = concurrencyTelemetry.observeThreadEnd(thread)

    fun enterRunnableRun(receiver: Any?, siteId: Long, source: String, line: Int, origin: Int): Long =
        if (receiver is Class<*> && telemetryAccess.usesTaskWrappers) 0L
        else concurrencyTelemetry.enterRunnableRun(receiver, siteId, source, line, origin)

    fun exitRunnableRun(token: Long, outcome: Int) = concurrencyTelemetry.exitRunnableRun(token, outcome)

    fun wrapExecutorRunnable(executor: Any?, command: Runnable?, ownerName: String?): Runnable? =
        if (!isEnabled(JankHunterRuntimeFeature.EXECUTORS) || !telemetryAccess.usesTaskWrappers ||
            executor == null || executor is JankHunterTrackedExecutor || preservesNativeExecutorTasks(executor)) command
        else asyncTelemetry.wrapRunnable(command, ownerName)

    fun <T> wrapExecutorCallable(executor: Any?, command: Callable<T>?, ownerName: String?): Callable<T>? =
        if (!isEnabled(JankHunterRuntimeFeature.EXECUTORS) || !telemetryAccess.usesTaskWrappers ||
            executor == null || preservesNativeExecutorTasks(executor)) command
        else asyncTelemetry.wrapCallable(command, ownerName)

    fun prepareExecutorRunnable(
        executor: Any?, command: Runnable?, siteId: Long, source: String, legacyOwnerLabel: String,
        line: Int, origin: Int, delay: Long, period: Long, unit: Any?, cadence: Int,
    ): Runnable? {
        val legacy = if (isEnabled(JankHunterRuntimeFeature.EXECUTORS))
            wrapExecutorRunnable(executor, command, legacyOwnerLabel) else command
        return if (isEnabled(JankHunterRuntimeFeature.CONCURRENCY_DIAGNOSTICS))
            RuntimeHookGuard.value(legacy) {
                concurrencyTelemetry.prepareExecutorRunnable(executor, command, legacy, siteId, source, line, origin,
                    delay, period, unit, cadence)
            } else legacy
    }

    fun <T> prepareExecutorCallable(
        executor: Any?, command: Callable<T>?, siteId: Long, source: String, legacyOwnerLabel: String,
        line: Int, origin: Int, delay: Long, period: Long, unit: Any?, cadence: Int,
    ): Callable<T>? {
        val legacy = if (isEnabled(JankHunterRuntimeFeature.EXECUTORS))
            wrapExecutorCallable(executor, command, legacyOwnerLabel) else command
        return if (isEnabled(JankHunterRuntimeFeature.CONCURRENCY_DIAGNOSTICS))
            RuntimeHookGuard.value(legacy) {
                concurrencyTelemetry.prepareExecutorCallable(executor, command, legacy, siteId, source, line, origin,
                    delay, period, unit, cadence)
            } else legacy
    }

    fun executorHandoffReturned(prepared: Any?, result: Any?) = concurrencyTelemetry.executorHandoffReturned(prepared, result)
    fun executorHandoffThrew(prepared: Any?) = concurrencyTelemetry.executorHandoffThrew(prepared)
    fun observeExecutorConstructed(executor: Any?, siteId: Long, source: String, line: Int, origin: Int) =
        concurrencyTelemetry.observeExecutorConstructed(executor, siteId, source, line, origin)
    fun observeExecutorFactoryReturned(executor: Any?, siteId: Long, source: String, line: Int, origin: Int) =
        concurrencyTelemetry.observeExecutorFactoryReturned(executor, siteId, source, line, origin)
    override fun beginExecutorShutdown(executor: Any?, now: Boolean, siteId: Long, source: String, line: Int, origin: Int): Long =
        concurrencyTelemetry.beginExecutorShutdown(executor, siteId, source, line, origin)
    fun executorShutdownReturned(executor: Any?, token: Long) = concurrencyTelemetry.executorShutdownReturned(executor, token)
    fun executorShutdownThrew(executor: Any?, token: Long) = concurrencyTelemetry.executorShutdownThrew(executor, token)
    fun observeExecutorTerminated(executor: Any?, terminated: Boolean) =
        concurrencyTelemetry.observeExecutorTerminated(executor, terminated)
    override fun beginFutureCancel(future: Any?, mayInterrupt: Boolean, siteId: Long, source: String,
                          line: Int, origin: Int): Long =
        concurrencyTelemetry.beginFutureCancel(future, siteId, source, line, origin)
    fun futureCancelReturned(future: Any?, token: Long, cancelled: Boolean) =
        concurrencyTelemetry.futureCancelReturned(future, token, cancelled)
    fun futureCancelThrew(future: Any?, token: Long) = concurrencyTelemetry.futureCancelThrew(future, token)

    fun observeCausalWorkSubmissionRequests(requests: Any?, siteId: Long, source: String, line: Int, origin: Int) =
        concurrencyTelemetry.observeCausalWorkSubmissionRequests(requests, siteId, source, line, origin)

    fun observeWorkerImplementationSite(worker: Any?, siteId: Long, source: String, line: Int, origin: Int) =
        concurrencyTelemetry.observeWorkerImplementationSite(worker, siteId, source, line, origin)

    fun enterCallableCall(receiver: Any?, siteId: Long, source: String, line: Int, origin: Int): Long =
        if (receiver is Class<*> && telemetryAccess.usesTaskWrappers) 0L
        else concurrencyTelemetry.enterCallableCall(receiver, siteId, source, line, origin)

    fun exitCallableCall(token: Long, outcome: Int) = concurrencyTelemetry.exitCallableCall(token, outcome)

    fun observeExecutingThread(source: String) {
        val epoch = telemetryAccess.collectionEpoch ?: return
        if (!epoch.config.threadSamplerEnabled() || !epoch.writer.isAcceptingEvents()) return
        if (executingThreadIdentity.isRegistered(epoch.id)) return
        val tid = android.os.Process.myTid().toLong()
        val thread = Thread.currentThread()
        val now = android.os.SystemClock.elapsedRealtimeNanos()
        executingThreadIdentity.register(epoch.id, epoch.threadIdentities, tid, thread, now, source)
        // Keep one direct run event even if the bounded identity registry is full; registration can retry later.
        if (threadLifecycleEpoch.get() != epoch.id) {
            threadLifecycleEpoch.set(epoch.id)
            recordThreadPoint(epoch, ThreadLifecycleStage.RUN_OBSERVED, thread, tid, now, source, "")
        }
    }

    /** Retries registration after capacity rejection until this thread is recorded for the epoch. */
    fun observeOwnedThread(source: String): Boolean {
        val epoch = telemetryAccess.collectionEpoch ?: return false
        if (!epoch.config.threadSamplerEnabled() || !epoch.writer.isAcceptingEvents()) return false
        if (ownedThreadIdentity.isRegistered(epoch.id)) return true
        return ownedThreadIdentity.register(
            epoch.id, epoch.threadIdentities, android.os.Process.myTid().toLong(), Thread.currentThread(),
            android.os.SystemClock.elapsedRealtimeNanos(), source,
        )
    }

    fun observeThreadStart(thread: Thread, source: String) {
        val epoch = telemetryAccess.collectionEpoch ?: return
        if (!epoch.config.threadSamplerEnabled() || !epoch.writer.isAcceptingEvents()) return
        recordThreadPoint(epoch, ThreadLifecycleStage.START_RETURNED, thread, 0, android.os.SystemClock.elapsedRealtimeNanos(), source, "")
    }

    fun observeThreadFactory(thread: Thread?, factory: Any?, source: String) {
        if (thread == null) return
        val epoch = telemetryAccess.collectionEpoch ?: return
        if (!epoch.config.threadSamplerEnabled() || !epoch.writer.isAcceptingEvents()) return
        recordThreadPoint(epoch, ThreadLifecycleStage.FACTORY_RETURNED, thread, 0,
            android.os.SystemClock.elapsedRealtimeNanos(), source, factory?.javaClass?.name.orEmpty())
    }

    private fun recordThreadPoint(
        epoch: RuntimeCollectionEpoch, stage: ThreadLifecycleStage, thread: Thread, tid: Long,
        now: Long, source: String, factory: String,
    ) {
        val id = RuntimeHookGuard.value(0L) { observedThreadId(thread) }
        telemetryAccess.ensureContextRecorded(activeWriter = epoch.writer)
        epoch.writer.threadLifecycle(ThreadLifecycleObservation(
            stage, id, tid, now.coerceAtLeast(0), thread.isDaemon, thread.name.take(256), source.take(256), factory.take(256),
        ))
    }

    fun enterMethod(methodId: Long, methodName: String): Long {
        return runtimeCallGraph.enter(
            methodId,
            methodName,
            telemetryAccess.isFeatureActive(JankHunterRuntimeFeature.CALL_GRAPH),
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

    fun enterServiceCallback(): Long {
        if (!isEnabled(JankHunterRuntimeFeature.ANDROID_COMPONENTS)) return 0L
        return androidComponentTelemetry.enterServiceCallback()
    }

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
        if (!isEnabled(JankHunterRuntimeFeature.ANDROID_COMPONENTS)) return
        androidComponentTelemetry.recordServiceForegroundTransition(service, componentId, componentName, stage)
    }

    fun enterReceiverCallback(
        receiver: Any?,
        intent: Any?,
        componentId: Long,
        componentName: String,
    ): Long {
        if (!isEnabled(JankHunterRuntimeFeature.ANDROID_COMPONENTS)) return 0L
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

    fun enterBinderClient(): Long {
        if (!isEnabled(JankHunterRuntimeFeature.BINDER_IPC)) return 0L
        return binderTelemetry.enter()
    }

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

    fun enterBinderServer(): Long {
        if (!isEnabled(JankHunterRuntimeFeature.BINDER_IPC)) return 0L
        return binderTelemetry.enter()
    }

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

    fun workSubmissionEpoch(): Long = workerTelemetry.registrationEpoch()

    fun observeWorkBuilderPeriodic(builder: Any, periodMS: Long, flexMS: Long?) =
        workerTelemetry.builderPeriodic(builder, periodMS, flexMS)

    fun observeWorkBuilderInitialDelay(builder: Any, delayMS: Long) =
        workerTelemetry.builderInitialDelay(builder, delayMS)

    fun observeWorkBuilderBackoff(builder: Any, policy: Int, delayMS: Long) =
        workerTelemetry.builderBackoff(builder, policy, delayMS)

    fun observeWorkBuilderExpedited(builder: Any, quotaPolicy: Int) =
        workerTelemetry.builderExpedited(builder, quotaPolicy)

    fun observeWorkBuilderBuilt(builder: Any, most: Long, least: Long) =
        workerTelemetry.builderBuilt(builder, most, least)

    fun workChainOmitted(epoch: Long, count: Int) = workerTelemetry.chainOmitted(epoch, count)

    fun observeWorkChainRoot(epoch: Long, continuation: Any, most: Long, least: Long, uniqueName: String?, policy: String?, source: String) =
        workerTelemetry.chainRoot(epoch, continuation, most, least, uniqueName, policy, source)

    fun observeWorkChainChild(epoch: Long, parent: Any, continuation: Any, most: Long, least: Long, source: String) =
        workerTelemetry.chainChild(epoch, parent, continuation, most, least, source)

    fun observeWorkChainCombine(epoch: Long, parents: List<*>, continuation: Any) =
        workerTelemetry.chainCombine(epoch, parents, continuation)

    fun observeWorkSubmission(epoch: Long, most: Long, least: Long, periodic: Boolean, uniqueName: String?, policy: String?, omitted: Int, source: String) =
        workerTelemetry.submitted(most, least, periodic, uniqueName, policy, omitted, epoch, source)

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

    fun workerFailure(token: Long, instanceId: Long, runAttempt: Int, throwable: Throwable) {
        workerTelemetry.failed(token, instanceId, runAttempt, generation = 0, throwable)
    }

    fun classifyWorkerOutcome(result: Any?): Int = workerTelemetry.classifyOutcome(result)

    fun recordMethodCall(methodId: Long, methodName: String) {
        if (isEnabled(JankHunterRuntimeFeature.METHOD_COUNTERS)) {
            runtimeHookEvents.recordMethod(methodId, methodName)
        }
    }

    fun recordCounter(name: String?, value: Long) {
        if (isEnabled(JankHunterRuntimeFeature.HANDLERS)) systemTelemetry.recordCounter(name, value)
    }

    fun recordLogSpam(ownerName: String?, source: String?, level: Int) {
        if (isEnabled(JankHunterRuntimeFeature.LOGGING)) {
            systemTelemetry.recordLogSpam(ownerName, source, level)
        }
    }

    fun wrapRunnable(runnable: Runnable?, ownerName: String?): Runnable? {
        if (!isEnabled(JankHunterRuntimeFeature.EXECUTORS)) return runnable
        return asyncTelemetry.wrapRunnable(runnable, ownerName)
    }

    fun <T> wrapCallable(callable: Callable<T>?, ownerName: String?): Callable<T>? {
        if (!isEnabled(JankHunterRuntimeFeature.EXECUTORS)) return callable
        return asyncTelemetry.wrapCallable(callable, ownerName)
    }

    fun wrapCoroutineBlock(block: Function2<*, *, *>?, ownerName: String?): Function2<*, *, *>? {
        if (!isEnabled(JankHunterRuntimeFeature.COROUTINES)) return block
        return asyncTelemetry.wrapCoroutineBlock(block, ownerName)
    }

    fun wrapCausalCoroutineBlock(block: Function2<*, *, *>?, siteId: Long, source: String, line: Int, origin: Int): Function2<*, *, *>? =
        asyncTelemetry.wrapCausalCoroutineBlock(block, siteId, source, line, origin)

    fun enterCausalCoroutineSegment(continuation: Any?, owner: String, siteId: Long, source: String, line: Int, origin: Int): Long =
        asyncTelemetry.enterCoroutineSegment(continuation, owner,
            isEnabled(JankHunterRuntimeFeature.COROUTINES) || concurrencyTelemetry.isEnabled(), siteId, source, line, origin)

    fun coroutineBuilderReturned(prepared: Function2<*, *, *>?, job: Any?) = asyncTelemetry.coroutineBuilderReturned(prepared, job)
    fun coroutineBuilderThrew(prepared: Function2<*, *, *>?) = asyncTelemetry.coroutineBuilderThrew(prepared)
    fun beginCoroutineJobCancel(job: Any?, siteId: Long, source: String, line: Int, origin: Int): Long =
        asyncTelemetry.beginCoroutineJobCancel(job, siteId, source, line, origin)
    fun coroutineJobCancelReturned(job: Any?, token: Long) = asyncTelemetry.coroutineJobCancelResult(job, token, false)
    fun coroutineJobCancelThrew(job: Any?, token: Long) = asyncTelemetry.coroutineJobCancelResult(job, token, true)

    fun enterCoroutineSegment(continuation: Any?, ownerName: String?): Long {
        return asyncTelemetry.enterCoroutineSegment(
            continuation,
            ownerName,
            collectNew = isEnabled(JankHunterRuntimeFeature.COROUTINES),
        )
    }

    fun exitCoroutineSegment(token: Long, continuation: Any?, result: Any?, throwable: Throwable?) {
        val suspended = throwable == null && result === kotlin.coroutines.intrinsics.COROUTINE_SUSPENDED
        val outcome = when {
            throwable is java.util.concurrent.CancellationException -> CoroutineExecutionOutcome.CANCELLED
            throwable != null -> CoroutineExecutionOutcome.FAILURE
            else -> CoroutineExecutionOutcome.SUCCESS
        }
        asyncTelemetry.exitCoroutineSegment(token, continuation, suspended, outcome)
    }

    fun wrapClickListener(listener: View.OnClickListener?, ownerName: String?): View.OnClickListener? {
        if (!isEnabled(JankHunterRuntimeFeature.INTERACTIONS)) return listener
        return asyncTelemetry.wrapClickListener(listener, ownerName)
    }

    fun wrapHandlerRunnable(handler: Handler, runnable: Runnable, token: Any?, ownerName: String?): Runnable {
        if (!isEnabled(JankHunterRuntimeFeature.HANDLERS)) return runnable
        return handlerHooks.wrap(handler, runnable, token, ownerName)
    }

    fun onHandlerPostResult(original: Runnable, wrapped: Runnable, posted: Boolean) {
        handlerHooks.onPostResult(original, wrapped, posted)
    }

    fun beginHandlerPost(handler: Any?, callback: Runnable?, siteId: Long, source: String,
                         line: Int, origin: Int, requestedDelayMillis: Long): Long =
        concurrencyTelemetry.beginHandlerPost(handler, callback, siteId, source, line, origin,
            requestedDelayMillis)

    fun handlerPostReturned(handler: Any?, callback: Runnable?, token: Long, posted: Boolean) =
        concurrencyTelemetry.handlerPostReturned(handler, callback, token, posted)

    fun handlerPostThrew(handler: Any?, callback: Runnable?, token: Long) =
        concurrencyTelemetry.handlerPostThrew(handler, callback, token)

    fun handlerWrappers(handler: Handler, runnable: Runnable, token: Any?): Array<Runnable> {
        return handlerHooks.wrappers(handler, runnable, token)
    }

    fun clearHandlerWrappers(handler: Handler, runnable: Runnable, token: Any?) {
        handlerHooks.clear(handler, runnable, token)
    }

    fun clearHandlerWrappers(handler: Handler, token: Any?) {
        handlerHooks.clear(handler, token)
    }

    fun enterAnnotatedContext(screenName: String?, ownerName: String?): Any? {
        if (!isEnabled(JankHunterRuntimeFeature.INTERACTIONS)) return null
        return contextTelemetry.enterAnnotated(screenName, ownerName)
    }

    fun exitAnnotatedContext(token: Any?) {
        contextTelemetry.exitAnnotated(token)
    }

    fun startOperation(name: String, kind: JankHunterOperationKind, budgetMs: Long): JankHunterOperation {
        if (!isEnabled(JankHunterRuntimeFeature.INTERACTIONS)) return JankHunterOperation.NONE
        return operationTelemetry.start(name, kind, budgetMs, JankHunterOperationAttributes.EMPTY)
    }

    fun watchLifecycleObject(instance: Any?, lifecycleEvent: String?, ownerHint: String?) {
        if (isEnabled(JankHunterRuntimeFeature.LIFECYCLE_LEAKS)) {
            retentionTelemetry.watchLifecycleObject(instance, lifecycleEvent, ownerHint)
        }
    }

    fun watchLifecycleObject(instance: Any?, targetKind: Int, lifecycleEvent: String?, ownerHint: String?) {
        if (isEnabled(JankHunterRuntimeFeature.LIFECYCLE_LEAKS)) {
            retentionTelemetry.watchLifecycleObject(instance, targetKind, lifecycleEvent, ownerHint)
        }
    }

    private fun isEnabled(feature: JankHunterRuntimeFeature): Boolean {
        return telemetryAccess.isFeatureActive(feature)
    }
}

/** A rejected registration remains retryable after maintenance frees registry capacity. */
internal class ThreadIdentityRegistration(private val ownedByJankHunter: Boolean) {
    private val registeredEpoch = ThreadLocal<Long>()

    fun isRegistered(epochId: Long): Boolean = registeredEpoch.get() == epochId

    fun register(
        epochId: Long,
        identities: ThreadIdentityRegistry,
        tid: Long,
        thread: Thread,
        observedNanos: Long,
        source: String,
    ): Boolean {
        if (isRegistered(epochId)) return true
        if (!identities.register(tid, thread, observedNanos, source, ownedByJankHunter)) return false
        registeredEpoch.set(epochId)
        return true
    }
}
