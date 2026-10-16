package io.jankhunter.runtime

import android.os.Handler
import android.view.View
import io.jankhunter.runtime.internal.io.Jhlog
import java.util.concurrent.Callable

/**
 * Tiny fail-open ABI used by injected bytecode.
 *
 * Builder observation must not construct or wait for the process runtime graph before SDK initialization.
 * Every entry point owns its complete Throwable boundary and never invokes application business
 * work; wrappers are only prepared or looked up for the original bytecode call site.
 */
internal object JankHunterHooks {
    @JvmStatic
    fun observeOwnedThread(source: String): Boolean = try { hooks().observeOwnedThread(source) } catch (throwable: Throwable) {
        recordFailure(throwable)
        false
    }

    @JvmStatic
    fun workSubmissionEpoch(): Long = try { hooks().workSubmissionEpoch() } catch (throwable: Throwable) {
        recordFailure(throwable)
        0L
    }

    @JvmStatic
    fun workChainOmitted(epoch: Long, count: Int) {
        try { hooks().workChainOmitted(epoch, count) } catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic
    fun observeWorkChainRootRequests(continuation: Any, requests: Any?, uniqueName: Any?, policy: Any?, source: String) {
        try {
            val epoch = hooks().workSubmissionEpoch()
            if (epoch != 0L) WorkChainRequestCapture.root(epoch, continuation, requests, uniqueName, policy, source)
        } catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic
    fun observeWorkChainChildRequests(parent: Any, continuation: Any, requests: Any?, source: String) {
        try {
            val epoch = hooks().workSubmissionEpoch()
            if (epoch != 0L) WorkChainRequestCapture.child(epoch, parent, continuation, requests, source)
        } catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic
    fun observeWorkChainCombineRequests(parents: List<*>, continuation: Any) {
        try {
            val epoch = hooks().workSubmissionEpoch()
            if (epoch != 0L) hooks().observeWorkChainCombine(epoch, parents, continuation)
        } catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic
    fun observeWorkSubmissionRequests(requests: Any?, uniqueName: Any?, policy: Any?, source: String) {
        try {
            val epoch = hooks().workSubmissionEpoch()
            if (epoch != 0L) WorkChainRequestCapture.submission(epoch, requests, uniqueName, policy, source)
        } catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic
    fun observeWorkChainRoot(epoch: Long, continuation: Any, most: Long, least: Long, uniqueName: String?, policy: String?, source: String) {
        try { hooks().observeWorkChainRoot(epoch, continuation, most, least, uniqueName, policy, source) }
        catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic
    fun observeWorkChainChild(epoch: Long, parent: Any, continuation: Any, most: Long, least: Long, source: String) {
        try { hooks().observeWorkChainChild(epoch, parent, continuation, most, least, source) }
        catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic
    fun observeWorkChainCombine(epoch: Long, parents: List<*>, continuation: Any) {
        try { hooks().observeWorkChainCombine(epoch, parents, continuation) }
        catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic
    fun observeWorkSubmission(epoch: Long, most: Long, least: Long, periodic: Boolean, uniqueName: String?, policy: String?, omitted: Int, source: String) {
        try { hooks().observeWorkSubmission(epoch, most, least, periodic, uniqueName, policy, omitted, source) }
        catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic
    fun workSubmissionCaptureFailed(throwable: Throwable) = recordFailure(throwable)

    @JvmStatic fun observeWorkBuilderPeriodCall(builder: Any, period: Long, unit: Any?, flex: Long, flexUnit: Any?) {
        try {
            val active = JankHunter.workerBuilderHooksOrNull() ?: return
            if (active.workSubmissionEpoch() == 0L) return
            val millis = (unit as? java.util.concurrent.TimeUnit)?.toMillis(period) ?: return
            val flexMS = if (flexUnit == null) null else (flexUnit as? java.util.concurrent.TimeUnit)?.toMillis(flex) ?: return
            active.observeWorkBuilderPeriodic(builder, millis, flexMS)
        } catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic fun observeWorkBuilderPeriodDurationCall(builder: Any, period: Any?, flex: Any?) {
        try {
            val active = JankHunter.workerBuilderHooksOrNull() ?: return
            if (active.workSubmissionEpoch() == 0L || android.os.Build.VERSION.SDK_INT < 26) return
            val millis = (period as? java.time.Duration)?.toMillis() ?: return
            val flexMS = if (flex == null) null else (flex as? java.time.Duration)?.toMillis() ?: return
            active.observeWorkBuilderPeriodic(builder, millis, flexMS)
        } catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic fun observeWorkBuilderDelayCall(builder: Any, delay: Long, unit: Any?) {
        try {
            val active = JankHunter.workerBuilderHooksOrNull() ?: return
            if (active.workSubmissionEpoch() == 0L) return
            val millis = (unit as? java.util.concurrent.TimeUnit)?.toMillis(delay) ?: return
            active.observeWorkBuilderInitialDelay(builder, millis)
        } catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic fun observeWorkBuilderDelayDurationCall(builder: Any, delay: Any?) {
        try {
            val active = JankHunter.workerBuilderHooksOrNull() ?: return
            if (active.workSubmissionEpoch() == 0L || android.os.Build.VERSION.SDK_INT < 26) return
            val millis = (delay as? java.time.Duration)?.toMillis() ?: return
            active.observeWorkBuilderInitialDelay(builder, millis)
        } catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic fun observeWorkBuilderBackoffCall(builder: Any, policy: Any?, delay: Long, unit: Any?) {
        try {
            val active = JankHunter.workerBuilderHooksOrNull() ?: return
            if (active.workSubmissionEpoch() == 0L) return
            WorkChainRequestCapture.backoff(builder, policy, delay, unit)
        }
        catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic fun observeWorkBuilderBackoffDurationCall(builder: Any, policy: Any?, duration: Any?) {
        try {
            val active = JankHunter.workerBuilderHooksOrNull() ?: return
            if (active.workSubmissionEpoch() == 0L) return
            WorkChainRequestCapture.backoffDuration(builder, policy, duration)
        }
        catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic fun observeWorkBuilderExpeditedCall(builder: Any, policy: Any?) {
        try {
            val active = JankHunter.workerBuilderHooksOrNull() ?: return
            if (active.workSubmissionEpoch() == 0L) return
            WorkChainRequestCapture.expedited(builder, policy)
        }
        catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic fun observeWorkBuilderBuildCall(builder: Any, request: Any?) {
        try {
            val active = JankHunter.workerBuilderHooksOrNull() ?: return
            if (active.workSubmissionEpoch() == 0L) return
            WorkChainRequestCapture.built(builder, request)
        }
        catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic fun observeWorkBuilderBackoff(builder: Any, policy: Int, delayMS: Long) {
        try { JankHunter.workerBuilderHooksOrNull()?.observeWorkBuilderBackoff(builder, policy, delayMS) }
        catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic fun observeWorkBuilderExpedited(builder: Any, quotaPolicy: Int) {
        try { JankHunter.workerBuilderHooksOrNull()?.observeWorkBuilderExpedited(builder, quotaPolicy) }
        catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic fun observeWorkBuilderBuilt(builder: Any, most: Long, least: Long) {
        try { JankHunter.workerBuilderHooksOrNull()?.observeWorkBuilderBuilt(builder, most, least) }
        catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic
    fun observeThreadStart(thread: Thread, source: String) {
        try {
            hooks().observeThreadStart(thread, source)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
        }
    }

    @JvmStatic
    fun observeThreadFactory(thread: Thread?, factory: Any?, source: String) {
        try {
            hooks().observeThreadFactory(thread, factory, source)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
        }
    }

    @JvmStatic
    fun observeExecutingThread(source: String) {
        try {
            hooks().observeExecutingThread(source)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
        }
    }

    @JvmStatic
    fun observeThreadConstructed(thread: Thread?, siteId: Long, source: String, line: Int, origin: Int) {
        try { hooks().observeThreadConstructed(thread, siteId, source, line, origin) }
        catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic
    fun prepareThreadTarget(target: Runnable?, siteId: Long, source: String, line: Int, origin: Int): Runnable? =
        RuntimeHookGuard.value(target) { hooks().prepareThreadTarget(target, siteId, source, line, origin) }

    @JvmStatic
    fun bindThreadTarget(thread: Thread?, prepared: Runnable?) {
        RuntimeHookGuard.run { hooks().bindThreadTarget(thread, prepared) }
    }

    @JvmStatic
    fun observeThreadFactoryReturned(thread: Thread?, factory: Any?, siteId: Long, source: String, line: Int, origin: Int) {
        try { hooks().observeThreadFactoryReturned(thread, factory, siteId, source, line, origin) }
        catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic
    fun beginThreadStart(thread: Thread?, siteId: Long, source: String, line: Int, origin: Int): Long = try {
        hooks().beginThreadStart(thread, siteId, source, line, origin)
    } catch (throwable: Throwable) { recordFailure(throwable); 0L }

    @JvmStatic
    fun threadStartReturned(thread: Thread?, token: Long) {
        try { hooks().threadStartReturned(thread, token) } catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic
    fun threadStartThrew(thread: Thread?, token: Long) {
        try { hooks().threadStartThrew(thread, token) } catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic
    fun beginThreadInterrupt(thread: Thread?, siteId: Long, source: String, line: Int, origin: Int): Long = try {
        hooks().beginThreadInterrupt(thread, siteId, source, line, origin)
    } catch (throwable: Throwable) { recordFailure(throwable); 0L }

    @JvmStatic
    fun threadInterruptReturned(thread: Thread?, token: Long) {
        try { hooks().threadInterruptReturned(thread, token) } catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic
    fun threadInterruptThrew(thread: Thread?, token: Long) {
        try { hooks().threadInterruptThrew(thread, token) } catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic
    fun observeThreadEnd(thread: Thread?) {
        RuntimeHookGuard.run { hooks().observeThreadEnd(thread) }
    }

    @JvmStatic
    fun enterRunnableRun(receiver: Any?, siteId: Long, source: String, line: Int, origin: Int): Long = try {
        hooks().enterRunnableRun(receiver, siteId, source, line, origin)
    } catch (throwable: Throwable) { recordFailure(throwable); 0L }

    @JvmStatic
    fun exitRunnableRun(token: Long, outcome: Int) {
        try { hooks().exitRunnableRun(token, outcome) } catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic
    fun prepareExecutorRunnable(
        executor: Any?, command: Runnable?, siteId: Long, source: String, legacyOwnerLabel: String,
        line: Int, origin: Int, delay: Long, period: Long, unit: Any?, cadence: Int,
    ): Runnable? = try {
        hooks().prepareExecutorRunnable(executor, command, siteId, source, legacyOwnerLabel,
            line, origin, delay, period, unit, cadence)
    } catch (throwable: Throwable) { recordFailure(throwable); command }

    @JvmStatic
    fun <T> prepareExecutorCallable(
        executor: Any?, command: Callable<T>?, siteId: Long, source: String, legacyOwnerLabel: String,
        line: Int, origin: Int, delay: Long, period: Long, unit: Any?, cadence: Int,
    ): Callable<T>? = try {
        hooks().prepareExecutorCallable(executor, command, siteId, source, legacyOwnerLabel,
            line, origin, delay, period, unit, cadence)
    } catch (throwable: Throwable) { recordFailure(throwable); command }

    @JvmStatic fun executorHandoffReturned(prepared: Any?, result: Any?) {
        try { hooks().executorHandoffReturned(prepared, result) } catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic fun executorHandoffThrew(prepared: Any?) {
        try { hooks().executorHandoffThrew(prepared) } catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic fun observeExecutorConstructed(executor: Any?, siteId: Long, source: String, line: Int, origin: Int) {
        try { hooks().observeExecutorConstructed(executor, siteId, source, line, origin) }
        catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic fun observeExecutorFactoryReturned(executor: Any?, siteId: Long, source: String, line: Int, origin: Int) {
        try { hooks().observeExecutorFactoryReturned(executor, siteId, source, line, origin) }
        catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic fun beginExecutorShutdown(
        executor: Any?, now: Boolean, siteId: Long, source: String, line: Int, origin: Int,
    ): Long = try {
        hooks().beginExecutorShutdown(executor, now, siteId, source, line, origin)
    } catch (throwable: Throwable) { recordFailure(throwable); 0L }

    @JvmStatic fun executorShutdownReturned(executor: Any?, token: Long) {
        try { hooks().executorShutdownReturned(executor, token) } catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic fun executorShutdownThrew(executor: Any?, token: Long) {
        try { hooks().executorShutdownThrew(executor, token) } catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic fun observeExecutorTerminated(executor: Any?, terminated: Boolean) {
        try { hooks().observeExecutorTerminated(executor, terminated) } catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic fun beginFutureCancel(
        future: Any?, mayInterrupt: Boolean, siteId: Long, source: String, line: Int, origin: Int,
    ): Long = try {
        hooks().beginFutureCancel(future, mayInterrupt, siteId, source, line, origin)
    } catch (throwable: Throwable) { recordFailure(throwable); 0L }

    @JvmStatic fun futureCancelReturned(future: Any?, token: Long, cancelled: Boolean) {
        try { hooks().futureCancelReturned(future, token, cancelled) }
        catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic fun futureCancelThrew(future: Any?, token: Long) {
        try { hooks().futureCancelThrew(future, token) } catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic fun observeCausalWorkSubmissionRequests(
        requests: Any?, siteId: Long, source: String, line: Int, origin: Int,
    ) {
        try { hooks().observeCausalWorkSubmissionRequests(requests, siteId, source, line, origin) }
        catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic fun observeWorkerImplementationSite(
        worker: Any?, siteId: Long, source: String, line: Int, origin: Int,
    ) {
        try { hooks().observeWorkerImplementationSite(worker, siteId, source, line, origin) }
        catch (throwable: Throwable) { recordFailure(throwable) }
    }

    @JvmStatic fun enterCallableCall(receiver: Any?, siteId: Long, source: String, line: Int, origin: Int): Long = try {
        hooks().enterCallableCall(receiver, siteId, source, line, origin)
    } catch (throwable: Throwable) { recordFailure(throwable); 0L }

    @JvmStatic fun exitCallableCall(token: Long, outcome: Int) {
        try { hooks().exitCallableCall(token, outcome) } catch (throwable: Throwable) { recordFailure(throwable) }
    }

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
    fun workerFailure(token: Long, instanceId: Long, runAttempt: Int, throwable: Throwable) {
        try {
            hooks().workerFailure(token, instanceId, runAttempt, throwable)
        } catch (hookFailure: Throwable) {
            recordFailure(hookFailure)
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
    fun wrapRunnable(executor: Any?, runnable: Runnable?, ownerName: String?): Runnable? =
        RuntimeHookGuard.value(runnable) { hooks().wrapExecutorRunnable(executor, runnable, ownerName) }

    @JvmStatic
    fun <T> wrapCallable(executor: Any?, callable: Callable<T>?, ownerName: String?): Callable<T>? =
        RuntimeHookGuard.value(callable) { hooks().wrapExecutorCallable(executor, callable, ownerName) }

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
    fun enterCoroutineSegment(continuation: Any?, ownerName: String?): Long {
        return try {
            hooks().enterCoroutineSegment(continuation, ownerName)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
            0L
        }
    }

    @JvmStatic
    fun wrapCausalCoroutineBlock(block: Function2<*, *, *>?, siteId: Long, source: String, line: Int, origin: Int): Function2<*, *, *>? =
        RuntimeHookGuard.value(block) { hooks().wrapCausalCoroutineBlock(block, siteId, source, line, origin) }

    @JvmStatic
    fun enterCausalCoroutineSegment(continuation: Any?, owner: String, siteId: Long, source: String, line: Int, origin: Int): Long =
        RuntimeHookGuard.value(0L) { hooks().enterCausalCoroutineSegment(continuation, owner, siteId, source, line, origin) }

    @JvmStatic
    fun coroutineBuilderReturned(prepared: Function2<*, *, *>?, job: Any?) {
        RuntimeHookGuard.run { hooks().coroutineBuilderReturned(prepared, job) }
    }

    @JvmStatic
    fun coroutineBuilderThrew(prepared: Function2<*, *, *>?) {
        RuntimeHookGuard.run { hooks().coroutineBuilderThrew(prepared) }
    }

    @JvmStatic
    fun beginCoroutineJobCancel(job: Any?, siteId: Long, source: String, line: Int, origin: Int): Long =
        RuntimeHookGuard.value(0L) { hooks().beginCoroutineJobCancel(job, siteId, source, line, origin) }

    @JvmStatic
    fun coroutineJobCancelReturned(job: Any?, token: Long) {
        RuntimeHookGuard.run { hooks().coroutineJobCancelReturned(job, token) }
    }

    @JvmStatic
    fun coroutineJobCancelThrew(job: Any?, token: Long) {
        RuntimeHookGuard.run { hooks().coroutineJobCancelThrew(job, token) }
    }

    @JvmStatic
    fun exitCoroutineSegment(token: Long, continuation: Any?, result: Any?, throwable: Throwable?) {
        try {
            hooks().exitCoroutineSegment(token, continuation, result, throwable)
        } catch (hookFailure: Throwable) {
            recordFailure(hookFailure)
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
    fun beginHandlerPost(handler: Any?, callback: Runnable?, siteId: Long, source: String,
                         line: Int, origin: Int, requestedDelayMillis: Long): Long =
        RuntimeHookGuard.value(0L) {
            hooks().beginHandlerPost(handler, callback, siteId, source, line, origin, requestedDelayMillis)
        }

    @JvmStatic
    fun handlerPostReturned(handler: Any?, callback: Runnable?, token: Long, posted: Boolean) {
        RuntimeHookGuard.run { hooks().handlerPostReturned(handler, callback, token, posted) }
    }

    @JvmStatic
    fun handlerPostThrew(handler: Any?, callback: Runnable?, token: Long) {
        RuntimeHookGuard.run { hooks().handlerPostThrew(handler, callback, token) }
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

    /**
     * Legacy ABI for previously instrumented bytecode. Without generated V1 accessors,
     * automatic lifecycle/binding coverage is partial, including under R8. Rebuild with
     * the current Gradle plugin for the typed instrumentation contract.
     */
    @JvmStatic
    fun watchLifecycleObject(instance: Any?, lifecycleEvent: String?, ownerHint: String?) {
        try {
            hooks().watchLifecycleObject(instance, lifecycleEvent, ownerHint)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
        }
    }

    @JvmStatic
    fun watchLifecycleObject(instance: Any?, targetKind: Int, lifecycleEvent: String?, ownerHint: String?) {
        try {
            hooks().watchLifecycleObject(instance, targetKind, lifecycleEvent, ownerHint)
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
