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
import io.jankhunter.runtime.internal.system.RetentionEvidence
import java.io.File
import java.io.IOException
import java.util.concurrent.Semaphore
import java.util.concurrent.TimeUnit
import java.util.concurrent.locks.LockSupport
import java.util.concurrent.locks.ReentrantLock
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
    private val logGrowthManager: LogGrowthManager?,
    private val onTerminalStop: AsyncWriterTerminalObserver,
) : OperationEventSink {
    private val controlLane = AsyncControlLane(CONTROL_QUEUE_CAPACITY)
    private val queuedEvents = Semaphore(0)
    private val admissionLock = ReentrantLock()
    private val lifecycle = AsyncWriterLifecycle()
    private val producerContext = ProducerContextTracker()
    @Volatile
    private var binaryStorage: JankHunterBinaryStorage? = config.binaryStorage()

    private val eventLanes = AsyncEventLanes(config.maxQueueSize())
    private val sessionFactory = AsyncLogSessionFactory(
        directory = directory,
        config = config,
        processName = processName,
        expectedProcesses = expectedProcesses,
        rosterDeclarationComplete = rosterDeclarationComplete,
        collectorStartElapsedUs = collectorStartElapsedUs,
        quality = quality,
        logGrowthManager = logGrowthManager,
    )

    private var acceptedSequence = 0L
    private var completedSequence = 0L
    private val segmentLedger = SessionSegmentLedger(directory, processName)
    private var writer: BinaryLogWriter? = null
    private var runCohortLease: ProcessRunCohort.Lease? = null
    private var runId = ByteArray(0)
    private var runLocalDate = sessionLocalDate
    private var dailySessionIndex = 0L
    private val sessionId = BinaryLogFileHeader.randomId()
    private var segmentIndex = 0L
    private var previousSegmentDigest = ByteArray(0)
    private var completedSegmentStats = LogContainerStats.EMPTY
    private var lastFlushAtMs = SystemClock.elapsedRealtime()
    private val runtimeHookFailures = RuntimeHookFailureQualitySynchronizer(quality)
    private val retention = SessionLogRetentionCoordinator(directory, config, quality)

    @Volatile
    private var worker: Thread? = null

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
            PendingLogEvent.Session(
                producerContext.capture(),
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
        producerContext.update(screen, owner, operationId)
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
    ) {
        enqueue(Jhlog.TYPE_DEVICE_CONTEXT, LogEventLane.BULK) {
            PendingLogEvent.DeviceContext(
                producerContext.capture(),
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
            PendingLogEvent.Http(
                producerContext.capture(screen, owner),
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
            PendingLogEvent.WebSocket(producerContext.capture(screen, owner), owner, event)
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
            PendingLogEvent.Database(
                producerContext.capture(), sourceId, sourceName, query, framework,
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
            PendingLogEvent.DatabaseTransaction(
                producerContext.capture(), sourceId, sourceName, transactionId, parentId, stage, mode,
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
            PendingLogEvent.ProcessState(
                producerContext.capture(),
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
            PendingLogEvent.AndroidComponent(
                producerContext.capture(),
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
            PendingLogEvent.BinderTransaction(
                producerContext.capture(),
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
    ) {
        enqueue(Jhlog.TYPE_STALL, LogEventLane.CRITICAL) {
            PendingLogEvent.Stall(
                producerContext.capture(screen, owner),
                screen,
                owner,
                stackHint,
                durationMs,
                foreground,
            )
        }
    }

    fun memory(pssKb: Long, javaHeapKb: Long, nativeHeapKb: Long, foreground: Boolean) {
        enqueue(Jhlog.TYPE_MEMORY, LogEventLane.BULK) {
            PendingLogEvent.Memory(producerContext.capture(), pssKb, javaHeapKb, nativeHeapKb, foreground)
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
    ) {
        enqueue(Jhlog.TYPE_RETAINED, LogEventLane.CRITICAL) {
            PendingLogEvent.Retained(
                producerContext.capture(),
                screen,
                owner,
                className,
                holder,
                ageMs,
                count,
                foreground,
                evidence.wireValue,
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
            PendingLogEvent.UiWindow(
                producerContext.capture(),
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
            PendingLogEvent.ProcessExit(
                producerContext.capture(),
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
            PendingLogEvent.IO(
                producerContext.capture(),
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
            PendingLogEvent.Worker(
                producerContext.capture(),
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
            PendingLogEvent.Operation(
                producerContext.captureOperation(screen, owner, operationId),
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

    fun counter(name: String?, value: Long) {
        if (value < 0L) {
            recordQuality(QualityCounterId.INVALID_METRIC)
            return
        }
        enqueue(Jhlog.TYPE_COUNTER, metricLane(name)) {
            PendingLogEvent.Counter(producerContext.capture(), name, value)
        }
    }

    fun stableCounters(batch: StableCounterBatch): Boolean {
        if (batch.size <= 0) return true
        return enqueue(Jhlog.TYPE_COUNTER, LogEventLane.BULK, batch.logicalEventCount()) {
            PendingLogEvent.StableCounters(producerContext.capture(), batch)
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
            PendingLogEvent.Gauge(producerContext.capture(), name, value, count, sum, max, mode)
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
            PendingLogEvent.LogSpam(
                producerContext.captureOperation(screen, owner, operationId),
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
            PendingLogEvent.Problem(
                producerContext.capture(),
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
            PendingLogEvent.RuntimeCalls(producerContext.capture(), batch)
        }
    }

    /** Quality state is cumulative, lock-free, and intentionally bypasses the bounded data queue. */
    fun recordQuality(counterId: Int, delta: Long = 1L) {
        quality.add(counterId, delta)
    }

    internal fun isAcceptingEvents(): Boolean = lifecycle.isAccepting()

    internal fun terminalFailureCause(): Throwable? = lifecycle.terminalFailure()

    fun flush() {
        val target = beginControlSubmission()
        if (target < 0L) return
        try {
            if (!controlLane.offer(AsyncControlRequest(target, writeLogGrowth = false, blocking = false))) {
                quality.add(QualityCounterId.CONTROL_LANE_FULL_TOTAL)
            }
        } finally {
            controlLane.finishSubmission()
        }
    }

    fun flushBlocking(
        timeoutMs: Long = DEFAULT_BLOCKING_TIMEOUT_MS,
        waitForExactFrontier: Boolean = true,
    ): Boolean {
        return submitBlockingControl(timeoutMs, writeLogGrowth = false, waitForExactFrontier)
    }

    internal fun writeLogGrowthSummaryBlocking(timeoutMs: Long = DEFAULT_BLOCKING_TIMEOUT_MS): Boolean {
        if (logGrowthManager == null) return false
        return submitBlockingControl(timeoutMs, writeLogGrowth = true, waitForExactFrontier = true)
    }

    internal fun captureSnapshotBlocking(
        timeoutMs: Long = DEFAULT_BLOCKING_TIMEOUT_MS,
    ): LogSnapshotResult? {
        var snapshot: LogSnapshotResult? = null
        val succeeded = submitBlockingControl(
            timeoutMs = timeoutMs,
            writeLogGrowth = logGrowthManager != null,
            waitForExactFrontier = true,
            sealSnapshot = true,
            onComplete = { request -> snapshot = request.snapshot },
        )
        return snapshot.takeIf { succeeded }
    }

    internal fun switchBinaryStorageBlocking(
        storage: JankHunterBinaryStorage?,
        timeoutMs: Long = DEFAULT_BLOCKING_TIMEOUT_MS,
    ): JankHunterStorageSwitchResult {
        if (!lifecycle.isAccepting()) return JankHunterStorageSwitchResult.NOT_ACCEPTING
        if (Thread.currentThread() === worker) return JankHunterStorageSwitchResult.FAILED
        if (binaryStorage === storage) return JankHunterStorageSwitchResult.ALREADY_ACTIVE
        var result = JankHunterStorageSwitchResult.FAILED
        var completed = false
        submitBlockingControl(
            timeoutMs = timeoutMs,
            writeLogGrowth = logGrowthManager != null,
            waitForExactFrontier = true,
            storageSwitch = StorageSwitchRequest(storage),
            onComplete = { request ->
                result = request.storageSwitchResult
                completed = true
            },
        )
        return if (completed) result else when {
            !lifecycle.isAccepting() -> JankHunterStorageSwitchResult.NOT_ACCEPTING
            else -> JankHunterStorageSwitchResult.TIMED_OUT
        }
    }

    internal fun logGrowthSummary(): JankHunterLogGrowthSummary? =
        logGrowthManager?.summary(writer?.logGrowthStats())

    private fun submitBlockingControl(
        timeoutMs: Long,
        writeLogGrowth: Boolean,
        waitForExactFrontier: Boolean,
        sealSnapshot: Boolean = false,
        storageSwitch: StorageSwitchRequest? = null,
        onComplete: ((AsyncControlRequest) -> Unit)? = null,
    ): Boolean {
        val target = beginControlSubmission(startIfNeeded = writeLogGrowth || sealSnapshot || storageSwitch != null)
        if (target == CONTROL_NO_WORK) return !writeLogGrowth && !sealSnapshot
        if (target == CONTROL_NOT_ACCEPTING) return false
        if (config.exactEventCollectionEnabled() && waitForExactFrontier) {
            return submitExactBlockingControl(target, writeLogGrowth, sealSnapshot, storageSwitch, onComplete)
        }
        val timeoutNs = TimeUnit.MILLISECONDS.toNanos(timeoutMs.coerceAtLeast(1L))
        val startedAtNs = System.nanoTime()
        val request = AsyncControlRequest(target, writeLogGrowth, sealSnapshot, storageSwitch, blocking = true)
        val admitted = try {
            controlLane.offer(request, timeoutNs, TimeUnit.NANOSECONDS)
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
            quality.add(QualityCounterId.CONTROL_INTERRUPTED_TOTAL)
            return false
        } finally {
            controlLane.finishSubmission()
        }
        if (!admitted) {
            quality.add(QualityCounterId.CONTROL_TIMEOUT_TOTAL)
            return false
        }
        queuedEvents.release()

        val remainingNs = timeoutNs - (System.nanoTime() - startedAtNs).coerceAtLeast(0L)
        if (remainingNs <= 0L) {
            if (request.isComplete()) {
                onComplete?.invoke(request)
                return request.succeeded
            }
            quality.add(QualityCounterId.CONTROL_TIMEOUT_TOTAL)
            return false
        }
        val completed = try {
            request.await(remainingNs)
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
            quality.add(QualityCounterId.CONTROL_INTERRUPTED_TOTAL)
            return false
        }
        if (!completed) {
            quality.add(QualityCounterId.CONTROL_TIMEOUT_TOTAL)
            return false
        }
        onComplete?.invoke(request)
        return request.succeeded
    }

    private fun submitExactBlockingControl(
        target: Long,
        writeLogGrowth: Boolean,
        sealSnapshot: Boolean,
        storageSwitch: StorageSwitchRequest?,
        onComplete: ((AsyncControlRequest) -> Unit)?,
    ): Boolean {
        val request = AsyncControlRequest(target, writeLogGrowth, sealSnapshot, storageSwitch, blocking = true)
        var interrupted = false
        try {
            var admitted = false
            while (!admitted) {
                try {
                    admitted = controlLane.offer(request, CONTROL_WAIT_POLL_MS, TimeUnit.MILLISECONDS)
                } catch (_: InterruptedException) {
                    interrupted = true
                }
            }
        } finally {
            controlLane.finishSubmission()
        }
        queuedEvents.release()
        while (!request.isComplete()) {
            try {
                request.await(TimeUnit.MILLISECONDS.toNanos(CONTROL_WAIT_POLL_MS))
            } catch (_: InterruptedException) {
                interrupted = true
            }
        }
        if (interrupted) Thread.currentThread().interrupt()
        onComplete?.invoke(request)
        return request.succeeded
    }

    fun close(timeoutMs: Long = closeTimeoutMs()): Boolean {
        admissionLock.withLock {
            lifecycle.stopAccepting()
        }
        val activeWorker = worker
        if (activeWorker == null) {
            finishSession(null)
            return true
        }
        // Wake an idle poll without interrupting an in-flight file lock, custom storage call or
        // chunk commit. Interrupting those operations can turn an orderly shutdown into data loss.
        queuedEvents.release()
        val finished = waitForWorker(
            activeWorker,
            timeoutMs.coerceAtLeast(1L),
            waitUntilFinished = config.exactEventCollectionEnabled(),
        )
        if (!finished) {
            quality.add(QualityCounterId.CLOSE_TIMEOUT_TOTAL)
        }
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
            val waitMs = if (Thread.currentThread().name == MAIN_THREAD_NAME) {
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
            val acquired = admissionLock.tryLock()
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
                    if (eventLanes.hasCapacity(lane)) {
                        val event = createEvent()
                        val sequence = acceptedSequence + 1L
                        event.sequence = sequence
                        when (eventLanes.tryOffer(lane, event)) {
                            OfferResult.OFFERED -> {
                                acceptedSequence = sequence
                                quality.addAccepted(event.logicalEventCount)
                                recordWriterBackpressure(blockedAtNs)
                                if (!startWorker()) return false
                                queuedEvents.release()
                                return true
                            }
                            OfferResult.FULL -> retryReason = QualityCounterId.REASON_QUEUE_FULL
                            OfferResult.CONTENDED -> Unit
                        }
                    } else {
                        retryReason = QualityCounterId.REASON_QUEUE_FULL
                    }
                } finally {
                    admissionLock.unlock()
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

    private fun beginControlSubmission(startIfNeeded: Boolean = false): Long {
        return admissionLock.withLock {
            if (!lifecycle.isAccepting()) return@withLock CONTROL_NOT_ACCEPTING
            if (worker == null && (!startIfNeeded || !startWorker())) return@withLock CONTROL_NO_WORK
            controlLane.beginSubmission()
            acceptedSequence
        }
    }

    private fun startWorker(): Boolean {
        if (worker != null) return true
        return try {
            val startedWorker = Thread(::runWorkerFailOpen, "JankHunterWriter").apply {
                isDaemon = true
            }
            worker = startedWorker
            startedWorker.start()
            true
        } catch (error: Throwable) {
            worker = null
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

    private fun waitForWorker(activeWorker: Thread, timeoutMs: Long, waitUntilFinished: Boolean): Boolean {
        val deadlineNs = if (waitUntilFinished) Long.MAX_VALUE else {
            System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(timeoutMs)
        }
        var interrupted = false
        while (activeWorker.isAlive) {
            val remainingMs = if (waitUntilFinished) {
                CLOSE_JOIN_POLL_MS
            } else {
                val remainingNs = deadlineNs - System.nanoTime()
                if (remainingNs <= 0L) break
                TimeUnit.NANOSECONDS.toMillis(remainingNs).coerceAtLeast(1L)
            }
            try {
                activeWorker.join(minOf(CLOSE_JOIN_POLL_MS, remainingMs))
            } catch (_: InterruptedException) {
                interrupted = true
                if (!waitUntilFinished) break
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
                controlLane.hasPending() ||
                controlLane.hasSubmitters()
            ) {
                if (writer == null) {
                    // A size or I/O failure rejects queued events, so controls targeting their
                    // sequence can never become ready. Complete them as failed instead of keeping
                    // the daemon alive until every caller times out.
                    failPendingControls()
                    if (controlLane.hasSubmitters()) LockSupport.parkNanos(CONTROL_DRAIN_PARK_NS)
                    continue
                }
                val event = pollNextEvent()
                if (event != null) {
                    if (writeEvent(event)) {
                        completedSequence = event.sequence
                    }
                }
                processReadyControls()
                flushIfNeeded(force = false)
                if (event != null && initialCleanupPending) {
                    initialCleanupPending = false
                    retention.enforce(writer, binaryStorage, runId)
                }
            }
            if (writer != null) {
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
            writer?.let(::sealIoFailure)
        } finally {
            failPendingControlsAfterAdmissionClosed()
            closeSessionWriter()
        }
    }

    private fun openSessionWriter(
        storage: JankHunterBinaryStorage? = binaryStorage,
        terminalOnFailure: Boolean = true,
    ): Boolean {
        return try {
            if (runCohortLease == null) {
                val authoritativeStoragePaths = storage?.let { activeStorage ->
                    runCatching { activeStorage.listFiles() }.getOrNull()
                }
                runCohortLease = ProcessRunCohort.join(
                    directory,
                    sessionLocalDate,
                    authoritativeStoragePaths,
                ).also { lease ->
                    runId = lease.runId()
                    runLocalDate = lease.localDate()
                    dailySessionIndex = lease.dailySessionIndex()
                }
            }
            val opened = sessionFactory.open(
                localDate = runLocalDate,
                dailySessionIndex = dailySessionIndex,
                runId = runId,
                sessionId = sessionId,
                segmentIndex = segmentIndex,
                previousSegmentDigest = previousSegmentDigest,
                baseStats = completedSegmentStats,
                segmentStartElapsedUs = if (segmentIndex == 0L) collectorStartElapsedUs else nowElapsedUs(),
                segmentStartUnixMs = if (segmentIndex == 0L) {
                    sessionStartMs
                } else {
                    currentTimeMs.getAsLong().coerceAtLeast(0L)
                },
                storage = storage,
            )
            segmentLedger.register(opened, storage)
            binaryStorage = storage
            writer = opened.writer
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

    private fun hasPendingEvents(): Boolean = eventLanes.hasEvents()

    private fun pollNextEvent(): PendingLogEvent? {
        val available = try {
            queuedEvents.tryAcquire(WORKER_POLL_MS, TimeUnit.MILLISECONDS)
        } catch (_: InterruptedException) {
            false
        }
        if (!available) return null
        return eventLanes.pollNext()
    }

    private fun writeEvent(event: PendingLogEvent): Boolean {
        var rotatedWithoutProgress = false
        while (true) {
            val activeWriter = writer ?: return false
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
            if (writer === activeWriter) activeWriter.abort()
            stopAndDrain(currentEvent = null, reason = QualityCounterId.REASON_IO_LOST, failure = error)
            false
        }
    }

    private fun switchStorage(target: JankHunterBinaryStorage?): JankHunterStorageSwitchResult {
        if (binaryStorage === target) return JankHunterStorageSwitchResult.ALREADY_ACTIVE
        val activeWriter = writer ?: return JankHunterStorageSwitchResult.NOT_STARTED
        val previousStorage = binaryStorage
        return try {
            sealSegment(activeWriter)
            val handoff = segmentLedger.prepareHandoff(target)
            if (!openSessionWriter(target, terminalOnFailure = false)) {
                segmentLedger.rollbackHandoff(target, handoff)
                if (!openSessionWriter(previousStorage, terminalOnFailure = false)) {
                    stopAndDrain(
                        currentEvent = null,
                        reason = QualityCounterId.REASON_IO_LOST,
                        failure = IOException("Cannot reopen Jank Hunter storage after failed handoff"),
                    )
                }
                JankHunterStorageSwitchResult.FAILED
            } else {
                segmentLedger.commitHandoff(handoff)
                JankHunterStorageSwitchResult.SWITCHED
            }
        } catch (error: Throwable) {
            if (error.isFatal()) throw error
            quality.add(QualityCounterId.WRITER_IO_ERROR_TOTAL)
            if (writer == null && !openSessionWriter(previousStorage, terminalOnFailure = false)) {
                stopAndDrain(currentEvent = null, reason = QualityCounterId.REASON_IO_LOST, failure = error)
            }
            JankHunterStorageSwitchResult.FAILED
        }
    }

    private fun sealSegment(activeWriter: BinaryLogWriter) {
        activeWriter.sealRotation()
        previousSegmentDigest = activeWriter.sealedDigest()
            ?: throw IOException("Sealed Jank Hunter segment has no SHA-256 digest")
        val sealedStats = activeWriter.logGrowthStats()
        completedSegmentStats = sealedStats.copy(
            segmentRotationCount = saturatedAdd(sealedStats.segmentRotationCount, 1L),
        )
        segmentLedger.seal(activeWriter.path, binaryStorage)
        retention.enforce(activeWriter, binaryStorage, runId)
        if (writer === activeWriter) writer = null
        if (segmentIndex == Long.MAX_VALUE) throw IOException("Jank Hunter segment index exhausted")
        segmentIndex++
    }

    private fun processReadyControls() {
        while (true) {
            val request = controlLane.peek() ?: return
            if (completedSequence < request.targetSequence) return
            if (controlLane.poll() !== request) continue
            val flushed = flushIfNeeded(force = true)
            val succeeded = if (flushed && request.writeLogGrowth) {
                writer?.writeLogGrowthSummary() == true
            } else {
                flushed
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
                val activeWriter = writer
                if (activeWriter != null && rotateSegment(activeWriter)) {
                    request.snapshot = LogSnapshotResult(
                        capturedAtMs = capturedAtMs,
                        logPaths = segmentLedger.completedPaths(),
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

    private fun failPendingControls() {
        while (true) {
            val request = controlLane.poll() ?: return
            request.complete(success = false)
        }
    }

    /**
     * A submitter increments under [admissionLock] before publishing its control and decrements only
     * after the offer. Once admission is closed no new submitter can appear, so observing zero and
     * draining once more establishes a terminal frontier for every accepted control.
     */
    private fun failPendingControlsAfterAdmissionClosed() {
        while (controlLane.hasSubmitters()) {
            failPendingControls()
            LockSupport.parkNanos(CONTROL_DRAIN_PARK_NS)
        }
        failPendingControls()
    }

    private fun flushIfNeeded(force: Boolean): Boolean {
        runtimeHookFailures.sync()
        val now = SystemClock.elapsedRealtime()
        val interval = config.flushIntervalMs()
        if (!force && (interval <= 0L || now - lastFlushAtMs < interval)) return true
        val activeWriter = writer ?: return false
        return try {
            if (logGrowthManager == null) {
                activeWriter.flush()
            } else {
                if (!activeWriter.writeLogGrowthSummary()) {
                    activeWriter.flush()
                }
            }
            lastFlushAtMs = now
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
        admissionLock.withLock {
            lifecycle.stopAccepting()
            rejectAllQueuedLocked(reason)
        }
        currentEvent?.let { event ->
            quality.addRejected(event.recordType, reason, event.remainingEventCount)
        }
    }

    /** Called while [admissionLock] is held when the worker could not be started. */
    private fun terminateWithoutWorker(error: Throwable) {
        lifecycle.recordTermination(QualityCounterId.REASON_IO_LOST, error)
        lifecycle.stopAccepting()
        try {
            rejectAllQueuedLocked(QualityCounterId.REASON_IO_LOST)
        } catch (_: Throwable) {
            // Admission is already closed; quality accounting is best effort under VM pressure.
        }
        failPendingControlsAfterAdmissionClosed()
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
            lifecycle.stopAccepting()
        }
        try {
            failPendingControlsAfterAdmissionClosed()
        } catch (_: Throwable) {
            // Keep the fail-open boundary intact even when the VM cannot finish diagnostics.
        }
    }

    private fun deliverTerminalCallback() {
        lifecycle.deliverTerminalCallback(this, onTerminalStop)
    }

    private fun rejectAllQueuedLocked(reason: Int) {
        while (true) {
            val event = eventLanes.pollNext() ?: break
            quality.addRejected(event.recordType, reason, event.remainingEventCount)
        }
        queuedEvents.drainPermits()
    }

    private fun closeSessionWriter() {
        val activeWriter = writer
        if (activeWriter == null) {
            finishSession(null)
            return
        }
        try {
            retention.enforce(activeWriter, binaryStorage, runId)
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
        if (writer === activeWriter) writer = null
        try {
            if (activeWriter != null) retention.enforce(activeWriter, binaryStorage, runId)
        } finally {
            segmentLedger.finish()
            runCohortLease?.close()
            runCohortLease = null
        }
    }

    companion object {
        private const val CONTROL_NOT_ACCEPTING = -2L
        private const val CONTROL_NO_WORK = -1L
        private const val CLOSE_JOIN_POLL_MS = 250L
        private const val DEFAULT_BLOCKING_TIMEOUT_MS = 1_000L
        private const val WORKER_POLL_MS = 50L
        private const val CONTROL_QUEUE_CAPACITY = 16
        private const val CONTROL_WAIT_POLL_MS = 50L
        private const val CONTROL_DRAIN_PARK_NS = 100_000L
        private const val EXACT_BACKPRESSURE_PARK_NS = 100_000L
        private const val MAIN_THREAD_NAME = "main"
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
