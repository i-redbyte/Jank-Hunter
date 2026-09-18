package io.jankhunter.runtime.internal.io

import android.os.SystemClock
import io.jankhunter.runtime.JankHunterBinaryStorage
import io.jankhunter.runtime.JankHunterConfig
import io.jankhunter.runtime.JankHunterHttpEvent
import io.jankhunter.runtime.JankHunterWebSocketEvent
import io.jankhunter.runtime.JankHunterLogGrowthSummary
import io.jankhunter.runtime.JankHunterOperationAttributes
import io.jankhunter.runtime.JankHunterStorageSwitchResult
import io.jankhunter.runtime.OperationEventSink
import io.jankhunter.runtime.RuntimeLongSource
import io.jankhunter.runtime.internal.concurrent.BoundedMpscQueue.OfferResult
import io.jankhunter.runtime.internal.monotonicDeadlineAfterMillis
import io.jankhunter.runtime.internal.system.RetentionEvidence
import io.jankhunter.runtime.internal.system.isRuntimeMainThread
import java.io.File
import java.io.IOException
import java.util.concurrent.TimeUnit
import java.util.concurrent.locks.ReentrantLock
import java.util.concurrent.locks.LockSupport
import kotlin.concurrent.withLock

/**
 * Non-blocking producer facade for the binary session writer.
 *
 * The file and worker stay lazy. Preallocated bounded MPSC event lanes avoid queue-node allocation
 * and blocking queue locks. High-value evidence has an independent reserve, while a short
 * fail-open admission critical section assigns one global sequence across both lanes. Quality and
 * flush controls never consume bulk slots.
 */
