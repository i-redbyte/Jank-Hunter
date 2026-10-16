package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.WorkSubmissionObservation
import io.jankhunter.runtime.internal.io.WorkSubmissionPolicy
import io.jankhunter.runtime.internal.io.WorkChainEdgeObservation
import io.jankhunter.runtime.internal.io.QualityCounterId
import android.os.Looper
import android.os.SystemClock
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.BinaryLogWriter
import io.jankhunter.runtime.internal.io.WorkerProgressObservation
import io.jankhunter.runtime.internal.io.Jhlog
import java.lang.ref.WeakReference

private const val MAX_CHAIN_PARENTS = 8

internal class RuntimeWorkerTelemetry(
    private val access: RuntimeTelemetryAccess,
    private val semantic: RuntimeSemanticTelemetry,
) {
    private val outcomeClasses = BoundedWeakIdentityCache<Class<*>, JankHunterWorkerOutcome>(16)
    @Volatile private var successType: WeakReference<Class<*>>? = null
    @Volatile private var failureType: WeakReference<Class<*>>? = null
    @Volatile private var retryType: WeakReference<Class<*>>? = null
    private val instanceSalt = mixInstanceId(System.nanoTime(), System.identityHashCode(this).toLong())

    fun classifyOutcome(result: Any?): Int = inferOutcome(result).code

    fun classifyResult(result: Any?): JankHunterWorkerOutcome = inferOutcome(result)

    fun instanceId(mostSignificantBits: Long, leastSignificantBits: Long): Long {
        val mixed = mixInstanceId(
            mostSignificantBits xor instanceSalt,
            java.lang.Long.rotateLeft(leastSignificantBits, ID_ROTATION),
        )
        val id = mixed.nonZero()
        RuntimeHookGuard.run {
            if (isEnabled()) access.writer?.workerIdentity(id, mostSignificantBits, leastSignificantBits)
        }
        return id
    }

    fun enqueued(instanceId: Long, periodic: Boolean) = registrationEvent(instanceId, periodic, Jhlog.WORKER_STAGE_ENQUEUED)

    fun submitted(most: Long, least: Long, periodic: Boolean, uniqueName: String?, policy: String?, omitted: Int, expectedEpoch: Long = 0, callerSource: String? = null) {
        RuntimeHookGuard.run {
            if (!isEnabled()) return@run
            val epoch = access.collectionEpoch ?: return@run
            if (expectedEpoch != 0L && epoch.id != expectedEpoch) return@run
            val selected = if (policy == null) WorkSubmissionPolicy.NONE else WorkSubmissionPolicy.valueOf(policy)
            val name = submissionText(uniqueName)
            val source = submissionText(callerSource)?.takeIf { it.isNotEmpty() }
            val hints = epoch.workBuilders.find(most, least)
            access.ensureContextRecorded(activeWriter = epoch.writer)
            epoch.writer.workSubmission(WorkSubmissionObservation(
                most, least, SystemClock.elapsedRealtimeNanos().coerceAtLeast(0), periodic, name, selected,
                omitted.coerceAtLeast(0), uniqueName != null && name != uniqueName,
                source ?: if (hints != null) "unknown" else null, source != null && source != callerSource,
                backoffPolicy = hints?.backoffPolicy ?: 0,
                requestedBackoffMS = hints?.backoffDelayMS ?: 0,
                quotaPolicy = hints?.quotaPolicy ?: 0,
                requestedPeriodMS = hints?.periodMS.takeIf { periodic },
                requestedFlexMS = hints?.flexMS.takeIf { periodic },
                requestedInitialDelayMS = hints?.initialDelayMS,
            ))
        }
    }

    private fun submissionText(value: String?): String? = value?.let {
        val end = minOf(it.length, 128)
        it.substring(0, if (end > 0 && Character.isHighSurrogate(it[end - 1])) end - 1 else end)
    }

    fun registrationEpoch(): Long = RuntimeHookGuard.value(0L) {
        if (isEnabled()) access.collectionEpoch?.id ?: 0L else 0L
    }

    fun builderPeriodic(builder: Any, periodMS: Long, flexMS: Long?) = builderHint {
        it.workBuilders.periodic(builder, periodMS, flexMS)
    }

    fun builderInitialDelay(builder: Any, delayMS: Long) = builderHint {
        it.workBuilders.initialDelay(builder, delayMS)
    }

    fun builderBackoff(builder: Any, policy: Int, delayMS: Long) = builderHint {
        it.workBuilders.backoff(builder, policy, delayMS)
    }

    fun builderExpedited(builder: Any, quotaPolicy: Int) = builderHint {
        it.workBuilders.expedited(builder, quotaPolicy)
    }

    fun builderBuilt(builder: Any, most: Long, least: Long) = builderHint {
        it.workBuilders.built(builder, most, least)
    }

    private inline fun builderHint(crossinline capture: (RuntimeCollectionEpoch) -> Boolean) {
        RuntimeHookGuard.run {
            val epoch = access.collectionEpoch ?: return@run
            if (isEnabled() && !capture(epoch)) epoch.writer.recordQuality(QualityCounterId.WORK_REQUEST_BUILDER_OMITTED)
        }
    }

    fun chainOmitted(expectedEpoch: Long, count: Int) {
        if (count <= 0) return
        RuntimeHookGuard.run {
            val epoch = access.collectionEpoch ?: return@run
            if (isEnabled() && epoch.id == expectedEpoch) {
                epoch.writer.recordQuality(QualityCounterId.WORK_CHAIN_DECLARATION_OMITTED, count.toLong())
            }
        }
    }

    fun chainRoot(expectedEpoch: Long, continuation: Any, most: Long, least: Long, uniqueName: String?, policy: String?, callerSource: String) {
        RuntimeHookGuard.run {
            val epoch = access.collectionEpoch ?: return@run
            if (!isEnabled() || epoch.id != expectedEpoch) return@run
            if (!epoch.workChains.root(continuation, most, least)) {
                epoch.writer.recordQuality(QualityCounterId.WORK_CHAIN_DECLARATION_OMITTED)
                return@run
            }
            val name = submissionText(uniqueName)
            val source = submissionText(callerSource)?.takeIf(String::isNotEmpty) ?: "unknown"
            access.ensureContextRecorded(activeWriter = epoch.writer)
            epoch.writer.workChainRoot(WorkSubmissionObservation(
                most, least, SystemClock.elapsedRealtimeNanos().coerceAtLeast(0), false,
                name, if (policy == null) WorkSubmissionPolicy.NONE else WorkSubmissionPolicy.valueOf(policy), 0,
                nameTruncated = uniqueName != null && name != uniqueName,
                callerSource = source, sourceTruncated = source != callerSource,
            ))
        }
    }

    fun chainChild(expectedEpoch: Long, parent: Any, continuation: Any, most: Long, least: Long, callerSource: String) {
        RuntimeHookGuard.run {
            val epoch = access.collectionEpoch ?: return@run
            if (!isEnabled() || epoch.id != expectedEpoch) return@run
            val observedNanos = SystemClock.elapsedRealtimeNanos().coerceAtLeast(0)
            val source = submissionText(callerSource)?.takeIf(String::isNotEmpty) ?: "unknown"
            var contextRecorded = false
            val retained = epoch.workChains.then(parent, continuation, most, least) { prerequisite, child ->
                if (!contextRecorded) {
                    access.ensureContextRecorded(activeWriter = epoch.writer)
                    contextRecorded = true
                }
                epoch.writer.workChainEdge(WorkChainEdgeObservation(
                    prerequisite.most, prerequisite.least, child.most, child.least, observedNanos, source,
                    sourceTruncated = source != callerSource,
                ))
            }
            if (!retained) epoch.writer.recordQuality(QualityCounterId.WORK_CHAIN_DECLARATION_OMITTED)
        }
    }

    fun chainCombine(expectedEpoch: Long, parents: List<*>, continuation: Any) {
        RuntimeHookGuard.run {
            val epoch = access.collectionEpoch ?: return@run
            if (!isEnabled() || epoch.id != expectedEpoch) return@run
            if (!isMaterialized(parents)) {
                epoch.writer.recordQuality(QualityCounterId.WORK_CHAIN_DECLARATION_OMITTED)
                return@run
            }
            if (parents.size > MAX_CHAIN_PARENTS) {
                epoch.writer.recordQuality(QualityCounterId.WORK_CHAIN_DECLARATION_OMITTED,
                    (parents.size - MAX_CHAIN_PARENTS).toLong())
            }
            if (!epoch.workChains.combine(parents, continuation)) {
                epoch.writer.recordQuality(QualityCounterId.WORK_CHAIN_DECLARATION_OMITTED)
            }
        }
    }

    fun observeRegistration(expectedEpoch: Long, cancel: Runnable): JankHunterWorkRegistration? = RuntimeHookGuard.value(null) {
        if (!isEnabled()) return@value null
        val epoch = access.collectionEpoch ?: return@value null
        if (expectedEpoch == 0L || epoch.id != expectedEpoch) return@value null
        epoch.registrations.reserve(cancel) { instanceId, periodic ->
            RuntimeHookGuard.run {
                if (isEnabled() && access.collectionEpoch === epoch && instanceId != 0L) {
                    recordEvent(0L, null, instanceId, Jhlog.WORKER_STAGE_REGISTERED_OBSERVED,
                        Jhlog.WORKER_OUTCOME_UNKNOWN, periodic = periodic, activeWriter = epoch.writer)
                }
            }
        }
    }

    fun registeredObserved(instanceId: Long, periodic: Boolean) =
        registrationEvent(instanceId, periodic, Jhlog.WORKER_STAGE_REGISTERED_OBSERVED)

    private fun registrationEvent(instanceId: Long, periodic: Boolean, stage: Long) {
        RuntimeHookGuard.run {
            if (!isEnabled() || instanceId == 0L) return@run
            recordEvent(
                workerId = 0L,
                workerName = null,
                instanceId = instanceId,
                stage = stage,
                outcome = Jhlog.WORKER_OUTCOME_UNKNOWN,
                periodic = periodic,
            )
        }
    }

    fun isEnabled(): Boolean = access.isFeatureActive(JankHunterRuntimeFeature.WORKERS)

    fun failed(token: Long, instanceId: Long, runAttempt: Int, generation: Int, throwable: Throwable) {
        RuntimeHookGuard.run {
            if (token == 0L || instanceId == 0L || runAttempt < 0 || generation < 0 || !isEnabled()) return@run
            val epoch = access.collectionEpoch ?: return@run
            if (!epoch.tokens.isActive(token, RuntimeAsyncTokenTable.WORKER)) return@run
            val observation = captureWorkerFailure(instanceId, runAttempt, generation,
                SystemClock.elapsedRealtimeNanos().coerceAtLeast(0), throwable)
            access.ensureContextRecorded(activeWriter = epoch.writer)
            epoch.writer.workerFailure(observation)
        }
    }

    fun progressRequested(token: Long, instanceId: Long, runAttempt: Int, generation: Int, observedNanos: Long,
                          fields: List<JankHunterWorkDataField>, omittedKeys: Int, truncatedFields: Int) =
        recordProgress(token, WorkerProgressObservation(1, instanceId, runAttempt, generation,
            observedNanos, 0, fields, omittedKeys, truncatedFields))

    fun progressOmitted(token: Long, instanceId: Long, runAttempt: Int, generation: Int,
                        observedNanos: Long, omittedUpdates: Int) =
        recordProgress(token, WorkerProgressObservation(2, instanceId, runAttempt, generation,
            observedNanos, omittedUpdates, emptyList(), 0, 0))

    private fun recordProgress(token: Long, observation: WorkerProgressObservation) {
        RuntimeHookGuard.run {
            if (token == 0L || observation.instanceId == 0L || !isEnabled()) return@run
            val epoch = access.epochForCompletion(token) ?: return@run
            if (!epoch.tokens.isActive(token, RuntimeAsyncTokenTable.WORKER)) return@run
            access.ensureContextRecorded(activeWriter = epoch.writer)
            epoch.writer.workerProgress(observation)
        }
    }

    fun started(
        instanceId: Long,
        workerName: String,
        runAttempt: Int,
        generation: Int,
    ): Long {
        if (!isEnabled() || instanceId == 0L) return 0L
        val normalized = normalizedName(workerName)
        return started(instanceId, stableId(normalized), normalized, runAttempt, generation)
    }

    fun started(
        instanceId: Long,
        workerId: Long,
        workerName: String,
        runAttempt: Int,
        generation: Int,
    ): Long = RuntimeHookGuard.value(0L) {
        if (!isEnabled() || instanceId == 0L || workerId == 0L) return@value 0L
        val epoch = access.collectionEpoch ?: return@value 0L
        val token = epoch.tokens.begin(RuntimeAsyncTokenTable.WORKER, SystemClock.elapsedRealtimeNanos())
        if (token == 0L) return@value 0L
        recordEvent(
            workerId = workerId,
            workerName = workerName,
            instanceId = instanceId,
            stage = Jhlog.WORKER_STAGE_STARTED,
            activeWriter = epoch.writer,
            outcome = Jhlog.WORKER_OUTCOME_UNKNOWN,
            runAttempt = runAttempt,
            generation = generation,
        )
        token
    }

    fun foreground(token: Long, instanceId: Long, workerName: String, runAttempt: Int,
                   generation: Int, entered: Boolean) {
        RuntimeHookGuard.run {
            if (token == 0L || instanceId == 0L || !isEnabled()) return@run
            val epoch = access.epochForCompletion(token) ?: return@run
            if (!epoch.tokens.isActive(token, RuntimeAsyncTokenTable.WORKER)) return@run
            val normalized = normalizedName(workerName)
            recordEvent(
                workerId = stableId(normalized), workerName = normalized, instanceId = instanceId,
                stage = if (entered) Jhlog.WORKER_STAGE_FOREGROUND_ENTERED else
                    Jhlog.WORKER_STAGE_FOREGROUND_ATTEMPT_BOUNDARY,
                outcome = Jhlog.WORKER_OUTCOME_UNKNOWN, runAttempt = runAttempt, generation = generation,
                activeWriter = epoch.writer,
            )
        }
    }

    fun finished(
        token: Long,
        instanceId: Long,
        workerName: String,
        outcome: JankHunterWorkerOutcome,
        runAttempt: Int,
        generation: Int,
        stopReason: Int,
        stopReasonKnown: Boolean,
    ) {
        if (token == 0L) return
        val normalized = normalizedName(workerName)
        finished(
            token,
            instanceId,
            stableId(normalized),
            normalized,
            outcome,
            runAttempt,
            generation,
            stopReason,
            stopReasonKnown,
        )
    }

    fun finished(
        token: Long,
        instanceId: Long,
        workerId: Long,
        workerName: String,
        outcome: JankHunterWorkerOutcome,
        runAttempt: Int,
        generation: Int,
        stopReason: Int,
        stopReasonKnown: Boolean,
    ) {
        RuntimeHookGuard.run {
            if (token == 0L) return@run
            val epoch = access.epochForCompletion(token) ?: return@run
            val started = access.claimAsync(epoch, token, RuntimeAsyncTokenTable.WORKER, JankHunterRuntimeFeature.WORKERS)
            if (started < 0L) return@run
            try {
                if (instanceId == 0L || workerId == 0L) {
                    access.rejectAsyncCompletion(RuntimeAsyncTokenTable.REJECT_INVALID)
                    return@run
                }
                val durationNanos = (SystemClock.elapsedRealtimeNanos() - started).coerceAtLeast(0L)
                recordEvent(
                    workerId = workerId,
                    workerName = workerName,
                    instanceId = instanceId,
                    stage = Jhlog.WORKER_STAGE_FINISHED,
                    activeWriter = epoch.writer,
                    outcome = outcome.wireValue(),
                    durationMs = durationNanos / NANOS_PER_MILLISECOND,
                    runAttempt = runAttempt,
                    generation = generation,
                    stopReason = stopReason,
                    stopReasonKnown = stopReasonKnown,
                )
                semantic.recordBoundary(
                    JankHunterSemanticWork.WORKER,
                    workerId,
                    workerName,
                    durationNanos,
                    outcome,
                    expectedWriter = epoch.writer,
                )
            } finally {
                epoch.tokens.release(token)
            }
        }
    }

    private fun recordEvent(
        workerId: Long,
        workerName: String?,
        instanceId: Long,
        stage: Long,
        outcome: Long,
        durationMs: Long = 0L,
        runAttempt: Int = 0,
        generation: Int = 0,
        stopReason: Int = 0,
        stopReasonKnown: Boolean = false,
        periodic: Boolean = false,
        activeWriter: AsyncLogWriter? = access.writer,
    ) {
        if (activeWriter == null) return
        var flags = access.uiVisibleFlag()
        val mainLooper = Looper.getMainLooper()
        if (mainLooper != null && Looper.myLooper() === mainLooper) {
            flags = flags or BinaryLogWriter.FLAG_THREAD_MAIN
        }
        if (periodic) flags = flags or Jhlog.FLAG_WORKER_PERIODIC
        if (stopReasonKnown) flags = flags or Jhlog.FLAG_WORKER_STOP_REASON_KNOWN
        activeWriter.worker(
            workerId = workerId,
            workerName = workerName,
            instanceId = instanceId,
            stage = stage,
            outcome = outcome,
            durationMs = durationMs,
            runAttempt = runAttempt.coerceAtLeast(0).toLong(),
            generation = generation.coerceAtLeast(0).toLong(),
            stopReason = if (stopReasonKnown) stopReason.coerceAtLeast(0).toLong() else 0L,
            flags = flags,
        )
    }

    private fun mixInstanceId(first: Long, second: Long): Long {
        var value = first xor java.lang.Long.rotateLeft(second, 31) xor ID_GOLDEN_RATIO
        value = (value xor (value ushr 30)) * ID_MIX_1
        value = (value xor (value ushr 27)) * ID_MIX_2
        return value xor (value ushr 31)
    }

    private fun inferOutcome(result: Any?): JankHunterWorkerOutcome {
        val type = result?.javaClass ?: return JankHunterWorkerOutcome.UNKNOWN
        // The three real result classes use a lock-free path. The bounded fallback
        // handles unknown types and alternate class loaders without retaining them.
        if (successType?.get() === type) return JankHunterWorkerOutcome.SUCCESS
        if (failureType?.get() === type) return JankHunterWorkerOutcome.FAILURE
        if (retryType?.get() === type) return JankHunterWorkerOutcome.RETRY
        return outcomeClasses.getOrPut(type) {
            resolveOutcomeClass(type).also { outcome ->
                when (outcome) {
                    JankHunterWorkerOutcome.SUCCESS -> successType = WeakReference(type)
                    JankHunterWorkerOutcome.FAILURE -> failureType = WeakReference(type)
                    JankHunterWorkerOutcome.RETRY -> retryType = WeakReference(type)
                    else -> Unit
                }
            }
        }
    }

    private fun resolveOutcomeClass(type: Class<*>): JankHunterWorkerOutcome {
        // Consumer rules preserve this narrow legacy reflection contract under R8.
        // Literal names also allow shrinkers to recognize the class references.
        // No initialization and no mandatory WorkManager dependency; the bounded cache
        // retains neither result objects nor their class loaders.
        return try {
            val loader = type.classLoader
            when (type) {
                Class.forName("androidx.work.ListenableWorker${'$'}Result${'$'}Success", false, loader) ->
                    JankHunterWorkerOutcome.SUCCESS
                Class.forName("androidx.work.ListenableWorker${'$'}Result${'$'}Failure", false, loader) ->
                    JankHunterWorkerOutcome.FAILURE
                Class.forName("androidx.work.ListenableWorker${'$'}Result${'$'}Retry", false, loader) ->
                    JankHunterWorkerOutcome.RETRY
                else -> JankHunterWorkerOutcome.UNKNOWN
            }
        } catch (_: ClassNotFoundException) {
            JankHunterWorkerOutcome.UNKNOWN
        } catch (_: LinkageError) {
            JankHunterWorkerOutcome.UNKNOWN
        }
    }

    private fun JankHunterWorkerOutcome.wireValue(): Long {
        return when (this) {
            JankHunterWorkerOutcome.SUCCESS -> Jhlog.WORKER_OUTCOME_SUCCESS
            JankHunterWorkerOutcome.FAILURE -> Jhlog.WORKER_OUTCOME_FAILURE
            JankHunterWorkerOutcome.RETRY -> Jhlog.WORKER_OUTCOME_RETRY
            JankHunterWorkerOutcome.CANCELLED -> Jhlog.WORKER_OUTCOME_CANCELLED
            JankHunterWorkerOutcome.STOPPED -> Jhlog.WORKER_OUTCOME_STOPPED
            JankHunterWorkerOutcome.UNKNOWN -> Jhlog.WORKER_OUTCOME_UNKNOWN
        }
    }

    private fun normalizedName(name: String): String = name.trim().takeIf(String::isNotEmpty) ?: UNKNOWN

    private fun stableId(name: String): Long = JankHunterSemanticWork.stableId(WORKER_PREFIX, name)

    private fun Long.nonZero(): Long = if (this == 0L) 1L else this

    private companion object {
        const val WORKER_PREFIX = "jankhunter.worker.v1\u0000"
        const val UNKNOWN = "unknown"
        const val NANOS_PER_MILLISECOND = 1_000_000L
        const val ID_ROTATION = 29
        const val ID_GOLDEN_RATIO = -7046029254386353131L
        const val ID_MIX_1 = -4658895280553007687L
        const val ID_MIX_2 = -7723592293110705685L
    }
}
