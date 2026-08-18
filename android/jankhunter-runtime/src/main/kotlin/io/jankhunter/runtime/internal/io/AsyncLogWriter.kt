package io.jankhunter.runtime.internal.io

import android.os.Process
import android.os.SystemClock
import io.jankhunter.runtime.JankHunterBinaryStorage
import io.jankhunter.runtime.JankHunterBinaryArtifact
import io.jankhunter.runtime.JankHunterBinaryWriter
import io.jankhunter.runtime.JankHunterConfig
import io.jankhunter.runtime.JankHunterLogGrowthSummary
import io.jankhunter.runtime.RuntimeHookFailureTracker
import io.jankhunter.runtime.RuntimeHookFailureReason
import io.jankhunter.runtime.internal.concurrent.BoundedMpscQueue
import io.jankhunter.runtime.internal.concurrent.BoundedMpscQueue.OfferResult
import io.jankhunter.runtime.internal.system.RetentionEvidence
import java.io.File
import java.io.IOException
import java.nio.charset.StandardCharsets
import java.security.MessageDigest
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Semaphore
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
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
internal class AsyncLogWriter private constructor(
    private val directory: File,
    private val config: JankHunterConfig,
    private val processName: String,
    private val expectedProcesses: Set<String>,
    private val rosterDeclarationComplete: Boolean,
    private val sessionStartMs: Long,
    private val sessionLocalDate: String,
    private val collectorStartElapsedUs: Long,
    private val currentTimeMs: () -> Long,
    private val quality: LogQualityCounters,
    private val logGrowthManager: LogGrowthManager?,
    private val onTerminalStop: (AsyncLogWriter, Int, Throwable?) -> Unit,
) {
    private val controlQueue = ArrayBlockingQueue<FlushControl>(CONTROL_QUEUE_CAPACITY)
    private val queuedEvents = Semaphore(0)
    private val admissionLock = ReentrantLock()
    private val controlSubmitters = AtomicInteger()
    private val terminalReason = AtomicInteger(TERMINAL_REASON_NONE)
    private val terminalCallbackDelivered = AtomicBoolean(false)

    @Volatile
    private var terminalFailure: Throwable? = null
    private val running = AtomicBoolean(true)
    private val sessionFinished = AtomicBoolean(false)
    private val producerContext = ThreadLocal<LogEventContext>()
    private val binaryStorage: JankHunterBinaryStorage? = config.binaryStorage()

    private val eventLanes = EventLanes(
        bulkCapacity = config.maxQueueSize(),
        criticalCapacity = criticalQueueCapacity(config.maxQueueSize()),
    )

    @Volatile
    private var accepting = true

    private var acceptedSequence = 0L
    private var completedSequence = 0L
    private val allocations = ArrayList<SessionLogAllocator.Allocation>()
    private val completedSegmentPaths = ArrayList<String>()
    private val segmentProtections = ArrayList<JankHunterBinaryArtifact>()
    private var writer: BinaryLogWriter? = null
    private var runCohortLease: ProcessRunCohort.Lease? = null
    private var runId = ByteArray(0)
    private val sessionId = BinaryLogFileHeader.randomId()
    private var segmentIndex = 0L
    private var previousSegmentDigest = ByteArray(0)
    private var completedSegmentStats = LogContainerStats.EMPTY
    private var lastFlushAtMs = SystemClock.elapsedRealtime()
    private val reportedRuntimeHookFailures = RuntimeHookFailureTracker.snapshot()

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
        return enqueue(Jhlog.TYPE_SESSION, EventLane.CRITICAL) {
            PendingLogEvent.Session(
                captureProducer(),
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
    fun updateProducerContext(screen: String?, owner: String?, flow: String?, step: String?) {
        val current = producerContext.get() ?: LogEventContext.EMPTY
        if (current.matches(screen, owner, flow, step)) return
        val context = LogEventContext.of(screen, owner, flow, step)
        if (context == LogEventContext.EMPTY) {
            producerContext.remove()
        } else {
            producerContext.set(context)
        }
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
        enqueue(Jhlog.TYPE_DEVICE_CONTEXT, EventLane.BULK) {
            PendingLogEvent.DeviceContext(
                captureProducer(),
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
        flow: String?,
        step: String?,
        route: String?,
        durationMs: Long,
        dnsMs: Long,
        connectMs: Long,
        ttfbMs: Long,
        statusClass: Int,
        rxBytes: Long,
        txBytes: Long,
        flags: Long,
    ) {
        enqueue(Jhlog.TYPE_HTTP, EventLane.BULK) {
            PendingLogEvent.Http(
                captureProducer(screen, owner, flow, step),
                owner,
                route,
                durationMs,
                dnsMs,
                connectMs,
                ttfbMs,
                statusClass,
                rxBytes,
                txBytes,
                flags,
            )
        }
    }

    fun stall(
        screen: String?,
        owner: String?,
        flow: String?,
        step: String?,
        stackHint: String?,
        durationMs: Long,
        foreground: Boolean,
    ) {
        enqueue(Jhlog.TYPE_STALL, EventLane.CRITICAL) {
            PendingLogEvent.Stall(
                captureProducer(screen, owner, flow, step),
                screen,
                owner,
                flow,
                step,
                stackHint,
                durationMs,
                foreground,
            )
        }
    }

    fun memory(pssKb: Long, javaHeapKb: Long, nativeHeapKb: Long, foreground: Boolean) {
        enqueue(Jhlog.TYPE_MEMORY, EventLane.BULK) {
            PendingLogEvent.Memory(captureProducer(), pssKb, javaHeapKb, nativeHeapKb, foreground)
        }
    }

    fun retained(
        screen: String?,
        owner: String?,
        flow: String?,
        step: String?,
        className: String?,
        holder: String?,
        ageMs: Long,
        count: Long,
        foreground: Boolean,
        evidence: RetentionEvidence,
    ) {
        enqueue(Jhlog.TYPE_RETAINED, EventLane.CRITICAL) {
            PendingLogEvent.Retained(
                captureProducer(),
                screen,
                owner,
                flow,
                step,
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
        enqueue(Jhlog.TYPE_UI_WINDOW, EventLane.BULK) {
            PendingLogEvent.UiWindow(
                captureProducer(),
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
        enqueue(Jhlog.TYPE_PROCESS_EXIT, EventLane.CRITICAL) {
            PendingLogEvent.ProcessExit(
                captureProducer(),
                reason,
                timestampUnixMs,
                importance,
                pssKb,
                rssKb,
                processName,
            )
        }
    }

    fun io(operation: Long, durationUs: Long, bytes: Long, mainThread: Boolean) {
        enqueue(Jhlog.TYPE_IO, if (mainThread) EventLane.CRITICAL else EventLane.BULK) {
            PendingLogEvent.IO(captureProducer(), operation, durationUs, bytes, mainThread)
        }
    }

    fun counter(name: String?, value: Long) {
        if (value < 0L) {
            recordQuality(QualityCounterId.INVALID_METRIC)
            return
        }
        enqueue(Jhlog.TYPE_COUNTER, metricLane(name)) {
            PendingLogEvent.Counter(captureProducer(), name, value)
        }
    }

    fun stableCounters(batch: StableCounterBatch): Boolean {
        if (batch.size <= 0) return true
        return enqueue(Jhlog.TYPE_COUNTER, EventLane.BULK, batch.logicalEventCount()) {
            PendingLogEvent.StableCounters(captureProducer(), batch)
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
            PendingLogEvent.Gauge(captureProducer(), name, value, count, sum, max, mode)
        }
    }

    fun logSpam(
        screen: String?,
        owner: String?,
        flow: String?,
        step: String?,
        source: String?,
        level: Int,
        count: Long,
    ): Boolean {
        return enqueue(Jhlog.TYPE_LOG_SPAM, EventLane.BULK, count) {
            PendingLogEvent.LogSpam(captureProducer(), screen, owner, flow, step, source, level, count)
        }
    }

    fun problemWindow(
        screen: String?,
        owner: String?,
        flow: String?,
        step: String?,
        kind: String?,
        windowMs: Long,
        count: Long,
        maxMs: Long,
        foreground: Boolean = false,
    ) {
        enqueue(Jhlog.TYPE_PROBLEM, EventLane.CRITICAL) {
            PendingLogEvent.Problem(
                captureProducer(),
                screen,
                owner,
                flow,
                step,
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
        return enqueue(Jhlog.TYPE_RUNTIME_CALL, EventLane.BULK, batch.logicalEventCount()) {
            PendingLogEvent.RuntimeCalls(captureProducer(), batch)
        }
    }

    /** Quality state is cumulative, lock-free, and intentionally bypasses the bounded data queue. */
    fun recordQuality(counterId: Int, delta: Long = 1L) {
        quality.add(counterId, delta)
    }

    internal fun isAcceptingEvents(): Boolean = accepting

    internal fun terminalFailureCause(): Throwable? = terminalFailure

    fun flush() {
        val target = beginControlSubmission()
        if (target < 0L) return
        try {
            if (!controlQueue.offer(FlushControl(target, writeLogGrowth = false))) {
                quality.add(QualityCounterId.CONTROL_LANE_FULL_TOTAL)
            }
        } finally {
            controlSubmitters.decrementAndGet()
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

    internal fun logGrowthSummary(): JankHunterLogGrowthSummary? =
        logGrowthManager?.summary(writer?.logGrowthStats())

    internal fun logGrowthManager(): LogGrowthManager? = logGrowthManager

    private fun submitBlockingControl(
        timeoutMs: Long,
        writeLogGrowth: Boolean,
        waitForExactFrontier: Boolean,
        sealSnapshot: Boolean = false,
        onComplete: ((FlushControl) -> Unit)? = null,
    ): Boolean {
        val target = beginControlSubmission(startIfNeeded = writeLogGrowth || sealSnapshot)
        if (target == CONTROL_NO_WORK) return !writeLogGrowth && !sealSnapshot
        if (target == CONTROL_NOT_ACCEPTING) return false
        if (config.exactEventCollectionEnabled() && waitForExactFrontier) {
            return submitExactBlockingControl(target, writeLogGrowth, sealSnapshot, onComplete)
        }
        val timeoutNs = TimeUnit.MILLISECONDS.toNanos(timeoutMs.coerceAtLeast(1L))
        val startedAtNs = System.nanoTime()
        val request = FlushControl(target, writeLogGrowth, sealSnapshot, CountDownLatch(1))
        val admitted = try {
            controlQueue.offer(request, timeoutNs, TimeUnit.NANOSECONDS)
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
            quality.add(QualityCounterId.CONTROL_INTERRUPTED_TOTAL)
            return false
        } finally {
            controlSubmitters.decrementAndGet()
        }
        if (!admitted) {
            quality.add(QualityCounterId.CONTROL_TIMEOUT_TOTAL)
            return false
        }
        queuedEvents.release()

        val remainingNs = timeoutNs - (System.nanoTime() - startedAtNs).coerceAtLeast(0L)
        if (remainingNs <= 0L) {
            if (request.isComplete()) {
                if (request.succeeded) onComplete?.invoke(request)
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
        if (request.succeeded) onComplete?.invoke(request)
        return request.succeeded
    }

    private fun submitExactBlockingControl(
        target: Long,
        writeLogGrowth: Boolean,
        sealSnapshot: Boolean,
        onComplete: ((FlushControl) -> Unit)?,
    ): Boolean {
        val request = FlushControl(target, writeLogGrowth, sealSnapshot, CountDownLatch(1))
        var interrupted = false
        try {
            var admitted = false
            while (!admitted) {
                try {
                    admitted = controlQueue.offer(request, CONTROL_WAIT_POLL_MS, TimeUnit.MILLISECONDS)
                } catch (_: InterruptedException) {
                    interrupted = true
                }
            }
        } finally {
            controlSubmitters.decrementAndGet()
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
        if (request.succeeded) onComplete?.invoke(request)
        return request.succeeded
    }

    fun close(timeoutMs: Long = closeTimeoutMs()): Boolean {
        admissionLock.withLock {
            accepting = false
            running.set(false)
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

    private fun captureProducer(): LogEventContext? = producerContext.get()

    private fun captureProducer(screen: String?, owner: String?, flow: String?, step: String?): LogEventContext? {
        val current = producerContext.get()
        return when {
            current != null && current.matches(screen, owner, flow, step) -> current
            LogEventContext.EMPTY.matches(screen, owner, flow, step) -> null
            else -> LogEventContext.of(screen, owner, flow, step)
        }
    }

    private inline fun enqueue(
        recordType: Int,
        lane: EventLane,
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
                    if (!accepting) {
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
            if (!accepting) return@withLock CONTROL_NOT_ACCEPTING
            if (worker == null && (!startIfNeeded || !startWorker())) return@withLock CONTROL_NO_WORK
            controlSubmitters.incrementAndGet()
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

    private fun metricLane(name: String?): EventLane {
        return if (isCriticalMetricName(name)) {
            EventLane.CRITICAL
        } else {
            EventLane.BULK
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
                running.get() ||
                hasPendingEvents() ||
                controlQueue.isNotEmpty() ||
                controlSubmitters.get() > 0
            ) {
                if (writer == null) {
                    // A size or I/O failure rejects queued events, so controls targeting their
                    // sequence can never become ready. Complete them as failed instead of keeping
                    // the daemon alive until every caller times out.
                    failPendingControls()
                    if (controlSubmitters.get() > 0) LockSupport.parkNanos(CONTROL_DRAIN_PARK_NS)
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
                    cleanupOldFiles()
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

    private fun openSessionWriter(): Boolean {
        return try {
            if (runCohortLease == null) {
                runCohortLease = ProcessRunCohort.join(directory).also { lease ->
                    runId = lease.runId()
                }
            }
            val opened = openSession(
                directory = directory,
                config = config,
                processName = processName,
                expectedProcesses = expectedProcesses,
                rosterDeclarationComplete = rosterDeclarationComplete,
                localDate = sessionLocalDate,
                collectorStartElapsedUs = collectorStartElapsedUs,
                quality = quality,
                logGrowthManager = logGrowthManager,
                runId = runId,
                sessionId = sessionId,
                segmentIndex = segmentIndex,
                previousSegmentDigest = previousSegmentDigest,
                baseStats = completedSegmentStats,
                segmentStartElapsedUs = if (segmentIndex == 0L) collectorStartElapsedUs else nowElapsedUs(),
                segmentStartUnixMs = if (segmentIndex == 0L) sessionStartMs else currentTimeMs().coerceAtLeast(0L),
            )
            allocations += opened.allocation
            opened.protection?.let(segmentProtections::add)
            writer = opened.writer
            true
        } catch (error: StorageBudgetExhaustedException) {
            stopAndDrain(currentEvent = null, reason = QualityCounterId.REASON_STORAGE_BUDGET, failure = error)
            false
        } catch (error: Throwable) {
            if (error.isFatal()) throw error
            quality.add(QualityCounterId.WRITER_IO_ERROR_TOTAL)
            stopAndDrain(currentEvent = null, reason = QualityCounterId.REASON_IO_LOST, failure = error)
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
                if (terminalReason.get() == QualityCounterId.REASON_STORAGE_BUDGET) {
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
            activeWriter.sealRotation()
            previousSegmentDigest = activeWriter.sealedDigest()
                ?: throw IOException("Sealed Jank Hunter segment has no SHA-256 digest")
            val sealedStats = activeWriter.logGrowthStats()
            completedSegmentStats = sealedStats.copy(
                segmentRotationCount = saturatedAdd(sealedStats.segmentRotationCount, 1L),
            )
            completedSegmentPaths += activeWriter.path
            cleanupOldFiles(activeWriter)
            if (writer === activeWriter) writer = null
            if (segmentIndex == Long.MAX_VALUE) throw IOException("Jank Hunter segment index exhausted")
            segmentIndex++
            openSessionWriter()
        } catch (error: Throwable) {
            if (error.isFatal()) throw error
            quality.add(QualityCounterId.WRITER_IO_ERROR_TOTAL)
            activeWriter.abort()
            stopAndDrain(currentEvent = null, reason = QualityCounterId.REASON_IO_LOST, failure = error)
            false
        }
    }

    private fun processReadyControls() {
        while (true) {
            val request = controlQueue.peek() ?: return
            if (completedSequence < request.targetSequence) return
            if (controlQueue.poll() !== request) continue
            val flushed = flushIfNeeded(force = true)
            val succeeded = if (flushed && request.writeLogGrowth) {
                writer?.writeLogGrowthSummary() == true
            } else {
                flushed
            }
            val snapshotSucceeded = if (succeeded && request.sealSnapshot) {
                val capturedAtMs = currentTimeMs().coerceAtLeast(0L)
                val activeWriter = writer
                if (activeWriter != null && rotateSegment(activeWriter)) {
                    request.snapshot = LogSnapshotResult(
                        capturedAtMs = capturedAtMs,
                        logPaths = ArrayList(completedSegmentPaths),
                    )
                    true
                } else {
                    false
                }
            } else {
                succeeded
            }
            request.complete(snapshotSucceeded)
        }
    }

    private fun failPendingControls() {
        while (true) {
            val request = controlQueue.poll() ?: return
            request.complete(success = false)
        }
    }

    /**
     * A submitter increments under [admissionLock] before publishing its control and decrements only
     * after the offer. Once admission is closed no new submitter can appear, so observing zero and
     * draining once more establishes a terminal frontier for every accepted control.
     */
    private fun failPendingControlsAfterAdmissionClosed() {
        while (controlSubmitters.get() > 0) {
            failPendingControls()
            LockSupport.parkNanos(CONTROL_DRAIN_PARK_NS)
        }
        failPendingControls()
    }

    private fun flushIfNeeded(force: Boolean): Boolean {
        syncRuntimeHookFailures()
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

    private fun syncRuntimeHookFailures() {
        val current = RuntimeHookFailureTracker.snapshot()
        RuntimeHookFailureReason.entries.forEach { reason ->
            val index = reason.ordinal
            val previous = reportedRuntimeHookFailures[index]
            val value = current[index]
            if (value > previous) {
                val delta = value - previous
                quality.add(QualityCounterId.RUNTIME_HOOK_FAILURE_TOTAL, delta)
                quality.add(reason.qualityCounterId(), delta)
                reportedRuntimeHookFailures[index] = value
            }
        }
    }

    private fun RuntimeHookFailureReason.qualityCounterId(): Int = when (this) {
        RuntimeHookFailureReason.INSTRUMENTATION_HOOK -> QualityCounterId.RUNTIME_HOOK_INSTRUMENTATION_FAILURE
        RuntimeHookFailureReason.ASYNC_WRAPPER -> QualityCounterId.RUNTIME_HOOK_ASYNC_WRAPPER_FAILURE
        RuntimeHookFailureReason.RUNTIME_LIFECYCLE -> QualityCounterId.RUNTIME_HOOK_LIFECYCLE_FAILURE
        RuntimeHookFailureReason.COLLECTOR -> QualityCounterId.RUNTIME_HOOK_COLLECTOR_FAILURE
        RuntimeHookFailureReason.CONTEXT -> QualityCounterId.RUNTIME_HOOK_CONTEXT_FAILURE
        RuntimeHookFailureReason.SCHEDULER -> QualityCounterId.RUNTIME_HOOK_SCHEDULER_FAILURE
        RuntimeHookFailureReason.JANKSTATS_DEPENDENCY_MISSING -> QualityCounterId.JANKSTATS_DEPENDENCY_MISSING
        RuntimeHookFailureReason.JANKSTATS_INSTALL -> QualityCounterId.JANKSTATS_INSTALL_FAILURE
        RuntimeHookFailureReason.JANKSTATS_FRAME -> QualityCounterId.JANKSTATS_FRAME_FAILURE
        RuntimeHookFailureReason.JANKSTATS_CONTROL -> QualityCounterId.JANKSTATS_CONTROL_FAILURE
        RuntimeHookFailureReason.UNCLASSIFIED -> QualityCounterId.RUNTIME_HOOK_UNCLASSIFIED_FAILURE
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
        terminalReason.compareAndSet(TERMINAL_REASON_NONE, reason)
        if (terminalFailure == null && failure != null) terminalFailure = failure
        admissionLock.withLock {
            accepting = false
            running.set(false)
            rejectAllQueuedLocked(reason)
        }
        currentEvent?.let { event ->
            quality.addRejected(event.recordType, reason, event.remainingEventCount)
        }
    }

    /** Called while [admissionLock] is held when the worker could not be started. */
    private fun terminateWithoutWorker(error: Throwable) {
        terminalReason.compareAndSet(TERMINAL_REASON_NONE, QualityCounterId.REASON_IO_LOST)
        if (terminalFailure == null) terminalFailure = error
        accepting = false
        running.set(false)
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
            terminalReason.compareAndSet(TERMINAL_REASON_NONE, QualityCounterId.REASON_IO_LOST)
            if (terminalFailure == null) terminalFailure = error
            accepting = false
            running.set(false)
        }
        try {
            failPendingControlsAfterAdmissionClosed()
        } catch (_: Throwable) {
            // Keep the fail-open boundary intact even when the VM cannot finish diagnostics.
        }
    }

    private fun deliverTerminalCallback() {
        val reason = terminalReason.get()
        if (reason == TERMINAL_REASON_NONE || !terminalCallbackDelivered.compareAndSet(false, true)) return
        try {
            onTerminalStop(this, reason, terminalFailure)
        } catch (_: Throwable) {
            // A lifecycle callback must not escape the fail-open writer boundary.
        }
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
            cleanupOldFiles(activeWriter)
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
        if (!sessionFinished.compareAndSet(false, true)) return
        if (writer === activeWriter) writer = null
        try {
            if (activeWriter != null) cleanupOldFiles(activeWriter)
        } finally {
            segmentProtections.forEach { protection -> runCatching { protection.commit() } }
            segmentProtections.clear()
            allocations.forEach { allocation -> allocation.close() }
            allocations.clear()
            runCohortLease?.close()
            runCohortLease = null
        }
    }

    private fun cleanupOldFiles(activeWriter: BinaryLogWriter? = writer) {
        val currentWriter = activeWriter ?: return
        try {
            val storage = binaryStorage
            if (storage != null) {
                val protectedPaths = SessionLogAllocator.activeLeases(directory).protectedPaths + currentWriter.path
                val result = SessionLogRetention.enforce(
                    storage = storage,
                    currentRunId = SessionLogName.runIdHex(runId),
                    protectedPaths = protectedPaths,
                    historyLimitBytes = archiveLimitBytes(config, storage),
                )
                recordRetention(result)
            } else {
                val result = SessionLogRetention.enforce(
                    directory = directory,
                    currentRunId = SessionLogName.runIdHex(runId),
                    protectedPaths = SessionLogAllocator.activeLeases(directory).protectedPaths,
                    historyLimitBytes = config.sessionLogSizeLimitBytes(),
                )
                recordRetention(result)
            }
        } catch (error: Throwable) {
            if (error.isFatal()) throw error
        }
    }

    private fun recordRetention(result: SessionLogRetention.Result) {
        recordRetention(quality, result)
    }

    private enum class EventLane {
        CRITICAL,
        BULK,
    }

    private class EventLanes(
        bulkCapacity: Int,
        criticalCapacity: Int,
    ) {
        private val critical = BoundedMpscQueue<PendingLogEvent>(criticalCapacity)
        private val bulk = BoundedMpscQueue<PendingLogEvent>(bulkCapacity)

        fun tryOffer(lane: EventLane, event: PendingLogEvent): OfferResult {
            return queue(lane).tryOffer(event)
        }

        fun hasEvents(): Boolean = !critical.isEmpty() || !bulk.isEmpty()

        fun hasCapacity(lane: EventLane): Boolean = queue(lane).hasCapacity()

        fun pollNext(): PendingLogEvent? {
            val criticalHead = critical.peek()
            val bulkHead = bulk.peek()
            return when {
                criticalHead == null -> bulk.poll()
                bulkHead == null -> critical.poll()
                criticalHead.sequence < bulkHead.sequence -> critical.poll()
                else -> bulk.poll()
            }
        }

        private fun queue(lane: EventLane): BoundedMpscQueue<PendingLogEvent> {
            return if (lane == EventLane.CRITICAL) critical else bulk
        }
    }

    private class FlushControl(
        val targetSequence: Long,
        val writeLogGrowth: Boolean,
        val sealSnapshot: Boolean = false,
        private val completion: CountDownLatch? = null,
    ) {
        var snapshot: LogSnapshotResult? = null

        @Volatile
        var succeeded = false
            private set

        fun complete(success: Boolean) {
            succeeded = success
            completion?.countDown()
        }

        fun await(timeoutNs: Long): Boolean = completion?.await(timeoutNs, TimeUnit.NANOSECONDS) ?: true

        fun isComplete(): Boolean = completion?.count == 0L
    }

    internal data class LogSnapshotResult(
        val capturedAtMs: Long,
        val logPaths: List<String>,
    )

    private class OpenedSession(
        val allocation: SessionLogAllocator.Allocation,
        val writer: BinaryLogWriter,
        val protection: JankHunterBinaryArtifact?,
    )

    companion object {
        private const val TERMINAL_REASON_NONE = 0
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
        private const val CRITICAL_QUEUE_CAPACITY_DIVISOR = 8
        private const val MIN_CRITICAL_QUEUE_CAPACITY = 16
        private const val MAX_CRITICAL_QUEUE_CAPACITY = 256
        private const val MAX_OPEN_ATTEMPTS = 1_024
        private const val PROCESS_SCOPE_FINGERPRINT_BYTES = 32
        private const val APP_LIFECYCLE_METRIC_PREFIX = "app.lifecycle."
        private const val SCREEN_LIFECYCLE_METRIC_MARKER = ".lifecycle."
        private const val RUNTIME_SESSION_METRIC_PREFIX = "jankhunter.runtime.session."
        private const val RUNTIME_CRASH_METRIC_PREFIX = "jankhunter.runtime.crash."
        private const val HEAP_DUMP_METRIC_PREFIX = "jankhunter.heap_dump."
        private val PROCESS_INSTANCE_ID = BinaryLogFileHeader.randomId()

        fun open(directory: File, config: JankHunterConfig, processName: String): AsyncLogWriter {
            return open(
                directory = directory,
                config = config,
                processName = processName,
                currentTimeMs = System::currentTimeMillis,
                onTerminalStop = { _, _, _ -> },
            )
        }

        internal fun open(
            directory: File,
            config: JankHunterConfig,
            processName: String,
            expectedProcesses: Set<String> = setOf(processName),
            rosterDeclarationComplete: Boolean = true,
            onTerminalStop: (AsyncLogWriter, Int, Throwable?) -> Unit,
        ): AsyncLogWriter {
            return open(
                directory = directory,
                config = config,
                processName = processName,
                expectedProcesses = expectedProcesses,
                rosterDeclarationComplete = rosterDeclarationComplete,
                currentTimeMs = System::currentTimeMillis,
                onTerminalStop = onTerminalStop,
            )
        }

        internal fun open(
            directory: File,
            config: JankHunterConfig,
            processName: String,
            currentTimeMs: () -> Long,
        ): AsyncLogWriter {
            return open(
                directory = directory,
                config = config,
                processName = processName,
                currentTimeMs = currentTimeMs,
                onTerminalStop = { _, _, _ -> },
            )
        }

        private fun open(
            directory: File,
            config: JankHunterConfig,
            processName: String,
            expectedProcesses: Set<String> = setOf(processName),
            rosterDeclarationComplete: Boolean = true,
            currentTimeMs: () -> Long,
            onTerminalStop: (AsyncLogWriter, Int, Throwable?) -> Unit,
        ): AsyncLogWriter {
            val sessionStartMs = currentTimeMs().coerceAtLeast(0L)
            val localDate = SimpleDateFormat("yyyy-MM-dd", Locale.US).format(Date(sessionStartMs))
            return AsyncLogWriter(
                directory = directory,
                config = config,
                processName = processName,
                expectedProcesses = expectedProcesses,
                rosterDeclarationComplete = rosterDeclarationComplete,
                sessionStartMs = sessionStartMs,
                sessionLocalDate = localDate,
                collectorStartElapsedUs = nowElapsedUs(),
                currentTimeMs = currentTimeMs,
                quality = LogQualityCounters(),
                logGrowthManager = if (config.logGrowthAnalyticsEnabled()) {
                    createLogGrowthManager(
                        directory,
                        processName,
                        expectedProcesses,
                        rosterDeclarationComplete,
                        currentTimeMs,
                    )
                } else {
                    null
                },
                onTerminalStop = onTerminalStop,
            )
        }

        private fun openSession(
            directory: File,
            config: JankHunterConfig,
            processName: String,
            expectedProcesses: Set<String>,
            rosterDeclarationComplete: Boolean,
            localDate: String,
            collectorStartElapsedUs: Long,
            quality: LogQualityCounters,
            logGrowthManager: LogGrowthManager?,
            runId: ByteArray,
            sessionId: ByteArray,
            segmentIndex: Long,
            previousSegmentDigest: ByteArray,
            baseStats: LogContainerStats,
            segmentStartElapsedUs: Long,
            segmentStartUnixMs: Long,
        ): OpenedSession {
            val storage = config.binaryStorage()
            val archiveLimit = archiveLimitBytes(config, storage)
            val physicalLimit = storage?.fileSizeLimitBytes
                ?.takeIf { limit -> limit < Long.MAX_VALUE }
                ?: 0L
            val exactAdmission = config.exactEventCollectionEnabled()
            // Exact admission may wait for bounded queues to drain, but it must never turn memory
            // limits into sizing hints. Dictionary overflow remains explicit in quality counters.
            val dictionaryEntries = config.maxDictionaryEntries()
            val dictionaryValueBytes = config.maxDictionaryValueBytes()
            val allowedProcesses = config.allowedProcesses()
            val allowedProcessCount = allowedProcesses.size.toLong()
            val processScope = when {
                allowedProcessCount > 0L -> Jhlog.PROCESS_SCOPE_ALLOWLIST
                config.mainProcessOnly() -> Jhlog.PROCESS_SCOPE_MAIN_ONLY
                else -> Jhlog.PROCESS_SCOPE_ALL
            }
            val header = BinaryLogFileHeader(
                runId = runId,
                processInstanceId = PROCESS_INSTANCE_ID,
                sessionId = sessionId,
                segmentIndex = segmentIndex,
                osPid = Process.myPid().toLong().coerceAtLeast(0L),
                collectorStartElapsedUs = collectorStartElapsedUs,
                segmentStartElapsedUs = segmentStartElapsedUs,
                segmentStartUnixMs = segmentStartUnixMs,
                identitySource = 0L,
                processName = processName,
                symbolNamespace = config.symbolNamespace(),
                processScope = processScope,
                allowedProcessCount = allowedProcessCount,
                processScopeFingerprint = processScopeFingerprint(allowedProcesses),
                previousSegmentDigest = previousSegmentDigest,
                expectedProcessCount = expectedProcesses.size.toLong(),
                expectedProcessFingerprint = processScopeFingerprint(expectedProcesses),
                processRosterDeclarationComplete = rosterDeclarationComplete,
                requiredFeatures = if (exactAdmission) {
                    Jhlog.REQUIRED_FEATURES
                } else {
                    Jhlog.BEST_EFFORT_FEATURES
                },
            )
            val logGrowth = logGrowthManager?.let { manager ->
                LogGrowthSessionBinding(manager, localDate, archiveLimit, baseStats)
            }
            var attempts = 0
            var minimumIndex = 0L
            while (attempts < MAX_OPEN_ATTEMPTS) {
                attempts++
                val authoritativeStoragePaths = storage?.let { binaryStorage ->
                    runCatching { binaryStorage.listFiles() }.getOrNull()
                }
                val allocation = SessionLogAllocator.reserve(
                    directory,
                    localDate,
                    runId,
                    authoritativeStoragePaths,
                    minimumIndex,
                )
                var localFile: File? = null
                var externalWriter: JankHunterBinaryWriter? = null
                var protection: JankHunterBinaryArtifact? = null
                var archiveBudget: RunArchiveBudget? = null
                var binaryWriter: BinaryLogWriter? = null
                try {
                    if (storage == null) {
                        val candidate = File(directory, allocation.fileName)
                        if (!candidate.createNewFile()) {
                            minimumIndex = allocation.index + 1L
                            allocation.close()
                            continue
                        }
                        localFile = candidate
                        archiveBudget = openArchiveBudget(directory, storage, runId, archiveLimit, quality)
                        binaryWriter = BinaryLogWriter(
                            candidate,
                            dictionaryEntries,
                            dictionaryValueBytes,
                            header,
                            quality,
                            physicalLimit,
                            logGrowth = logGrowth,
                            archiveBudget = archiveBudget,
                        )
                    } else {
                        val candidate = storage.openWriter(allocation.fileName)
                        externalWriter = candidate
                        if (candidate.bytesWritten() != 0L) {
                            minimumIndex = allocation.index + 1L
                            runCatching { candidate.close() }
                            allocation.close()
                            continue
                        }
                        archiveBudget = openArchiveBudget(directory, storage, runId, archiveLimit, quality)
                        binaryWriter = BinaryLogWriter(
                            candidate,
                            dictionaryEntries,
                            dictionaryValueBytes,
                            header,
                            quality,
                            physicalLimit,
                            logGrowth,
                            archiveBudget,
                        )
                        protection = storage.protect(allocation.fileName)
                    }
                    val openedWriter = checkNotNull(binaryWriter)
                    allocation.updateProtectedPath(openedWriter.path)
                    return OpenedSession(allocation, openedWriter, protection)
                } catch (error: Throwable) {
                    runCatching { protection?.abort() }
                    binaryWriter?.abort() ?: runCatching {
                        externalWriter?.close()
                    }
                    runCatching { archiveBudget?.close() }
                    if (binaryWriter == null && externalWriter != null) {
                        runCatching { storage?.delete(allocation.fileName) }
                    }
                    allocation.close()
                    localFile?.delete()
                    throw error
                }
            }
            throw IOException("Cannot allocate an unused Jank Hunter session log name")
        }

        private fun processScopeFingerprint(allowedProcesses: Set<String>): ByteArray {
            if (allowedProcesses.isEmpty()) return ByteArray(0)
            val digest = MessageDigest.getInstance("SHA-256")
            val length = ByteArray(Int.SIZE_BYTES)
            allowedProcesses.sorted().forEach { processName ->
                val bytes = processName.toByteArray(StandardCharsets.UTF_8)
                length[0] = (bytes.size ushr 24).toByte()
                length[1] = (bytes.size ushr 16).toByte()
                length[2] = (bytes.size ushr 8).toByte()
                length[3] = bytes.size.toByte()
                digest.update(length)
                digest.update(bytes)
            }
            return digest.digest().copyOf(PROCESS_SCOPE_FINGERPRINT_BYTES)
        }

        private fun criticalQueueCapacity(bulkCapacity: Int): Int {
            val remainder = if (bulkCapacity % CRITICAL_QUEUE_CAPACITY_DIVISOR == 0) 0 else 1
            val proportional = bulkCapacity / CRITICAL_QUEUE_CAPACITY_DIVISOR + remainder
            return proportional.coerceIn(MIN_CRITICAL_QUEUE_CAPACITY, MAX_CRITICAL_QUEUE_CAPACITY)
        }

        private fun createLogGrowthManager(
            directory: File,
            processName: String,
            expectedProcesses: Set<String>,
            rosterDeclarationComplete: Boolean,
            currentTimeMs: () -> Long,
        ): LogGrowthManager? {
            return try {
                if (rosterDeclarationComplete && processName in expectedProcesses) {
                    LogGrowthHistoryStore.deleteObsoleteProcessScopes(directory, expectedProcesses)
                }
                LogGrowthManager(directory, processName, currentTimeMs)
            } catch (error: Throwable) {
                if (error.isFatal()) throw error
                null
            }
        }

        internal fun isCriticalMetricName(name: String?): Boolean {
            if (name == null) return false
            return name.startsWith(APP_LIFECYCLE_METRIC_PREFIX) ||
                name.contains(SCREEN_LIFECYCLE_METRIC_MARKER) ||
                name.startsWith(RUNTIME_SESSION_METRIC_PREFIX) ||
                name.startsWith(RUNTIME_CRASH_METRIC_PREFIX) ||
                name.startsWith(HEAP_DUMP_METRIC_PREFIX)
        }

        private fun minPositiveLimit(first: Long, second: Long): Long {
            if (first <= 0L) return second
            if (second <= 0L) return first
            return minOf(first, second)
        }

        private fun archiveLimitBytes(config: JankHunterConfig, storage: JankHunterBinaryStorage?): Long {
            return minPositiveLimit(
                config.sessionLogSizeLimitBytes(),
                storage?.archivesSizeLimitBytes?.takeIf { limit -> limit < Long.MAX_VALUE } ?: 0L,
            )
        }

        private fun openArchiveBudget(
            directory: File,
            storage: JankHunterBinaryStorage?,
            runId: ByteArray,
            archiveLimit: Long,
            quality: LogQualityCounters,
        ): RunArchiveBudget? {
            if (archiveLimit <= 0L || archiveLimit == Long.MAX_VALUE) return null
            val runIdHex = SessionLogName.runIdHex(runId)
            fun paths(): List<String> = storage?.listFiles() ?: directory.listFiles { file -> file.isFile }
                .orEmpty()
                .map(File::getAbsolutePath)
            return RunArchiveBudget.open(
                directory = directory,
                runId = runIdHex,
                limitBytes = archiveLimit,
                actualArchiveBytes = { RunArchiveBudget.retainedJhlogBytes(paths()) },
                reclaimBytesTo = { targetBytes ->
                    val protectedPaths = SessionLogAllocator.activeLeases(directory).protectedPaths
                    val result = if (storage == null) {
                        SessionLogRetention.enforce(directory, runIdHex, protectedPaths, targetBytes)
                    } else {
                        SessionLogRetention.enforce(storage, runIdHex, protectedPaths, targetBytes)
                    }
                    recordRetention(quality, result)
                    result.deletedBytes
                },
            )
        }

        private fun recordRetention(quality: LogQualityCounters, result: SessionLogRetention.Result) {
            quality.add(QualityCounterId.ARCHIVE_EVICTED_RUN_TOTAL, result.deletedRuns)
            quality.add(QualityCounterId.ARCHIVE_EVICTED_SEGMENT_TOTAL, result.deletedSegments)
            quality.add(QualityCounterId.ARCHIVE_EVICTED_BYTES_TOTAL, result.deletedBytes)
        }

        private fun nowElapsedUs(): Long = SystemClock.elapsedRealtimeNanos().coerceAtLeast(0L) / 1_000L

        private fun Throwable.isFatal(): Boolean = this is VirtualMachineError || this is ThreadDeath
    }
}
