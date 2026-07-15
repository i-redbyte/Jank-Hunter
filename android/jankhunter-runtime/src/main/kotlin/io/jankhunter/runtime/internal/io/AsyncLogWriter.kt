package io.jankhunter.runtime.internal.io

import android.os.Process
import android.os.SystemClock
import io.jankhunter.runtime.JankHunterBinaryStorage
import io.jankhunter.runtime.JankHunterBinaryWriter
import io.jankhunter.runtime.JankHunterConfig
import io.jankhunter.runtime.internal.system.RetentionEvidence
import java.io.File
import java.io.IOException
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

/**
 * Non-blocking producer facade for the binary session writer.
 *
 * The file, worker and event-lane arrays stay unallocated until the first event wins admission.
 * High-value evidence has an independent bounded reserve, while a global sequence merge preserves
 * producer admission order across both lanes. Quality and flush controls never consume bulk slots.
 */
internal class AsyncLogWriter private constructor(
    private val directory: File,
    private val config: JankHunterConfig,
    private val processName: String,
    private val sessionStartMs: Long,
    private val sessionLocalDate: String,
    private val collectorStartElapsedUs: Long,
    private val quality: LogQualityCounters,
    private val onTerminalStop: (AsyncLogWriter, Int, Throwable?) -> Unit,
) {
    private val controlQueue = ArrayBlockingQueue<FlushControl>(CONTROL_QUEUE_CAPACITY)
    private val queuedEvents = Semaphore(0)
    private val admissionLock = Any()
    private val controlSubmitters = AtomicInteger()
    private val terminalReason = AtomicInteger(TERMINAL_REASON_NONE)
    private val terminalCallbackDelivered = AtomicBoolean(false)

    @Volatile
    private var terminalFailure: Throwable? = null
    private val running = AtomicBoolean(true)
    private val sessionFinished = AtomicBoolean(false)
    private val producerContext = ThreadLocal<LogEventContext>()
    private val binaryStorage: JankHunterBinaryStorage? = config.binaryStorage()

    @Volatile
    private var eventLanes: EventLanes? = null

    @Volatile
    private var accepting = true

    private var acceptedSequence = 0L
    private var completedSequence = 0L
    private var allocation: SessionLogAllocator.Allocation? = null
    private var writer: BinaryLogWriter? = null
    private var lastFlushAtMs = SystemClock.elapsedRealtime()

    @Volatile
    private var worker: Thread? = null

    fun session(
        appVersion: String?,
        build: String?,
        device: String?,
        sdkInt: Int,
        processName: String?,
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
    ): Boolean {
        return enqueue(JhlogV9.TYPE_SESSION, EventLane.CRITICAL) {
            PendingLogEvent.Session(
                captureProducer(),
                appVersion,
                build,
                device,
                sdkInt,
                processName,
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
        enqueue(JhlogV9.TYPE_DEVICE_CONTEXT, EventLane.BULK) {
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
        enqueue(JhlogV9.TYPE_HTTP, EventLane.BULK) {
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
        enqueue(JhlogV9.TYPE_STALL, EventLane.CRITICAL) {
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
        enqueue(JhlogV9.TYPE_MEMORY, EventLane.BULK) {
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
        enqueue(JhlogV9.TYPE_RETAINED, EventLane.CRITICAL) {
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
        p50Ms: Long,
        p95Ms: Long,
        p99Ms: Long,
        foreground: Boolean,
        flags: Long = 0L,
    ) {
        enqueue(JhlogV9.TYPE_UI_WINDOW, EventLane.BULK) {
            PendingLogEvent.UiWindow(
                captureProducer(),
                screen,
                windowMs,
                frameCount,
                jankCount,
                p50Ms,
                p95Ms,
                p99Ms,
                foreground,
                flags,
            )
        }
    }

    fun counter(name: String?, value: Long) {
        if (value < 0L) {
            recordQuality(QualityCounterId.INVALID_METRIC)
            return
        }
        enqueue(JhlogV9.TYPE_COUNTER, metricLane(name)) {
            PendingLogEvent.Counter(captureProducer(), name, value)
        }
    }

    fun stableCounters(batch: StableCounterBatch) {
        if (batch.size <= 0) return
        enqueue(JhlogV9.TYPE_COUNTER, EventLane.BULK, batch.size.toLong()) {
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
        enqueue(JhlogV9.TYPE_GAUGE, metricLane(name)) {
            PendingLogEvent.Gauge(captureProducer(), name, value, count, sum, max, mode)
        }
    }

    fun flowContext(screen: String?, owner: String?, flow: String?, step: String?) {
        updateProducerContext(screen, owner, flow, step)
        enqueue(JhlogV9.TYPE_FLOW_TRANSITION, EventLane.CRITICAL) {
            PendingLogEvent.Flow(captureProducer(), screen, owner, flow, step)
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
    ) {
        enqueue(JhlogV9.TYPE_LOG_SPAM, EventLane.BULK) {
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
        enqueue(JhlogV9.TYPE_PROBLEM, EventLane.CRITICAL) {
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

    fun runtimeCalls(batch: RuntimeCallBatch) {
        if (batch.size <= 0) return
        enqueue(JhlogV9.TYPE_RUNTIME_CALL, EventLane.BULK, batch.size.toLong()) {
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
            if (!controlQueue.offer(FlushControl(target))) {
                quality.add(QualityCounterId.CONTROL_LANE_FULL_TOTAL)
            }
        } finally {
            controlSubmitters.decrementAndGet()
        }
    }

    fun flushBlocking(timeoutMs: Long = DEFAULT_BLOCKING_TIMEOUT_MS): Boolean {
        val target = beginControlSubmission()
        if (target == CONTROL_NO_WORK) return true
        if (target == CONTROL_NOT_ACCEPTING) return false
        val timeoutNs = TimeUnit.MILLISECONDS.toNanos(timeoutMs.coerceAtLeast(1L))
        val startedAtNs = System.nanoTime()
        val request = FlushControl(target, CountDownLatch(1))
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

        val remainingNs = timeoutNs - (System.nanoTime() - startedAtNs).coerceAtLeast(0L)
        if (remainingNs <= 0L) {
            if (request.isComplete()) return request.succeeded
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
        return request.succeeded
    }

    fun close(timeoutMs: Long = closeTimeoutMs()): Boolean {
        val activeWorker = synchronized(admissionLock) {
            accepting = false
            running.set(false)
            worker
        }
        if (activeWorker == null) return true
        // Wake an idle poll without interrupting an in-flight file lock, custom storage call or
        // chunk commit. Interrupting those operations can turn an orderly shutdown into data loss.
        queuedEvents.release()
        val finished = waitForWorker(activeWorker, timeoutMs.coerceAtLeast(1L))
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
        return synchronized(admissionLock) {
            if (!accepting) {
                quality.addRejected(
                    recordType,
                    QualityCounterId.REASON_NOT_ACCEPTING,
                    logicalEventCount,
                )
                return@synchronized false
            }

            val lanes = eventLanes ?: config.maxQueueSize().let { bulkCapacity ->
                EventLanes(
                    bulkCapacity = bulkCapacity,
                    criticalCapacity = criticalQueueCapacity(bulkCapacity),
                )
            }.also { eventLanes = it }
            val queue = lanes.queue(lane)
            if (queue.remainingCapacity() == 0) {
                quality.addRejected(recordType, QualityCounterId.REASON_QUEUE_FULL, logicalEventCount)
                return@synchronized false
            }

            val event = createEvent()
            val sequence = acceptedSequence + 1L
            event.sequence = sequence
            if (!queue.offer(event)) {
                quality.addRejected(
                    recordType,
                    QualityCounterId.REASON_QUEUE_FULL,
                    logicalEventCount,
                )
                return@synchronized false
            }
            acceptedSequence = sequence
            quality.addAccepted(event.logicalEventCount)
            if (!startWorkerLocked()) return@synchronized false
            queuedEvents.release()
            true
        }
    }

    private fun beginControlSubmission(): Long {
        return synchronized(admissionLock) {
            if (!accepting) return@synchronized CONTROL_NOT_ACCEPTING
            if (worker == null) return@synchronized CONTROL_NO_WORK
            controlSubmitters.incrementAndGet()
            acceptedSequence
        }
    }

    private fun startWorkerLocked(): Boolean {
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

    private fun waitForWorker(activeWorker: Thread, timeoutMs: Long): Boolean {
        val deadlineNs = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(timeoutMs)
        var interrupted = false
        while (activeWorker.isAlive) {
            val remainingNs = deadlineNs - System.nanoTime()
            if (remainingNs <= 0L) break
            try {
                val remainingMs = TimeUnit.NANOSECONDS.toMillis(remainingNs).coerceAtLeast(1L)
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
                    try {
                        writeEvent(event)
                    } finally {
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
            val opened = openSession(
                directory = directory,
                config = config,
                processName = processName,
                sessionStartMs = sessionStartMs,
                localDate = sessionLocalDate,
                collectorStartElapsedUs = collectorStartElapsedUs,
                quality = quality,
            )
            allocation = opened.allocation
            writer = opened.writer
            true
        } catch (error: Throwable) {
            if (error.isFatal()) throw error
            quality.add(QualityCounterId.WRITER_IO_ERROR_TOTAL)
            stopAndDrain(currentEvent = null, reason = QualityCounterId.REASON_IO_LOST, failure = error)
            false
        }
    }

    private fun hasPendingEvents(): Boolean = eventLanes?.hasEvents() == true

    private fun pollNextEvent(): PendingLogEvent? {
        val available = try {
            queuedEvents.tryAcquire(WORKER_POLL_MS, TimeUnit.MILLISECONDS)
        } catch (_: InterruptedException) {
            false
        }
        if (!available) return null
        return eventLanes?.pollNext()
    }

    private fun writeEvent(event: PendingLogEvent) {
        val activeWriter = writer ?: return
        try {
            event.writeTo(activeWriter)
        } catch (error: LogSizeLimitReachedException) {
            stopAndDrain(event, QualityCounterId.REASON_SIZE_LIMIT, error)
            try {
                activeWriter.sealSizeLimit()
            } catch (error: Throwable) {
                if (error.isFatal()) throw error
                activeWriter.abort()
            } finally {
                finishSession(activeWriter)
            }
        } catch (error: Throwable) {
            if (error.isFatal()) throw error
            quality.add(QualityCounterId.WRITER_IO_ERROR_TOTAL)
            stopAndDrain(event, QualityCounterId.REASON_IO_LOST, error)
            sealIoFailure(activeWriter)
        }
    }

    private fun processReadyControls() {
        while (true) {
            val request = controlQueue.peek() ?: return
            if (completedSequence < request.targetSequence) return
            if (controlQueue.poll() !== request) continue
            request.complete(flushIfNeeded(force = true))
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
        val now = SystemClock.elapsedRealtime()
        val interval = config.flushIntervalMs()
        if (!force && (interval <= 0L || now - lastFlushAtMs < interval)) return true
        val activeWriter = writer ?: return false
        return try {
            activeWriter.flush()
            lastFlushAtMs = now
            true
        } catch (error: LogSizeLimitReachedException) {
            stopAndDrain(currentEvent = null, reason = QualityCounterId.REASON_SIZE_LIMIT, failure = error)
            try {
                activeWriter.sealSizeLimit()
            } catch (error: Throwable) {
                if (error.isFatal()) throw error
                activeWriter.abort()
            } finally {
                finishSession(activeWriter)
            }
            false
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
        terminalReason.compareAndSet(TERMINAL_REASON_NONE, reason)
        if (terminalFailure == null && failure != null) terminalFailure = failure
        synchronized(admissionLock) {
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
        val lanes = eventLanes ?: return
        while (true) {
            val event = lanes.pollNext() ?: break
            quality.addRejected(event.recordType, reason, event.remainingEventCount)
        }
        queuedEvents.drainPermits()
    }

    private fun closeSessionWriter() {
        val activeWriter = writer ?: return
        try {
            activeWriter.close(JhlogV9.SEGMENT_END_SHUTDOWN)
        } catch (error: Throwable) {
            if (error.isFatal()) throw error
            quality.add(QualityCounterId.WRITER_IO_ERROR_TOTAL)
            activeWriter.abort()
        } finally {
            finishSession(activeWriter)
        }
    }

    private fun finishSession(activeWriter: BinaryLogWriter) {
        if (!sessionFinished.compareAndSet(false, true)) return
        if (writer === activeWriter) writer = null
        val activeAllocation = allocation
        allocation = null
        try {
            cleanupOldFiles(activeWriter)
        } finally {
            activeAllocation?.close()
        }
    }

    private fun cleanupOldFiles(activeWriter: BinaryLogWriter? = writer) {
        val currentWriter = activeWriter ?: return
        try {
            val storage = binaryStorage
            if (storage != null) {
                val protectedPaths = SessionLogAllocator.activeLeases(directory).protectedPaths + currentWriter.path
                storage.cleanup(protectedPaths)
            } else {
                currentWriter.file?.let { file ->
                    SessionLogRetention.enforce(directory, file, LOCAL_SESSION_HISTORY_LIMIT_BYTES)
                }
            }
        } catch (error: Throwable) {
            if (error.isFatal()) throw error
        }
    }

    private enum class EventLane {
        CRITICAL,
        BULK,
    }

    private class EventLanes(
        bulkCapacity: Int,
        criticalCapacity: Int,
    ) {
        private val critical = ArrayBlockingQueue<PendingLogEvent>(criticalCapacity)
        private val bulk = ArrayBlockingQueue<PendingLogEvent>(bulkCapacity)

        fun queue(lane: EventLane): ArrayBlockingQueue<PendingLogEvent> {
            return if (lane == EventLane.CRITICAL) critical else bulk
        }

        fun hasEvents(): Boolean = critical.isNotEmpty() || bulk.isNotEmpty()

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
    }

    private class FlushControl(
        val targetSequence: Long,
        private val completion: CountDownLatch? = null,
    ) {
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

    private class OpenedSession(
        val allocation: SessionLogAllocator.Allocation,
        val writer: BinaryLogWriter,
    )

    companion object {
        private const val TERMINAL_REASON_NONE = 0
        private const val CONTROL_NOT_ACCEPTING = -2L
        private const val CONTROL_NO_WORK = -1L
        private const val CLOSE_JOIN_POLL_MS = 250L
        private const val DEFAULT_BLOCKING_TIMEOUT_MS = 1_000L
        private const val WORKER_POLL_MS = 50L
        private const val CONTROL_QUEUE_CAPACITY = 16
        private const val CONTROL_DRAIN_PARK_NS = 100_000L
        private const val CRITICAL_QUEUE_CAPACITY_DIVISOR = 8
        private const val MIN_CRITICAL_QUEUE_CAPACITY = 16
        private const val MAX_CRITICAL_QUEUE_CAPACITY = 256
        private const val MAX_OPEN_ATTEMPTS = 1_024
        private const val BYTES_PER_MIB = 1_048_576L
        private const val LOCAL_SESSION_HISTORY_LIMIT_BYTES = 64L * BYTES_PER_MIB
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
            onTerminalStop: (AsyncLogWriter, Int, Throwable?) -> Unit,
        ): AsyncLogWriter {
            return open(
                directory = directory,
                config = config,
                processName = processName,
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
            currentTimeMs: () -> Long,
            onTerminalStop: (AsyncLogWriter, Int, Throwable?) -> Unit,
        ): AsyncLogWriter {
            val sessionStartMs = currentTimeMs().coerceAtLeast(0L)
            val localDate = SimpleDateFormat("yyyy-MM-dd", Locale.US).format(Date(sessionStartMs))
            return AsyncLogWriter(
                directory = directory,
                config = config,
                processName = processName,
                sessionStartMs = sessionStartMs,
                sessionLocalDate = localDate,
                collectorStartElapsedUs = nowElapsedUs(),
                quality = LogQualityCounters(),
                onTerminalStop = onTerminalStop,
            )
        }

        private fun openSession(
            directory: File,
            config: JankHunterConfig,
            processName: String,
            sessionStartMs: Long,
            localDate: String,
            collectorStartElapsedUs: Long,
            quality: LogQualityCounters,
        ): OpenedSession {
            val storage = config.binaryStorage()
            val physicalLimit = minPositiveLimit(
                config.sessionLogSizeLimitBytes(),
                storage?.fileSizeLimitBytes ?: 0L,
            )
            val header = BinaryLogFileHeader(
                runId = BinaryLogFileHeader.randomId(),
                processInstanceId = PROCESS_INSTANCE_ID,
                sessionId = BinaryLogFileHeader.randomId(),
                segmentIndex = 0L,
                osPid = Process.myPid().toLong().coerceAtLeast(0L),
                collectorStartElapsedUs = collectorStartElapsedUs,
                segmentStartElapsedUs = collectorStartElapsedUs,
                segmentStartUnixMs = sessionStartMs,
                identitySource = 0L,
                processName = processName,
                symbolNamespace = config.symbolNamespace(),
            )
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
                    authoritativeStoragePaths,
                    minimumIndex,
                )
                var localFile: File? = null
                var externalWriter: JankHunterBinaryWriter? = null
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
                        binaryWriter = BinaryLogWriter(
                            candidate,
                            config.maxDictionaryEntries(),
                            config.maxDictionaryValueBytes(),
                            header,
                            quality,
                            physicalLimit,
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
                        binaryWriter = BinaryLogWriter(
                            candidate,
                            config.maxDictionaryEntries(),
                            config.maxDictionaryValueBytes(),
                            header,
                            quality,
                            physicalLimit,
                        )
                    }
                    val openedWriter = checkNotNull(binaryWriter)
                    allocation.updateProtectedPath(openedWriter.path)
                    return OpenedSession(allocation, openedWriter)
                } catch (error: Throwable) {
                    binaryWriter?.abort() ?: runCatching { externalWriter?.close() }
                    allocation.close()
                    localFile?.delete()
                    throw error
                }
            }
            throw IOException("Cannot allocate an unused Jank Hunter session log name")
        }

        private fun criticalQueueCapacity(bulkCapacity: Int): Int {
            val remainder = if (bulkCapacity % CRITICAL_QUEUE_CAPACITY_DIVISOR == 0) 0 else 1
            val proportional = bulkCapacity / CRITICAL_QUEUE_CAPACITY_DIVISOR + remainder
            return proportional.coerceIn(MIN_CRITICAL_QUEUE_CAPACITY, MAX_CRITICAL_QUEUE_CAPACITY)
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

        private fun nowElapsedUs(): Long = SystemClock.elapsedRealtimeNanos().coerceAtLeast(0L) / 1_000L

        private fun Throwable.isFatal(): Boolean = this is VirtualMachineError || this is ThreadDeath
    }
}