internal class AsyncLogWriter internal constructor(
    private val directory: File,
    private val config: JankHunterConfig,
    private val processName: String,
    private val expectedProcesses: Set<String>,
    private val rosterDeclarationComplete: Boolean,
    private val sessionStartMs: Long,
    private val sessionLocalDate: String,
    private val collectorStartElapsedUs: Long,
    private val currentTimeMs: RuntimeLongSource,
    private val quality: LogQualityCounters,
    private val prepareSession: () -> LogGrowthManager?,
    private val onTerminalStop: AsyncWriterTerminalObserver,
    private val workerThreadFactory: (Runnable, String) -> Thread = ::Thread,
    private val buildIdentity: RuntimeBuildIdentity = RuntimeBuildIdentity.Unknown(RuntimeBuildIdentity.Reason.MISSING),
) : OperationEventSink {
    @Volatile
    private var logGrowthManager: LogGrowthManager? = null

    private var sessionFactory: AsyncLogSessionFactory? = null
    private val producer = AsyncWriterProducer(config)
    private val consumer = AsyncWriterConsumer(
        directory = directory,
        config = config,
        processName = processName,
        sessionLocalDate = sessionLocalDate,
        quality = quality,
    )
    private val lifecycle = AsyncWriterLifecycle()
    private val collectionEndLock = ReentrantLock()
    private var collectionEndObserver: (() -> Unit)? = null
    // Match relative semaphore waits: active time, excluding Android deep sleep.
    private val flushIntervalNs = TimeUnit.MILLISECONDS.toNanos(config.flushIntervalMs())
    private val controls = AsyncWriterControlCoordinator(
        quality = quality,
        beginSubmission = ::beginControlSubmission,
        wakeWorker = { producer.requestWorkerWake() },
    )
    fun session(
        appVersion: String?,
        build: String?,
        device: String?,
        sdkInt: Int,
        androidRelease: String?,
        securityPatch: String?,
        primaryAbi: String?,
        supportedAbis: String?,
        manufacturer: String?,
        brand: String?,
        hardware: String?,
        board: String?,
        product: String?,
        deviceRooted: Boolean,
        collectorFlags: Long,
    ): Boolean {
        return enqueue(Jhlog.TYPE_SESSION, LogEventLane.CRITICAL) {
            PendingSessionEvent(
                producer.context.capture(),
                appVersion,
                build,
                device,
                sdkInt,
                androidRelease,
                securityPatch,
                primaryAbi,
                supportedAbis,
                manufacturer,
                brand,
                hardware,
                board,
                product,
                deviceRooted,
                collectorFlags,
            )
        }
    }

    /** Updates attribution for the calling producer thread without touching either queue. */
    fun updateProducerContext(
        screen: String?,
        owner: String?,
        operationId: Long = 0L,
    ) {
        producer.context.update(screen, owner, operationId)
    }

    fun context(
        networkKind: Int,
        batteryPct: Int,
        availMemoryKb: Long,
        batteryState: Int,
        batteryTempDeciC: Int,
        lowMemory: Boolean,
        networkMetered: Boolean,
        networkValidated: Boolean,
        rxBytes: Long,
        txBytes: Long,
        totalMemoryKb: Long,
        freeStorageKb: Long,
        totalStorageKb: Long,
        networkVpn: Boolean,
        foreground: Boolean,
        trafficUidPlusOne: Long = 0L,
        trafficKnownFlags: Int = 0,
    ) {
        enqueue(Jhlog.TYPE_DEVICE_CONTEXT, LogEventLane.BULK) {
            PendingDeviceContextEvent(
                producer.context.capture(),
                networkKind,
                batteryPct,
                availMemoryKb,
                batteryState,
                batteryTempDeciC,
                lowMemory,
                networkMetered,
                networkValidated,
                rxBytes,
                txBytes,
                totalMemoryKb,
                freeStorageKb,
                totalStorageKb,
                networkVpn,
                foreground,
                trafficUidPlusOne,
                trafficKnownFlags,
            )
        }
    }

    fun http(
        screen: String?,
        owner: String?,
        route: String?,
        event: JankHunterHttpEvent,
        flags: Long,
    ) {
        enqueue(Jhlog.TYPE_HTTP, LogEventLane.BULK) {
            PendingHttpEvent(
                producer.context.capture(screen, owner),
                owner,
                route,
                event,
                flags,
            )
        }
    }

    fun webSocket(
        screen: String?,
        owner: String?,
        event: JankHunterWebSocketEvent,
    ) {
        enqueue(Jhlog.TYPE_WEBSOCKET, LogEventLane.BULK) {
            PendingWebSocketEvent(producer.context.capture(screen, owner), owner, event)
        }
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
        failureKind: Long = if (outcome == Jhlog.DATABASE_OUTCOME_FAILURE) Jhlog.DATABASE_FAILURE_OTHER else Jhlog.DATABASE_FAILURE_NONE,
        boundary: Long = Jhlog.DATABASE_BOUNDARY_EXECUTE,
        statementFingerprint: Long = 0L,
        resultKnown: Boolean = false,
        resultKind: Long = Jhlog.DATABASE_RESULT_UNKNOWN,
        resultCountBucket: Long = Jhlog.DATABASE_COUNT_UNKNOWN,
        transactionId: Long = 0L,
        statementToken: Long = 0L,
        phaseMask: Long = 0L,
        poolWaitUs: Long = 0L,
        lockWaitUs: Long = 0L,
        executeUs: Long = 0L,
        materializeUs: Long = 0L,
    ) {
        enqueue(Jhlog.TYPE_DATABASE, if (mainThread) LogEventLane.CRITICAL else LogEventLane.BULK) {
            producer.databaseEventPool.acquire(
                producer.context.capture(), sourceId, sourceName, query, framework,
                operation, outcome, durationUs, mainThread, failureKind, boundary,
                statementFingerprint, resultKnown, resultKind, resultCountBucket,
                transactionId, statementToken, phaseMask, poolWaitUs, lockWaitUs,
                executeUs, materializeUs,
            )
        }
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
        enqueue(Jhlog.TYPE_DATABASE_TRANSACTION, if (mainThread) LogEventLane.CRITICAL else LogEventLane.BULK) {
            producer.databaseTransactionEventPool.acquire(
                producer.context.capture(), sourceId, sourceName, transactionId, parentId, stage, mode,
                outcome, failureKind, durationUs, statementCount, readCount, writeCount, mainThread,
            )
        }
    }

    fun processState(
        uiVisibility: Long,
        processImportance: Long,
        androidImportance: Long,
        reason: Long,
    ) {
        enqueue(Jhlog.TYPE_PROCESS_STATE, LogEventLane.CRITICAL) {
            PendingProcessStateEvent(
                producer.context.capture(),
                uiVisibility,
                processImportance,
                androidImportance,
                reason,
            )
        }
    }

    fun androidComponent(
        componentId: Long,
        componentName: String?,
        action: String?,
        instanceId: Long,
        flowId: Long,
        kind: Long,
        stage: Long,
        outcome: Long,
        durationUs: Long,
        componentFlags: Long,
    ) {
        enqueue(Jhlog.TYPE_ANDROID_COMPONENT, LogEventLane.CRITICAL) {
            PendingAndroidComponentEvent(
                producer.context.capture(),
                componentId,
                componentName,
                action,
                instanceId,
                flowId,
                kind,
                stage,
                outcome,
                durationUs,
                componentFlags,
            )
        }
    }

    fun binderTransaction(
        descriptor: String?,
        method: String?,
        callId: Long,
        direction: Long,
        transactionCode: Long,
        outcome: Long,
        failureKind: Long,
        durationUs: Long,
        binderFlags: Long,
        mainThread: Boolean,
    ) {
        val lane = if (mainThread || outcome == Jhlog.BINDER_OUTCOME_FAILURE) {
            LogEventLane.CRITICAL
        } else {
            LogEventLane.BULK
        }
        enqueue(Jhlog.TYPE_BINDER_TRANSACTION, lane) {
            PendingBinderTransactionEvent(
                producer.context.capture(),
                descriptor,
                method,
                callId,
                direction,
                transactionCode,
                outcome,
                failureKind,
                durationUs,
                binderFlags,
                mainThread,
            )
        }
    }

    fun stall(
        screen: String?,
        owner: String?,
        stackHint: String?,
        durationMs: Long,
        foreground: Boolean,
        incidentId: Long = 0L,
        state: Long = Jhlog.STALL_STATE_RECOVERED,
        stackOrigin: SymbolOrigin = SymbolOrigin.UNKNOWN,
    ) {
        validateStallLifecycle(incidentId, state)
        enqueue(Jhlog.TYPE_STALL, LogEventLane.CRITICAL) {
            PendingStallEvent(
                producer.context.capture(screen, owner),
                screen,
                owner,
                stackHint,
                durationMs,
                foreground,
                incidentId,
                state,
                stackOrigin,
            )
        }
    }

    fun memory(pssKb: Long, javaHeapKb: Long, nativeHeapKb: Long, foreground: Boolean) {
        enqueue(Jhlog.TYPE_MEMORY, LogEventLane.BULK) {
            PendingMemoryEvent(producer.context.capture(), pssKb, javaHeapKb, nativeHeapKb, foreground)
        }
    }

    fun retained(
        screen: String?,
        owner: String?,
        className: String?,
        holder: String?,
        ageMs: Long,
        count: Long,
        foreground: Boolean,
        evidence: RetentionEvidence,
        classOrigin: SymbolOrigin = SymbolOrigin.UNKNOWN,
    ) {
        enqueue(Jhlog.TYPE_RETAINED, LogEventLane.CRITICAL) {
            PendingRetainedEvent(
                producer.context.capture(),
                screen,
                owner,
                className,
                holder,
                ageMs,
                count,
                foreground,
                evidence.wireValue,
                classOrigin,
            )
        }
    }

    fun uiWindow(
        screen: String?,
        windowMs: Long,
        frameCount: Long,
        jankCount: Long,
        source: Long,
        frameDeadlineUs: Long,
        frameDurationBuckets: LongArray,
        foreground: Boolean,
        flags: Long = 0L,
    ) {
        enqueue(Jhlog.TYPE_UI_WINDOW, LogEventLane.BULK) {
            PendingUiWindowEvent(
                producer.context.capture(),
                screen,
                windowMs,
                frameCount,
                jankCount,
                source,
                frameDeadlineUs,
                frameDurationBuckets.copyOf(),
                foreground,
                flags,
            )
        }
    }

    fun processExit(
        reason: Long,
        timestampUnixMs: Long,
        importance: Long,
        pssKb: Long,
        rssKb: Long,
        processName: String?,
    ) {
        enqueue(Jhlog.TYPE_PROCESS_EXIT, LogEventLane.CRITICAL) {
            PendingProcessExitEvent(
                producer.context.capture(),
                reason,
                timestampUnixMs,
                importance,
                pssKb,
                rssKb,
                processName,
            )
        }
    }

    fun io(
        operation: Long,
        durationUs: Long,
        bytes: Long,
        mainThread: Boolean,
        sourceId: Long,
        sourceName: String?,
        outcome: Long,
        bytesKnown: Boolean,
    ) {
        enqueue(Jhlog.TYPE_IO, if (mainThread) LogEventLane.CRITICAL else LogEventLane.BULK) {
            PendingIoEvent(
                producer.context.capture(),
                operation,
                durationUs,
                bytes,
                mainThread,
                sourceId,
                sourceName,
                outcome,
                bytesKnown,
            )
        }
    }

    fun worker(
        workerId: Long,
        workerName: String?,
        instanceId: Long,
        stage: Long,
        outcome: Long,
        durationMs: Long,
        runAttempt: Long,
        generation: Long,
        stopReason: Long,
        flags: Long,
    ) {
        enqueue(Jhlog.TYPE_WORKER, LogEventLane.CRITICAL) {
            PendingWorkerEvent(
                producer.context.capture(),
                workerId,
                workerName,
                instanceId,
                stage,
                outcome,
                durationMs,
                runAttempt,
                generation,
                stopReason,
                flags,
            )
        }
    }

    override fun operation(
        name: String,
        operationId: Long,
        parentId: Long,
        phase: Long,
        kind: Long,
        outcome: Long,
        durationUs: Long,
        budgetUs: Long,
        screen: String?,
        owner: String?,
        attributes: JankHunterOperationAttributes,
    ): Boolean {
        return enqueue(Jhlog.TYPE_OPERATION, LogEventLane.CRITICAL) {
            PendingOperationEvent(
                producer.context.captureOperation(screen, owner, operationId),
                name,
                operationId,
                parentId,
                phase,
                kind,
                outcome,
                durationUs,
                budgetUs,
                attributes,
            )
        }
    }

    internal fun recordCrash() = recordCrashDiagnostic("jankhunter.runtime.crash.count")

    internal fun recordCrashDiagnostic(name: String): Boolean {
        // One cold-path attempt keeps the ordinary inline enqueue bytecode unchanged. As in
        // enqueue, the admission lock guards offer + sequence publication; rejections create no gap.
        if (!producer.admissionLock.tryLock()) return rejectCrashDiagnostic(QualityCounterId.REASON_ADMISSION_CONTENTION)
        try {
            if (!lifecycle.isAccepting()) return rejectCrashDiagnostic(QualityCounterId.REASON_NOT_ACCEPTING)
            if (!producer.eventLanes.hasCapacity(LogEventLane.CRITICAL)) {
                return rejectCrashDiagnostic(QualityCounterId.REASON_QUEUE_FULL)
            }
            val event = PendingCounterEvent(producer.context.capture(), name, 1L)
            event.sequence = producer.acceptedSequence + 1L
            val result = producer.eventLanes.tryOffer(LogEventLane.CRITICAL, event)
            if (result == OfferResult.OFFERED) {
                producer.acceptedSequence = event.sequence
                quality.addAccepted(event.logicalEventCount)
                if (!startWorker()) return false
                producer.requestWorkerWake()
                return true
            }
            event.rejectBeforeAdmission()
            return rejectCrashDiagnostic(
                if (result == OfferResult.FULL) QualityCounterId.REASON_QUEUE_FULL else QualityCounterId.REASON_ADMISSION_CONTENTION,
            )
        } finally {
            producer.admissionLock.unlock()
        }
    }

    private fun rejectCrashDiagnostic(reason: Int): Boolean {
        if (reason == QualityCounterId.REASON_ADMISSION_CONTENTION) {
            quality.add(QualityCounterId.WRITER_ADMISSION_CONTENTION_TOTAL)
        }
        quality.addRejected(Jhlog.TYPE_COUNTER, reason)
        return false
    }

    fun counter(name: String?, value: Long) {
        if (value < 0L) {
            recordQuality(QualityCounterId.INVALID_METRIC)
            return
        }
        enqueue(Jhlog.TYPE_COUNTER, metricLane(name)) {
            PendingCounterEvent(producer.context.capture(), name, value)
        }
    }

    fun stableCounters(batch: StableCounterBatch): Boolean {
        if (batch.size <= 0) return true
        return enqueue(Jhlog.TYPE_COUNTER, LogEventLane.BULK, batch.logicalEventCount()) {
            producer.stableCountersEventPool.acquire(producer.context.capture(), batch)
        }
    }

    fun gauge(
        name: String?,
        value: Long,
        count: Long = 1L,
        sum: Long = value,
        max: Long = value,
        mode: MetricAggregationMode = MetricAggregationMode.AVERAGE,
    ) {
        if (value < 0L || count < 0L || sum < 0L || max < 0L) {
            recordQuality(QualityCounterId.INVALID_METRIC)
            return
        }
        enqueue(Jhlog.TYPE_GAUGE, metricLane(name)) {
            PendingGaugeEvent(producer.context.capture(), name, value, count, sum, max, mode)
        }
    }

    fun gaugeWide(name: String?, value: Long, count: Long, sum: Long, max: Long, mode: MetricAggregationMode, sumHigh: Long) {
        if (value < 0L || count <= 0L || max < 0L || sumHigh < 0L || sumHigh >= count) {
            recordQuality(QualityCounterId.INVALID_METRIC)
            return
        }
        enqueue(Jhlog.TYPE_GAUGE, metricLane(name)) {
            PendingGaugeEvent(producer.context.capture(), name, value, count, sum, max, mode, sumHigh)
        }
    }

    fun logSpam(
        screen: String?,
        owner: String?,
        operationId: Long,
        source: String?,
        level: Int,
        count: Long,
    ): Boolean {
        return enqueue(Jhlog.TYPE_LOG_SPAM, LogEventLane.BULK, count) {
            PendingLogSpamEvent(
                producer.context.captureOperation(screen, owner, operationId),
                screen,
                owner,
                operationId,
                source,
                level,
                count,
            )
        }
    }

    fun problemWindow(
        screen: String?,
        owner: String?,
        kind: String?,
        windowMs: Long,
        count: Long,
        maxMs: Long,
        foreground: Boolean = false,
    ) {
        enqueue(Jhlog.TYPE_PROBLEM, LogEventLane.CRITICAL) {
            PendingProblemEvent(
                producer.context.capture(),
                screen,
                owner,
                kind,
                windowMs,
                count,
                maxMs,
                foreground,
            )
        }
    }

    fun runtimeCalls(batch: RuntimeCallBatch): Boolean {
        if (batch.size <= 0) return true
        return enqueue(Jhlog.TYPE_RUNTIME_CALL, LogEventLane.BULK, batch.logicalEventCount()) {
            producer.runtimeCallsEventPool.acquire(producer.context.capture(), batch)
        }
    }

    /** Quality state is cumulative, lock-free, and intentionally bypasses the bounded data queue. */
    fun recordQuality(counterId: Int, delta: Long = 1L) {
        quality.add(counterId, delta)
    }

    internal fun recordQualityOnce(counterId: Int, value: Long) = quality.recordOnce(counterId, value)

    internal fun isAcceptingEvents(): Boolean = lifecycle.isAccepting()

    /** Internal primitive accounting only: must finish before a terminal quality snapshot is sealed. */
    internal fun bindCollectionEndObserver(observer: () -> Unit) = collectionEndLock.withLock {
        check(collectionEndObserver == null) { "Collection observer already bound" }
        if (lifecycle.isAccepting()) collectionEndObserver = observer else observer()
    }

    private fun stopAccepting() = collectionEndLock.withLock { finishCollectionAccounting() }

    private fun stopAccepting(deadlineNs: Long): Boolean {
        val acquired = try {
            collectionEndLock.tryLock((deadlineNs - System.nanoTime()).coerceAtLeast(0L), TimeUnit.NANOSECONDS)
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
            false
        }
        if (!acquired) return false
        try {
            finishCollectionAccounting()
        } finally {
            collectionEndLock.unlock()
        }
        return true
    }

    private fun finishCollectionAccounting() {
        val observer = collectionEndObserver
        collectionEndObserver = null
        try {
            // Concurrent close must not let the consumer seal before this callback has finished.
            observer?.invoke()
        } finally {
            lifecycle.stopAccepting()
        }
    }

    internal fun terminalFailureCause(): Throwable? = lifecycle.terminalFailure()

    fun flush() {
        controls.flush()
    }

    fun flushBlocking(timeoutMs: Long = DEFAULT_BLOCKING_TIMEOUT_MS): Boolean {
        return controls.submitBlocking(timeoutMs, writeLogGrowth = false)
    }

    internal fun writeLogGrowthSummaryBlocking(timeoutMs: Long = DEFAULT_BLOCKING_TIMEOUT_MS): Boolean {
        if (!config.logGrowthAnalyticsEnabled()) return false
        return controls.submitBlocking(
            timeoutMs,
            writeLogGrowth = true,
            requireLogGrowth = true,
        )
    }

    internal fun captureSnapshotBlocking(
        timeoutMs: Long = DEFAULT_BLOCKING_TIMEOUT_MS,
    ): LogSnapshotResult? {
        var snapshot: LogSnapshotResult? = null
        val succeeded = controls.submitBlocking(
            timeoutMs = timeoutMs,
            writeLogGrowth = config.logGrowthAnalyticsEnabled(),
            sealSnapshot = true,
            onComplete = { request -> snapshot = request.snapshot },
        )
        return snapshot.takeIf { succeeded }
    }

    internal fun switchBinaryStorageBlocking(
        storage: JankHunterBinaryStorage?,
        timeoutMs: Long = DEFAULT_BLOCKING_TIMEOUT_MS,
        onComplete: (JankHunterStorageSwitchResult) -> Unit = {},
    ): JankHunterStorageSwitchResult {
        if (!lifecycle.isAccepting()) return JankHunterStorageSwitchResult.NOT_ACCEPTING
        if (Thread.currentThread() === consumer.worker) return JankHunterStorageSwitchResult.FAILED
        if (consumer.binaryStorage === storage) return JankHunterStorageSwitchResult.ALREADY_ACTIVE
        var result = JankHunterStorageSwitchResult.FAILED
        var completed = false
        val submission = controls.submitBlockingOutcome(
            timeoutMs = timeoutMs,
            writeLogGrowth = config.logGrowthAnalyticsEnabled(),
            storageSwitch = StorageSwitchRequest(storage),
            onComplete = { request ->
                result = request.storageSwitchResult
                completed = true
            },
            onDeferredComplete = { request -> onComplete(request.storageSwitchResult) },
        )
        return if (completed) result else when {
            !lifecycle.isAccepting() -> JankHunterStorageSwitchResult.NOT_ACCEPTING
            submission == AsyncControlSubmissionOutcome.IN_PROGRESS -> JankHunterStorageSwitchResult.IN_PROGRESS
            else -> JankHunterStorageSwitchResult.TIMED_OUT
        }
    }

    internal fun logGrowthSummary(): JankHunterLogGrowthSummary? =
        logGrowthManager?.summary(consumer.writer?.logGrowthStats())

    fun close(timeoutMs: Long = closeTimeoutMs()): Boolean {
        val deadlineNs = monotonicDeadlineAfterMillis(timeoutMs.coerceAtLeast(0L))
        if (!stopAccepting(deadlineNs)) {
            quality.add(QualityCounterId.CLOSE_TIMEOUT_TOTAL)
            return false
        }
        producer.requestWorkerWake()
        // The worker reference may be published before Thread.start returns. Admission is the
        // completion barrier for both event publication and lazy worker startup, with one deadline.
        val acquired = try {
            producer.admissionLock.tryLock((deadlineNs - System.nanoTime()).coerceAtLeast(0L), TimeUnit.NANOSECONDS)
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
            false
        }
        if (!acquired) {
            quality.add(QualityCounterId.CLOSE_TIMEOUT_TOTAL)
            return false
        }
        val activeWorker = try {
            consumer.worker
        } finally {
            producer.admissionLock.unlock()
        }
        if (activeWorker == null) {
            finishSession(null)
            return true
        }
        if (Thread.currentThread() === activeWorker) return true
        val finished = waitForWorker(activeWorker, deadlineNs)
        if (!finished) quality.add(QualityCounterId.CLOSE_TIMEOUT_TOTAL)
        return finished
    }

    private inline fun enqueue(
        recordType: Int,
        lane: LogEventLane,
        logicalEventCount: Long = 1L,
        createEvent: () -> PendingLogEvent,
    ): Boolean {
        val exact = config.exactEventCollectionEnabled()
        val admissionBudgetNs = if (exact) {
            val waitMs = if (isRuntimeMainThread()) {
                config.mainThreadAdmissionWaitMs()
            } else {
                config.backgroundAdmissionWaitMs()
            }
            TimeUnit.MILLISECONDS.toNanos(waitMs)
        } else {
            0L
        }
        var blockedAtNs = 0L
        var contentionRecorded = false
        while (true) {
            var retryReason = QualityCounterId.REASON_ADMISSION_CONTENTION
            val acquired = producer.admissionLock.tryLock()
            if (!acquired) {
                if (!contentionRecorded) {
                    contentionRecorded = true
                    quality.add(QualityCounterId.WRITER_ADMISSION_CONTENTION_TOTAL)
                }
                if (blockedAtNs == 0L) {
                    blockedAtNs = System.nanoTime()
                    quality.add(QualityCounterId.WRITER_BACKPRESSURE_COUNT)
                }
            }
            if (acquired) {
                try {
                    if (!lifecycle.isAccepting()) {
                        recordWriterBackpressure(blockedAtNs)
                        quality.addRejected(
                            recordType,
                            QualityCounterId.REASON_NOT_ACCEPTING,
                            logicalEventCount,
                        )
                        return false
                    }
                    if (producer.eventLanes.hasCapacity(lane)) {
                        val event = createEvent()
                        val sequence = producer.acceptedSequence + 1L
                        event.sequence = sequence
                        when (producer.eventLanes.tryOffer(lane, event)) {
                            OfferResult.OFFERED -> {
                                producer.acceptedSequence = sequence
                                quality.addAccepted(event.logicalEventCount)
                                recordWriterBackpressure(blockedAtNs)
                                if (!startWorker()) return false
                                producer.requestWorkerWake()
                                return true
                            }
                            OfferResult.FULL -> {
                                event.rejectBeforeAdmission()
                                retryReason = QualityCounterId.REASON_QUEUE_FULL
                            }
                            OfferResult.CONTENDED -> event.rejectBeforeAdmission()
                        }
                    } else {
                        retryReason = QualityCounterId.REASON_QUEUE_FULL
                    }
                } finally {
                    producer.admissionLock.unlock()
                }
            }
            if (retryReason == QualityCounterId.REASON_ADMISSION_CONTENTION && !contentionRecorded) {
                contentionRecorded = true
                quality.add(QualityCounterId.WRITER_ADMISSION_CONTENTION_TOTAL)
            }
            val waitExpired = blockedAtNs != 0L &&
                System.nanoTime() - blockedAtNs >= admissionBudgetNs
            if (!exact || admissionBudgetNs == 0L || waitExpired) {
                recordWriterBackpressure(blockedAtNs)
                quality.addRejected(recordType, retryReason, logicalEventCount)
                return false
            }
            if (blockedAtNs == 0L) {
                blockedAtNs = System.nanoTime()
                quality.add(QualityCounterId.WRITER_BACKPRESSURE_COUNT)
            }
            val remainingNs = admissionBudgetNs - (System.nanoTime() - blockedAtNs)
            if (remainingNs > 0L) {
                LockSupport.parkNanos(minOf(EXACT_BACKPRESSURE_PARK_NS, remainingNs))
            }
        }
    }

    private fun recordWriterBackpressure(blockedAtNs: Long) {
        if (blockedAtNs == 0L) return
        quality.add(
            QualityCounterId.WRITER_BACKPRESSURE_NANOS,
            (System.nanoTime() - blockedAtNs).coerceAtLeast(1L),
        )
    }

    private fun beginControlSubmission(startIfNeeded: Boolean, deadlineNs: Long): Long {
        val acquired = producer.admissionLock.tryLock() ||
            producer.admissionLock.tryLock((deadlineNs - System.nanoTime()).coerceAtLeast(0L), TimeUnit.NANOSECONDS)
        if (!acquired) return AsyncWriterControlCoordinator.ADMISSION_TIMED_OUT
        return try {
            if (!lifecycle.isAccepting()) return AsyncWriterControlCoordinator.NOT_ACCEPTING
            if (consumer.worker == null && (!startIfNeeded || !startWorker())) {
                return AsyncWriterControlCoordinator.NO_WORK
            }
            controls.beginLaneSubmission()
            producer.acceptedSequence
        } finally {
            producer.admissionLock.unlock()
        }
    }

    private fun startWorker(): Boolean {
        if (consumer.worker != null) return true
        return try {
            val startedWorker = workerThreadFactory(Runnable(::runWorkerFailOpen), "JankHunterWriter").apply {
                isDaemon = true
            }
            consumer.worker = startedWorker
            startedWorker.start()
            true
        } catch (error: Throwable) {
            consumer.worker = null
            if (!error.isFatal()) quality.add(QualityCounterId.WRITER_IO_ERROR_TOTAL)
            terminateWithoutWorker(error)
            if (error.isFatal()) throw error
            false
        }
    }

    private fun metricLane(name: String?): LogEventLane {
        return if (isCriticalMetricName(name)) {
            LogEventLane.CRITICAL
        } else {
            LogEventLane.BULK
        }
    }

    private fun closeTimeoutMs(): Long = DEFAULT_BLOCKING_TIMEOUT_MS

    private fun waitForWorker(activeWorker: Thread, deadlineNs: Long): Boolean {
        var interrupted = false
        while (activeWorker.isAlive) {
            val remainingNs = deadlineNs - System.nanoTime()
            if (remainingNs <= 0L) break
            val remainingMs = TimeUnit.NANOSECONDS.toMillis(remainingNs).coerceAtLeast(1L)
            try {
                activeWorker.join(minOf(CLOSE_JOIN_POLL_MS, remainingMs))
            } catch (_: InterruptedException) {
                interrupted = true
                break
            }
        }
        if (interrupted) Thread.currentThread().interrupt()
        return !activeWorker.isAlive
    }

    private fun runWorkerFailOpen() {
        try {
            loop()
        } catch (error: Throwable) {
            // Instrumentation must never route a background writer failure to the host app's
            // uncaught-exception handler. Gate admission here as well because a fatal failure can
            // escape from loop cleanup after its normal terminal path has already run.
            terminateAfterEscapedFailure(error)
        } finally {
            deliverTerminalCallback()
        }
    }

    private fun loop() {
        try {
            if (!openSessionWriter()) return
            var initialCleanupPending = true
            while (
                lifecycle.isRunning() ||
                hasPendingEvents() ||
                controls.hasPending() ||
                controls.hasSubmitters() ||
                // A publisher admitted before stop may still be constructing/publishing its event.
                producer.admissionLock.isLocked
            ) {
                if (consumer.writer == null) {
                    // A size or I/O failure rejects queued events, so controls targeting their
                    // sequence can never become ready. Complete them as failed instead of keeping
                    // the daemon alive until every caller times out.
                    controls.failPending()
                    if (controls.hasSubmitters()) LockSupport.parkNanos(CONTROL_DRAIN_PARK_NS)
                    continue
                }
                val event = pollNextEvent()
                if (event != null) {
                    val eventSequence = event.sequence
                    try {
                        if (writeEvent(event)) {
                            consumer.completedSequence = eventSequence
                        }
                    } finally {
                        event.recycle()
                    }
                }
                processReadyControls()
                flushIfNeeded(force = false)
                if (event != null && initialCleanupPending) {
                    initialCleanupPending = false
                    consumer.retention.enforce(consumer.writer, consumer.binaryStorage, consumer.runId)
                }
            }
            if (consumer.writer != null) {
                processReadyControls()
                flushIfNeeded(force = true)
            }
        } catch (error: Throwable) {
            if (error.isFatal()) {
                stopAndDrain(currentEvent = null, reason = QualityCounterId.REASON_IO_LOST, failure = error)
                throw error
            }
            quality.add(QualityCounterId.WRITER_IO_ERROR_TOTAL)
            stopAndDrain(currentEvent = null, reason = QualityCounterId.REASON_IO_LOST, failure = error)
            consumer.writer?.let(::sealIoFailure)
        } finally {
            controls.failAfterAdmissionClosed()
            closeSessionWriter()
        }
    }

    private fun openSessionWriter(
        storage: JankHunterBinaryStorage? = consumer.binaryStorage,
        terminalOnFailure: Boolean = true,
    ): Boolean {
        val activeSessionFactory = sessionFactory ?: createSessionFactory().also { sessionFactory = it }
        return try {
            if (consumer.runCohortLease == null) {
                val authoritativeStoragePaths = storage?.let { activeStorage ->
                    runCatching { activeStorage.listFiles() }.getOrNull()
                }
                consumer.runCohortLease = ProcessRunCohort.join(
                    directory,
                    sessionLocalDate,
                    authoritativeStoragePaths,
                ).also { lease ->
                    consumer.runId = lease.runId()
                    consumer.runLocalDate = lease.localDate()
                    consumer.dailySessionIndex = lease.dailySessionIndex()
                }
            }
            val opened = activeSessionFactory.open(
                localDate = consumer.runLocalDate,
                dailySessionIndex = consumer.dailySessionIndex,
                runId = consumer.runId,
                sessionId = consumer.sessionId,
                segmentIndex = consumer.segmentIndex,
                previousSegmentDigest = consumer.previousSegmentDigest,
                baseStats = consumer.completedSegmentStats,
                segmentStartElapsedUs = if (consumer.segmentIndex == 0L) collectorStartElapsedUs else nowElapsedUs(),
                segmentStartUnixMs = if (consumer.segmentIndex == 0L) {
                    sessionStartMs
                } else {
                    currentTimeMs.getAsLong().coerceAtLeast(0L)
                },
                storage = storage,
            )
            consumer.segmentLedger.register(opened, storage)
            consumer.binaryStorage = storage
            consumer.writer = opened.writer
            true
        } catch (error: StorageBudgetExhaustedException) {
            if (terminalOnFailure) {
                stopAndDrain(currentEvent = null, reason = QualityCounterId.REASON_STORAGE_BUDGET, failure = error)
            }
            false
        } catch (error: Throwable) {
            if (error.isFatal()) throw error
            quality.add(QualityCounterId.WRITER_IO_ERROR_TOTAL)
            if (terminalOnFailure) {
                stopAndDrain(currentEvent = null, reason = QualityCounterId.REASON_IO_LOST, failure = error)
            }
            false
        }
    }

    private fun createSessionFactory(): AsyncLogSessionFactory {
        logGrowthManager = prepareSession()
        return AsyncLogSessionFactory(
            directory = directory,
            config = config,
            processName = processName,
            expectedProcesses = expectedProcesses,
            rosterDeclarationComplete = rosterDeclarationComplete,
            collectorStartElapsedUs = collectorStartElapsedUs,
            quality = quality,
            logGrowthManager = logGrowthManager,
            buildIdentity = buildIdentity,
        )
    }

    private fun hasPendingEvents(): Boolean = producer.eventLanes.hasEvents()

    private fun pollNextEvent(): PendingLogEvent? {
        producer.eventLanes.pollNext()?.let { return it }
        producer.prepareWorkerWait()
        producer.eventLanes.pollNext()?.let { return it }
        if (controls.hasPending() || !lifecycle.isRunning()) return null
        val available = try {
            val remainingNs = flushIntervalNs - (System.nanoTime() - consumer.lastFlushAtNs)
            producer.queuedEvents.tryAcquire(remainingNs, TimeUnit.NANOSECONDS)
        } catch (_: InterruptedException) {
            false
        }
        if (!available) return null
        return producer.eventLanes.pollNext()
    }

    private fun writeEvent(event: PendingLogEvent): Boolean {
        var rotatedWithoutProgress = false
        while (true) {
            val activeWriter = consumer.writer ?: return false
            val remainingBeforeWrite = event.remainingEventCount
            try {
                event.writeTo(activeWriter)
                return true
            } catch (error: StorageBudgetExhaustedException) {
                quality.addRejected(
                    event.recordType,
                    QualityCounterId.REASON_STORAGE_BUDGET,
                    event.remainingEventCount,
                )
                stopAndDrain(currentEvent = null, reason = QualityCounterId.REASON_STORAGE_BUDGET, failure = error)
                try {
                    activeWriter.sealStorageBudget()
                } catch (sealError: Throwable) {
                    if (sealError.isFatal()) throw sealError
                    activeWriter.abort()
                } finally {
                    finishSession(activeWriter)
                }
                return false
            } catch (error: LogSizeLimitReachedException) {
                val madeProgress = event.remainingEventCount < remainingBeforeWrite
                if (!madeProgress && rotatedWithoutProgress) {
                    stopAndDrain(event, QualityCounterId.REASON_SIZE_LIMIT, error)
                    try {
                        activeWriter.sealSizeLimit()
                    } catch (sealError: Throwable) {
                        if (sealError.isFatal()) throw sealError
                        activeWriter.abort()
                    } finally {
                        finishSession(activeWriter)
                    }
                    return false
                }
                rotatedWithoutProgress = !madeProgress
                if (rotateSegment(activeWriter)) continue
                if (lifecycle.terminalReason() == QualityCounterId.REASON_STORAGE_BUDGET) {
                    quality.addRejected(
                        event.recordType,
                        QualityCounterId.REASON_STORAGE_BUDGET,
                        event.remainingEventCount,
                    )
                } else {
                    quality.addRejected(event.recordType, QualityCounterId.REASON_IO_LOST, event.remainingEventCount)
                }
                return false
            } catch (error: Throwable) {
                if (error.isFatal()) throw error
                quality.add(QualityCounterId.WRITER_IO_ERROR_TOTAL)
                stopAndDrain(event, QualityCounterId.REASON_IO_LOST, error)
                sealIoFailure(activeWriter)
                return false
            }
        }
    }

    private fun rotateSegment(activeWriter: BinaryLogWriter): Boolean {
        return try {
            sealSegment(activeWriter)
            openSessionWriter()
        } catch (error: Throwable) {
            if (error.isFatal()) throw error
            quality.add(QualityCounterId.WRITER_IO_ERROR_TOTAL)
            if (consumer.writer === activeWriter) activeWriter.abort()
            stopAndDrain(currentEvent = null, reason = QualityCounterId.REASON_IO_LOST, failure = error)
            false
        }
    }

    private fun switchStorage(target: JankHunterBinaryStorage?): JankHunterStorageSwitchResult {
        if (consumer.binaryStorage === target) return JankHunterStorageSwitchResult.ALREADY_ACTIVE
        val activeWriter = consumer.writer ?: return JankHunterStorageSwitchResult.NOT_STARTED
        val previousStorage = consumer.binaryStorage
        return try {
            sealSegment(activeWriter)
            val handoff = consumer.segmentLedger.prepareHandoff(target)
            if (!openSessionWriter(target, terminalOnFailure = false)) {
                consumer.segmentLedger.rollbackHandoff(target, handoff)
                if (!openSessionWriter(previousStorage, terminalOnFailure = false)) {
                    stopAndDrain(
                        currentEvent = null,
                        reason = QualityCounterId.REASON_IO_LOST,
                        failure = IOException("Cannot reopen Jank Hunter storage after failed handoff"),
                    )
                }
                JankHunterStorageSwitchResult.FAILED
            } else {
                consumer.segmentLedger.commitHandoff(handoff)
                JankHunterStorageSwitchResult.SWITCHED
            }
        } catch (error: Throwable) {
            if (error.isFatal()) throw error
            quality.add(QualityCounterId.WRITER_IO_ERROR_TOTAL)
            if (consumer.writer == null && !openSessionWriter(previousStorage, terminalOnFailure = false)) {
                stopAndDrain(currentEvent = null, reason = QualityCounterId.REASON_IO_LOST, failure = error)
            }
            JankHunterStorageSwitchResult.FAILED
        }
    }

    private fun sealSegment(activeWriter: BinaryLogWriter) {
        activeWriter.sealRotation()
        consumer.previousSegmentDigest = activeWriter.sealedDigest()
            ?: throw IOException("Sealed Jank Hunter segment has no SHA-256 digest")
        val sealedStats = activeWriter.logGrowthStats()
        consumer.completedSegmentStats = sealedStats.copy(
            segmentRotationCount = saturatedAdd(sealedStats.segmentRotationCount, 1L),
        )
        consumer.segmentLedger.seal(activeWriter.path, consumer.binaryStorage)
        consumer.retention.enforce(activeWriter, consumer.binaryStorage, consumer.runId)
        if (consumer.writer === activeWriter) consumer.writer = null
        if (consumer.segmentIndex == Long.MAX_VALUE) throw IOException("Jank Hunter segment index exhausted")
        consumer.segmentIndex++
    }

    private fun processReadyControls() {
        while (true) {
            val request = controls.claimReady(consumer.completedSequence) ?: return
            val flushed = flushIfNeeded(force = true)
            val succeeded = when {
                !flushed -> false
                !request.writeLogGrowth -> true
                logGrowthManager != null -> consumer.writer?.writeLogGrowthSummary() == true
                request.requireLogGrowth -> false
                else -> true
            }
            val switchSucceeded = if (succeeded && request.storageSwitch != null) {
                request.storageSwitchResult = switchStorage(request.storageSwitch.storage)
                request.storageSwitchResult == JankHunterStorageSwitchResult.SWITCHED ||
                    request.storageSwitchResult == JankHunterStorageSwitchResult.ALREADY_ACTIVE
            } else {
                succeeded
            }
            val snapshotSucceeded = if (switchSucceeded && request.sealSnapshot) {
                val capturedAtMs = currentTimeMs.getAsLong().coerceAtLeast(0L)
                val activeWriter = consumer.writer
                if (activeWriter != null && rotateSegment(activeWriter)) {
                    request.snapshot = LogSnapshotResult(
                        capturedAtMs = capturedAtMs,
                        logPaths = consumer.segmentLedger.completedPaths(),
                    )
                    true
                } else {
                    false
                }
            } else {
                switchSucceeded
            }
            request.complete(snapshotSucceeded)
        }
    }

    private fun flushIfNeeded(force: Boolean): Boolean {
        consumer.runtimeHookFailures.sync()
        val now = System.nanoTime()
        if (!force && now - consumer.lastFlushAtNs < flushIntervalNs) return true
        val activeWriter = consumer.writer ?: return false
        return try {
            if (logGrowthManager == null) {
                activeWriter.flush()
            } else {
                if (!activeWriter.writeLogGrowthSummary()) {
                    activeWriter.flush()
                }
            }
            consumer.lastFlushAtNs = now
            true
        } catch (error: StorageBudgetExhaustedException) {
            stopAndDrain(currentEvent = null, reason = QualityCounterId.REASON_STORAGE_BUDGET, failure = error)
            try {
                activeWriter.sealStorageBudget()
            } catch (sealError: Throwable) {
                if (sealError.isFatal()) throw sealError
                activeWriter.abort()
            } finally {
                finishSession(activeWriter)
            }
            false
        } catch (error: LogSizeLimitReachedException) {
            rotateSegment(activeWriter)
        } catch (error: Throwable) {
            if (error.isFatal()) throw error
            quality.add(QualityCounterId.WRITER_IO_ERROR_TOTAL)
            stopAndDrain(currentEvent = null, reason = QualityCounterId.REASON_IO_LOST, failure = error)
            sealIoFailure(activeWriter)
            false
        }
    }

    private fun sealIoFailure(activeWriter: BinaryLogWriter) {
        try {
            activeWriter.sealIoError()
        } catch (error: Throwable) {
            if (error.isFatal()) throw error
            activeWriter.abort()
        } finally {
            finishSession(activeWriter)
        }
    }

    private fun stopAndDrain(
        currentEvent: PendingLogEvent?,
        reason: Int,
        failure: Throwable? = null,
    ) {
        lifecycle.recordTermination(reason, failure)
        producer.admissionLock.withLock {
            stopAccepting()
            rejectAllQueuedLocked(reason)
        }
        currentEvent?.let { event ->
            quality.addRejected(event.recordType, reason, event.remainingEventCount)
        }
    }

    /** Called while the producer admission lock is held when the worker could not be started. */
    private fun terminateWithoutWorker(error: Throwable) {
        lifecycle.recordTermination(QualityCounterId.REASON_IO_LOST, error)
        stopAccepting()
        try {
            rejectAllQueuedLocked(QualityCounterId.REASON_IO_LOST)
        } catch (_: Throwable) {
            // Admission is already closed; quality accounting is best effort under VM pressure.
        }
        controls.failAfterAdmissionClosed()
    }

    private fun terminateAfterEscapedFailure(error: Throwable) {
        try {
            stopAndDrain(
                currentEvent = null,
                reason = QualityCounterId.REASON_IO_LOST,
                failure = error,
            )
        } catch (_: Throwable) {
            lifecycle.recordTermination(QualityCounterId.REASON_IO_LOST, error)
            stopAccepting()
        }
        try {
            controls.failAfterAdmissionClosed()
        } catch (_: Throwable) {
            // Keep the fail-open boundary intact even when the VM cannot finish diagnostics.
        }
    }

    private fun deliverTerminalCallback() {
        lifecycle.deliverTerminalCallback(this, onTerminalStop)
    }

    private fun rejectAllQueuedLocked(reason: Int) {
        while (true) {
            val event = producer.eventLanes.pollNext() ?: break
            try {
                quality.addRejected(event.recordType, reason, event.remainingEventCount)
            } finally {
                event.recycle()
            }
        }
        producer.prepareWorkerWait()
    }

    private fun closeSessionWriter() {
        val activeWriter = consumer.writer
        if (activeWriter == null) {
            finishSession(null)
            return
        }
        try {
            consumer.retention.enforce(activeWriter, consumer.binaryStorage, consumer.runId)
            activeWriter.close(Jhlog.SEGMENT_END_SHUTDOWN)
        } catch (error: Throwable) {
            if (error.isFatal()) throw error
            quality.add(QualityCounterId.WRITER_IO_ERROR_TOTAL)
            activeWriter.abort()
        } finally {
            finishSession(activeWriter)
        }
    }

    private fun finishSession(activeWriter: BinaryLogWriter?) {
        if (!lifecycle.markSessionFinished()) return
        if (consumer.writer === activeWriter) consumer.writer = null
        try {
            if (activeWriter != null) consumer.retention.enforce(activeWriter, consumer.binaryStorage, consumer.runId)
        } finally {
            consumer.segmentLedger.finish()
            consumer.runCohortLease?.close()
            consumer.runCohortLease = null
        }
    }

    companion object {
        private const val CLOSE_JOIN_POLL_MS = 250L
        private const val DEFAULT_BLOCKING_TIMEOUT_MS = 1_000L
        private const val CONTROL_DRAIN_PARK_NS = 100_000L
        private const val EXACT_BACKPRESSURE_PARK_NS = 100_000L
        private const val APP_LIFECYCLE_METRIC_PREFIX = "app.lifecycle."
        private const val SCREEN_LIFECYCLE_METRIC_MARKER = ".lifecycle."
        private const val RUNTIME_SESSION_METRIC_PREFIX = "jankhunter.runtime.session."
        private const val RUNTIME_CRASH_METRIC_PREFIX = "jankhunter.runtime.crash."
        private const val HEAP_DUMP_METRIC_PREFIX = "jankhunter.heap_dump."

        internal fun isCriticalMetricName(name: String?): Boolean {
            if (name == null) return false
            return name.startsWith(APP_LIFECYCLE_METRIC_PREFIX) ||
                name.contains(SCREEN_LIFECYCLE_METRIC_MARKER) ||
                name.startsWith(RUNTIME_SESSION_METRIC_PREFIX) ||
                name.startsWith(RUNTIME_CRASH_METRIC_PREFIX) ||
                name.startsWith(HEAP_DUMP_METRIC_PREFIX)
        }

        private fun nowElapsedUs(): Long = SystemClock.elapsedRealtimeNanos().coerceAtLeast(0L) / 1_000L

        private fun Throwable.isFatal(): Boolean = this is VirtualMachineError || this is ThreadDeath
    }
}
