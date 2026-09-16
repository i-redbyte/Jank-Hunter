package io.jankhunter.runtime

import android.os.Process
import io.jankhunter.runtime.internal.saturatingAdd
import io.jankhunter.runtime.internal.monotonicDeadlineAfterMillis
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.QualityCounterId
import io.jankhunter.runtime.internal.io.RuntimeCallBatch
import java.lang.ref.WeakReference
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.locks.LockSupport

/**
 * Runtime call graph with an allocation-free application-thread publication path.
 *
 * Each producer owns its stack and exact aggregate pages. Only [JankHunterGraph] merges published
 * pages or touches the asynchronous writer. Context is captured at callee entry and is part of the
 * edge identity.
 */
internal class RuntimeCallGraphSession(
    private val nowMs: RuntimeLongSource,
    private val captureScreen: () -> String?,
    private val captureOperationId: RuntimeLongSource,
    private val maxKeys: RuntimeIntSource,
    private val exactAdmission: RuntimeBooleanSource = RuntimeBooleanSource { true },
    private val admissionWaitNanos: RuntimeLongSource = RuntimeLongSource { DEFAULT_ADMISSION_WAIT_NS },
    private val periodicFlushIntervalMs: Long = DEFAULT_PERIODIC_FLUSH_INTERVAL_MS,
    private val consumerDelayNanos: Long = 0L,
    private val batchObserver: ((RuntimeCallBatch) -> Unit)? = null,
    private val publisherAdmissionObserver: (() -> Unit)? = null,
    private val consumerLoopObserver: (() -> Unit)? = null,
    private val consumerThreadFactory: (Runnable, String) -> Thread = ::Thread,
    private val producerRegistrationObserver: (() -> Unit)? = null,
    private val uptimeNanos: RuntimeLongSource = RuntimeLongSource(System::nanoTime),
    private val generation: Long,
    private val onStopped: () -> Unit,
    storageLimitBytes: Long = RuntimeGraphStorageBudget.DEFAULT_LIMIT_BYTES,
    private val nextSkippedToken: RuntimeLongSource = RuntimeLongSource { 0L },
) {
    private val storageBudget = RuntimeGraphStorageBudget(storageLimitBytes)
    private val completion = RuntimeConsumerCompletion()
    private val startupCleanupPending = AtomicBoolean()
    private val stateCleared = AtomicBoolean()
    @Volatile var hasConsumerOwner = true
        private set

    val isAccepting: Boolean get() = lifecycle.acceptingPublishers

    fun whenDrained(action: () -> Unit) = completion.whenComplete(action)

    private val producer = RuntimeGraphProducer()
    private val consumer = RuntimeGraphConsumer(RUNTIME_BATCH_POOL_CAPACITY, MAX_FLUSH_RECORDS)
    private val lifecycle = RuntimeGraphLifecycle()

    fun resetFlushState(writer: AsyncLogWriter, publish: () -> Unit) {
        producer.epoch.set(generation)
        clearRegistry()
        clearQuality()
        lifecycle.clearRequested.set(false)
        lifecycle.producerWake.clear()
        lifecycle.activeWriter = writer
        lifecycle.consumerFailed.set(false)
        lifecycle.running.set(true)
        lifecycle.acceptingPublishers = true
        try {
            val graphThread = consumerThreadFactory(Runnable(::runConsumerFailOpen), CONSUMER_NAME).apply { isDaemon = true }
            lifecycle.consumerThread = graphThread
            publish()
            graphThread.start()
        } catch (error: Throwable) {
            lifecycle.acceptingPublishers = false
            lifecycle.consumerFailed.set(true)
            lifecycle.running.set(false)
            producer.registry.forEach { entry -> entry.buffer.owner.get()?.let(LockSupport::unpark) }
            if (lifecycle.consumerThread?.isAlive != true && hasConsumerOwner) {
                startupCleanupPending.set(true)
                completeFailedStartIfIdle()
            }
            RuntimeHookGuard.rethrowFatal(error)
            throw error
        }
    }

    private fun completeFailedStartIfIdle() {
        if (!startupCleanupPending.get() || hasAdmittedProducers() || !startupCleanupPending.compareAndSet(true, false)) return
        val lost = unaccountedAcceptedEventCount()
        addSaturating(lifecycle.shutdownLoss, lost)
        addSaturating(lifecycle.acceptedEventLoss, lost)
        flushQuality(lifecycle.activeWriter)
        retireAllProducerBuffers()
        lifecycle.activeWriter = null
        lifecycle.consumerThread = null
        if (lifecycle.clearRequested.get()) clearStoppedState()
        try {
            completion.complete()
        } finally {
            hasConsumerOwner = false
            onStopped()
        }
    }

    fun ownsWriter(writer: AsyncLogWriter): Boolean = lifecycle.activeWriter === writer

    fun clear() {
        lifecycle.acceptingPublishers = false
        lifecycle.running.set(false)
        lifecycle.clearRequested.set(true)
        val graphThread = lifecycle.consumerThread
        graphThread?.let(LockSupport::unpark)
        producer.threadState.remove()
        if (hasConsumerOwner && lifecycle.consumerThread != null) return
        clearStoppedState()
    }

    fun enter(methodId: Long, methodName: String, enabled: Boolean): Long {
        if (!enabled || !lifecycle.acceptingPublishers) return DISABLED_TOKEN
        val currentEpoch = producer.epoch.get()
        val state = producerState(currentEpoch, methodEntry = true) ?: return DISABLED_TOKEN
        state.buffer.producerAdmitted = true
        try {
            if (!lifecycle.acceptingPublishers || state.epoch != producer.epoch.get()) return DISABLED_TOKEN
            val captured = state.stack.push(methodId, methodName, nowMs.getAsLong(), captureScreen(),
                captureOperationId.getAsLong())
            if (captured) return currentEpoch
            producer.storageSkippedEntries.incrementAndGet()
            wakeConsumer()
            return state.stack.skippedToken
        } finally {
            releaseProducer(state)
        }
    }

    fun hasCurrentMethod(): Boolean = withCurrentStack(false) { it.hasCurrentMethod() }

    fun currentMethodId(): Long = withCurrentStack(0L) { it.currentMethodId() }

    fun currentMethodName(): String? = withCurrentStack(null) { it.currentMethodName() }

    fun exit(token: Long, methodId: Long) {
        if (token == DISABLED_TOKEN) return
        val state = producer.threadState.get()?.get()
        if (state == null) {
            if (token == producer.epoch.get() && lifecycle.acceptingPublishers) producer.stackMismatch.incrementAndGet()
            return
        }
        state.buffer.producerAdmitted = true
        try {
            if (!lifecycle.acceptingPublishers) return
            publisherAdmissionObserver?.invoke()
            exitAccepted(token, methodId, state)
        } finally {
            state.buffer.producerActive = false
            state.buffer.producerAdmitted = false
            if (!lifecycle.acceptingPublishers) {
                lifecycle.consumerThread?.let(LockSupport::unpark)
                completeFailedStartIfIdle()
            }
        }
    }

    /**
     * Records a typed root boundary without requiring the full application runtime graph.
     *
     * Compose, database and worker hooks use this path so their high-frequency events retain the
     * same bounded per-thread aggregation and loss accounting as ordinary runtime edges.
     */
    fun recordSemantic(
        callerId: Long,
        callerName: String,
        calleeId: Long,
        calleeName: String,
        durationMs: Long,
        enabled: Boolean,
    ) {
        if (!enabled || !lifecycle.acceptingPublishers) return
        val currentEpoch = producer.epoch.get()
        val state = producerState(currentEpoch, methodEntry = false) ?: return
        state.buffer.producerAdmitted = true
        try {
            if (!lifecycle.acceptingPublishers || state.epoch != producer.epoch.get()) return
            publisherAdmissionObserver?.invoke()
            val buffer = state.buffer
            buffer.recordAttempted()
            val published = publishWithBackpressure(
                buffer = buffer,
                callerId = callerId,
                callerName = callerName,
                calleeId = calleeId,
                calleeName = calleeName,
                screen = captureScreen(),
                operationId = captureOperationId.getAsLong(),
                durationMs = durationMs.coerceAtLeast(0L),
            )
            if (published) buffer.recordAccepted()
        } finally {
            state.buffer.producerActive = false
            state.buffer.producerAdmitted = false
            if (!lifecycle.acceptingPublishers) {
                lifecycle.consumerThread?.let(LockSupport::unpark)
                completeFailedStartIfIdle()
            }
        }
    }

    private fun exitAccepted(token: Long, methodId: Long, state: RuntimeGraphProducerState) {
        val currentEpoch = producer.epoch.get()
        if (state.epoch != currentEpoch) {
            state.stack.reset()
            return
        }
        if (token < 0L) {
            state.stack.popSkipped(token)
            return
        }
        if (token != currentEpoch) return
        if (!state.stack.pop(methodId)) {
            producer.stackMismatch.incrementAndGet()
            return
        }
        if (!state.stack.hasPoppedParent) return
        val buffer = state.buffer
        buffer.recordAttempted()
        val durationMs = (nowMs.getAsLong() - state.stack.poppedStartedAtMs).coerceAtLeast(0L)
        val published = publishWithBackpressure(
            buffer = buffer,
            callerId = state.stack.poppedParentId,
            callerName = checkNotNull(state.stack.poppedParentName),
            calleeId = methodId,
            calleeName = checkNotNull(state.stack.poppedName),
            screen = state.stack.poppedScreen,
            operationId = state.stack.poppedOperationId,
            durationMs = durationMs,
        )
        if (!published) return
        buffer.recordAccepted()
    }

    private fun publishWithBackpressure(
        buffer: RuntimeGraphAggregateBuffer,
        callerId: Long,
        callerName: String,
        calleeId: Long,
        calleeName: String,
        screen: String?,
        operationId: Long,
        durationMs: Long,
    ): Boolean {
        buffer.admissionStartedAtNs = 0L
        try {
            if (!acquireProducer(buffer)) return rejectAdmission()
            while (true) {
                val result = buffer.tryAdd(
                    callerId, callerName, calleeId, calleeName, screen, operationId, durationMs,
                )
                if (result != RUNTIME_GRAPH_ADD_FULL) {
                    if (result == RUNTIME_GRAPH_ADD_PAGE_PUBLISHED) wakeConsumer()
                    return true
                }
                // Also wake in best-effort mode: budget pressure must reclaim unused pages even
                // when the current producer cannot wait for admission.
                wakeConsumer()
                if (buffer.rotationRequested) {
                    buffer.producerActive = false
                    if (!acquireProducer(buffer)) return rejectAdmission()
                } else if (!awaitAdmission(buffer, BACKPRESSURE_PARK_NS)) {
                    return rejectAdmission()
                }
            }
        } finally {
            buffer.producerWaiting = false
            recordGraphBackpressure(buffer.admissionStartedAtNs)
        }
    }

    private fun rejectAdmission(): Boolean {
        if (lifecycle.consumerFailed.get() || !lifecycle.running.get()) {
            recordShutdownLoss()
        } else {
            producer.capacityLoss.incrementAndGet()
        }
        return false
    }

    private fun awaitAdmission(buffer: RuntimeGraphAggregateBuffer, parkNs: Long, allowStopping: Boolean = false): Boolean {
        if (!canAwaitAdmission(buffer, allowStopping)) return false
        val remaining = buffer.admissionWaitNs - (System.nanoTime() - buffer.admissionStartedAtNs)
        if (remaining <= 0L) return false
        buffer.producerWaiting = true
        wakeConsumer()
        LockSupport.parkNanos(minOf(parkNs, remaining))
        return canAwaitAdmission(buffer, allowStopping)
    }

    private fun canAwaitAdmission(buffer: RuntimeGraphAggregateBuffer, allowStopping: Boolean = false): Boolean {
        if ((!allowStopping && !lifecycle.running.get()) || lifecycle.consumerFailed.get() ||
            Thread.currentThread().isInterrupted || !exactAdmission.getAsBoolean()
        ) return false
        if (buffer.admissionStartedAtNs == 0L) {
            val waitNs = admissionWaitNanos.getAsLong().coerceAtLeast(0L)
            if (waitNs == 0L) return false
            buffer.admissionWaitNs = waitNs
            buffer.admissionStartedAtNs = System.nanoTime()
            producer.backpressureCount.incrementAndGet()
        }
        return System.nanoTime() - buffer.admissionStartedAtNs < buffer.admissionWaitNs
    }

    private fun recordGraphBackpressure(blockedAtNs: Long) {
        if (blockedAtNs != 0L) {
            producer.backpressureNanos.addAndGet((System.nanoTime() - blockedAtNs).coerceAtLeast(1L))
        }
    }

    fun flushBlocking(timeoutMs: Long): Boolean {
        if (!lifecycle.running.get()) {
            return !lifecycle.consumerFailed.get() && lifecycle.consumerThread?.isAlive != true
        }
        val request = lifecycle.flushRequest.incrementAndGet()
        lifecycle.consumerThread?.let(LockSupport::unpark)
        val deadline = monotonicDeadlineAfterMillis(timeoutMs.coerceAtLeast(1L))
        while (lifecycle.flushCompleted.get() < request) {
            if (lifecycle.consumerFailed.get() || !lifecycle.running.get()) return false
            val remaining = deadline - System.nanoTime()
            if (remaining <= 0L) return false
            LockSupport.parkNanos(minOf(remaining, FLUSH_WAIT_POLL_NS))
            if (Thread.interrupted()) {
                Thread.currentThread().interrupt()
                return false
            }
        }
        return true
    }

    fun requestStop() {
        lifecycle.acceptingPublishers = false
        lifecycle.running.set(false)
        lifecycle.consumerThread?.let(LockSupport::unpark)
    }

    fun flushForShutdown(timeoutMs: Long = SHUTDOWN_TIMEOUT_MS): Boolean {
        val graphThread = lifecycle.consumerThread ?: return true
        lifecycle.acceptingPublishers = false
        lifecycle.running.set(false)
        LockSupport.unpark(graphThread)
        val terminated = awaitConsumerShutdown(graphThread, timeoutMs)
        if (terminated) flushQuality(lifecycle.activeWriter)
        return terminated
    }

    private fun awaitConsumerShutdown(graphThread: Thread, timeoutMs: Long): Boolean {
        val deadline = monotonicDeadlineAfterMillis(timeoutMs.coerceAtLeast(1L))
        var interrupted = false
        while (graphThread.isAlive) {
            val remaining = deadline - System.nanoTime()
            if (remaining <= 0L) break
            val waitMs = TimeUnit.NANOSECONDS.toMillis(remaining).coerceIn(1L, SHUTDOWN_JOIN_POLL_MS)
            try {
                graphThread.join(waitMs)
            } catch (_: InterruptedException) {
                interrupted = true
                break
            }
        }
        if (interrupted) Thread.currentThread().interrupt()
        return !graphThread.isAlive
    }

    internal fun acceptedForTest(): Long = producerAcceptedTotal()

    internal fun attemptedForTest(): Long = producerAttemptedTotal()

    internal fun emittedForTest(): Long = consumer.emitted.get()

    internal fun aggregatedEdgeKeysForTest(): Long = consumer.aggregatedEdgeKeys.get()

    internal fun acceptedEventLossForTest(): Long = lifecycle.acceptedEventLoss.get()

    internal fun currentThreadDepthForTest(): Int = withCurrentStack(0) { it.depth }

    internal fun consumerForTest(): Thread? = lifecycle.consumerThread

    internal fun backpressureCountForTest(): Long = producer.backpressureCount.get()

    internal fun producerCapacityLossForTest(): Long = producer.capacityLoss.get()
    internal fun storageUsedForTest(): Long = storageBudget.usedBytes()
    internal fun storagePeakForTest(): Long = storageBudget.peakBytes()

    internal fun acceptingPublishersForTest(): Boolean = lifecycle.acceptingPublishers

    internal fun registeredProducerCountForTest(): Int {
        var result = 0
        producer.registry.forEach { result++ }
        return result
    }

    private fun producerState(currentEpoch: Long, methodEntry: Boolean): RuntimeGraphProducerState? {
        producer.threadState.get()?.get()?.takeIf { it.epoch == currentEpoch }?.let { return it }
        producerRegistrationObserver?.invoke()
        // Register the allocation itself before touching the quota. Shutdown cannot retire this
        // session while a producer owns a reservation that has not reached the registry yet.
        producer.registering.incrementAndGet()
        try {
            if (!lifecycle.acceptingPublishers || producer.epoch.get() != currentEpoch) return null
            if (!storageBudget.tryReserve(RuntimeGraphStorageBudget.INITIAL_PRODUCER_BYTES)) {
                if (methodEntry) producer.storageSkippedEntries.incrementAndGet()
                else {
                    addSaturating(producer.retiredAttempted, 1L)
                    rejectAdmission()
                }
                wakeConsumer()
                return null
            }
            var state: RuntimeGraphProducerState? = null
            try {
                state = RuntimeGraphProducerState(currentEpoch, RuntimeCallStack(storageBudget, nextSkippedToken),
                    RuntimeGraphAggregateBuffer(Thread.currentThread(), storageBudget), storageBudget)
                producer.registry.add(state)
                if (!lifecycle.acceptingPublishers || producer.epoch.get() != currentEpoch) {
                    if (producer.registry.remove(state)) state.releaseStorage()
                    return null
                }
                producer.threadState.set(WeakReference(state))
                return state
            } catch (failure: Throwable) {
                // Registry nodes and the ThreadLocal weak reference can also fail to allocate.
                // Registration is still admitted, so cleanup cannot race terminal retirement.
                if (state == null) storageBudget.release(RuntimeGraphStorageBudget.INITIAL_PRODUCER_BYTES)
                else {
                    producer.threadState.remove()
                    producer.registry.remove(state)
                    state.releaseStorage()
                }
                throw failure
            }
        } finally {
            producer.registering.decrementAndGet()
            if (!lifecycle.acceptingPublishers) {
                lifecycle.consumerThread?.let(LockSupport::unpark)
                completeFailedStartIfIdle()
            }
        }
    }

    private fun releaseProducer(state: RuntimeGraphProducerState) {
        state.buffer.producerAdmitted = false
        if (!lifecycle.acceptingPublishers) {
            lifecycle.consumerThread?.let(LockSupport::unpark)
            completeFailedStartIfIdle()
        }
    }

    private fun runConsumerFailOpen() {
        try {
            val table = RuntimeGraphEdgeTable()
            try {
                Process.setThreadPriority(GRAPH_CONSUMER_PRIORITY)
            } catch (error: Throwable) {
                RuntimeHookGuard.rethrowFatal(error)
            }
            // Relative JVM waits use active time on Android, excluding deep sleep.
            val flushIntervalNs = TimeUnit.MILLISECONDS.toNanos(periodicFlushIntervalMs.coerceAtLeast(1L))
            var lastFlushAtNs = uptimeNanos.getAsLong()
            while (lifecycle.running.get() || hasAdmittedProducers() || hasBufferedEvents()) {
                consumerLoopObserver?.invoke()
                lifecycle.producerWake.clear()
                val drained = drainBuffers(table)
                if (storageBudget.consumePressure()) {
                    rotateProducerPages()
                    drainAllBuffers(table)
                    trimEmptyProducerPages()
                }
                if (drained > 0 && consumerDelayNanos > 0L) LockSupport.parkNanos(consumerDelayNanos)
                val request = lifecycle.flushRequest.get()
                val now = uptimeNanos.getAsLong()
                val due = now - lastFlushAtNs >= flushIntervalNs
                var completedRequest = NO_FLUSH_REQUEST
                if (request > lifecycle.flushCompleted.get() || due || !lifecycle.running.get()) {
                    rotateProducerPages()
                    drainAllBuffers(table)
                    emitAll(table)
                    flushQuality(lifecycle.activeWriter)
                    completedRequest = request
                    lastFlushAtNs = now
                }
                reclaimDeadBuffers()
                if (completedRequest != NO_FLUSH_REQUEST) lifecycle.flushCompleted.set(completedRequest)
                if (drained == 0 && lifecycle.running.get()) {
                    val remainingNs = flushIntervalNs - (uptimeNanos.getAsLong() - lastFlushAtNs)
                    if (!lifecycle.producerWake.isPending()) LockSupport.parkNanos(remainingNs)
                }
            }
            rotateProducerPages()
            drainAllBuffers(table)
            emitAll(table)
            flushQuality(lifecycle.activeWriter)
            reclaimDeadBuffers()
            retireAllProducerBuffers()
            lifecycle.flushCompleted.set(lifecycle.flushRequest.get())
        } catch (error: Throwable) {
            lifecycle.acceptingPublishers = false
            lifecycle.consumerFailed.set(true)
            lifecycle.running.set(false)
            producer.registry.forEach { entry -> entry.buffer.owner.get()?.let(LockSupport::unpark) }
            while (hasAdmittedProducers()) LockSupport.parkNanos(FLUSH_WAIT_POLL_NS)
            val lost = unaccountedAcceptedEventCount()
            addSaturating(lifecycle.shutdownLoss, lost)
            addSaturating(lifecycle.acceptedEventLoss, lost)
            flushQuality(lifecycle.activeWriter)
            retireAllProducerBuffers()
            lifecycle.activeWriter = null
            RuntimeHookGuard.rethrowFatal(error)
        } finally {
            producer.registry.forEach { entry -> entry.buffer.owner.get()?.let(LockSupport::unpark) }
            if (lifecycle.clearRequested.get()) {
                clearStoppedState()
            } else {
                lifecycle.consumerThread = null
            }
            // A clear racing the null publication either observes it or leaves this request.
            if (lifecycle.clearRequested.get()) clearStoppedState()
            try {
                completion.complete()
            } finally {
                hasConsumerOwner = false
                onStopped()
            }
        }
    }

    private fun drainBuffers(table: RuntimeGraphEdgeTable): Int {
        var total = 0
        for (entry in producer.registry) {
            val buffer = entry.buffer
            var drainedFromBuffer = 0
            while (drainedFromBuffer < MAX_DRAIN_PAGES_PER_BUFFER) {
                val position = buffer.tryClaimConsumer()
                if (position < 0L) break
                val page = buffer.pageAt(position)
                var entry = page.nextOccupiedIndex(0)
                while (entry >= 0) {
                    val limit = effectiveMaxKeys()
                    if (!table.add(page, entry, limit)) {
                        emitAll(table)
                        check(table.add(page, entry, effectiveMaxKeys())) {
                            "Runtime graph edge did not fit an empty aggregate table"
                        }
                    }
                    entry = page.nextOccupiedIndex(entry + 1)
                }
                buffer.release(position)
                if (buffer.producerWaiting) {
                    buffer.producerWaiting = false
                    buffer.owner.get()?.let(LockSupport::unpark)
                }
                total++
                drainedFromBuffer++
            }
        }
        return total
    }

    private fun acquireProducer(buffer: RuntimeGraphAggregateBuffer): Boolean {
        while (true) {
            while (buffer.rotationRequested) {
                // Stop closes new admission, but an already admitted exit still owns its slot.
                // Let the in-progress rotation finish within the original producer deadline.
                if (!awaitAdmission(buffer, ROTATION_PARK_NS, allowStopping = true)) return false
            }
            if (lifecycle.consumerFailed.get()) return false
            buffer.producerActive = true
            if (!buffer.rotationRequested) return true
            buffer.producerActive = false
            // Rotation may already have ended; bound retries without introducing a needless park.
            if (!canAwaitAdmission(buffer, allowStopping = true)) return false
        }
    }

    private fun recordShutdownLoss() {
        lifecycle.shutdownLoss.incrementAndGet()
    }

    private fun rotateProducerPages() {
        for (entry in producer.registry) {
            val buffer = entry.buffer
            buffer.rotationRequested = true
            while (buffer.producerActive) LockSupport.parkNanos(ROTATION_PARK_NS)
            buffer.publishActivePage()
            buffer.rotationRequested = false
            buffer.owner.get()?.let(LockSupport::unpark)
        }
    }

    private fun trimEmptyProducerPages() {
        for (entry in producer.registry) {
            val buffer = entry.buffer
            buffer.rotationRequested = true
            while (buffer.producerActive) LockSupport.parkNanos(ROTATION_PARK_NS)
            buffer.trimEmptyPages()
            buffer.rotationRequested = false
            buffer.owner.get()?.let(LockSupport::unpark)
        }
    }

    private fun wakeConsumer() {
        if (lifecycle.producerWake.tryRequest()) lifecycle.consumerThread?.let(LockSupport::unpark)
    }

    private fun effectiveMaxKeys(): Int = maxKeys.getAsInt().coerceAtLeast(1)

    private fun drainAllBuffers(table: RuntimeGraphEdgeTable) {
        while (drainBuffers(table) > 0) Unit
    }

    private fun emitAll(table: RuntimeGraphEdgeTable) {
        val writer = lifecycle.activeWriter
        while (table.size > 0) {
            val batch = consumer.batchPool.acquire()
            table.drainInto(batch)
            val logicalEvents = batch.logicalEventCount()
            consumer.aggregatedEdgeKeys.addAndGet(batch.size.toLong())
            batchObserver?.invoke(batch)
            val admitted = if (writer == null) {
                false
            } else {
                try {
                    writer.runtimeCalls(batch)
                } catch (error: Throwable) {
                    RuntimeHookGuard.rethrowFatal(error)
                    false
                }
            }
            if (!admitted) {
                batch.recycle()
                consumer.writerRejectionLoss.addAndGet(logicalEvents)
                lifecycle.acceptedEventLoss.addAndGet(logicalEvents)
            } else {
                consumer.emitted.addAndGet(logicalEvents)
            }
        }
    }

    private fun flushQuality(writer: AsyncLogWriter?) {
        val target = writer ?: return
        recordAndResetQuality(target, QualityCounterId.RUNTIME_GRAPH_SHUTDOWN_LOSS, lifecycle.shutdownLoss)
        recordAndResetQuality(target, QualityCounterId.RUNTIME_GRAPH_WRITER_REJECTION_LOSS, consumer.writerRejectionLoss)
        recordAndResetQuality(target, QualityCounterId.RUNTIME_GRAPH_PRODUCER_CAPACITY_LOSS, producer.capacityLoss)
        recordAndResetQuality(target, QualityCounterId.RUNTIME_STACK_MISMATCH, producer.stackMismatch)
        recordAndResetQuality(target, QualityCounterId.RUNTIME_GRAPH_STORAGE_SKIPPED_ENTRY_TOTAL,
            producer.storageSkippedEntries)
        recordAndResetQuality(target, QualityCounterId.RUNTIME_GRAPH_BACKPRESSURE_COUNT, producer.backpressureCount)
        recordAndResetQuality(target, QualityCounterId.RUNTIME_GRAPH_BACKPRESSURE_NANOS, producer.backpressureNanos)
        recordDelta(target, QualityCounterId.RUNTIME_GRAPH_INPUT_TOTAL, producerAttemptedTotal(), lifecycle.reportedAttempted)
        recordDelta(target, QualityCounterId.RUNTIME_GRAPH_EMITTED_TOTAL, consumer.emitted.get(), lifecycle.reportedEmitted)
    }

    private fun recordDelta(writer: AsyncLogWriter, counterId: Int, current: Long, reported: AtomicLong) {
        val previous = reported.getAndSet(current)
        if (current > previous) writer.recordQuality(counterId, current - previous)
    }

    private fun producerAttemptedTotal(): Long {
        var total = producer.retiredAttempted.get()
        producer.registry.forEach { entry -> total = saturatingAdd(total, entry.buffer.attemptedCount()) }
        return total
    }

    private fun producerAcceptedTotal(): Long {
        var total = producer.retiredAccepted.get()
        producer.registry.forEach { entry -> total = saturatingAdd(total, entry.buffer.acceptedCount()) }
        return total
    }

    private fun unaccountedAcceptedEventCount(): Long {
        val accounted = saturatingAdd(consumer.emitted.get(), lifecycle.acceptedEventLoss.get())
        return (producerAcceptedTotal() - accounted).coerceAtLeast(0L)
    }

    private inline fun <T> withCurrentStack(absent: T, read: (RuntimeCallStack) -> T): T {
        if (!lifecycle.acceptingPublishers) return absent
        val state = producer.threadState.get()?.get() ?: return absent
        // Reads also own the arrays until completion. Preserve an outer entry admission when a
        // context callback reads the current stack on the same producer thread.
        val alreadyAdmitted = state.buffer.producerAdmitted
        state.buffer.producerAdmitted = true
        try {
            if (!lifecycle.acceptingPublishers || state.epoch != producer.epoch.get()) return absent
            return read(state.stack)
        } finally {
            if (!alreadyAdmitted) releaseProducer(state)
        }
    }

    private fun hasBufferedEvents(): Boolean {
        for (entry in producer.registry) {
            if (entry.buffer.hasPublishedPages()) return true
        }
        return false
    }

    private fun hasAdmittedProducers(): Boolean {
        if (producer.registering.get() != 0L) return true
        for (entry in producer.registry) {
            val buffer = entry.buffer
            if (buffer.producerActive || buffer.producerAdmitted) return true
        }
        return false
    }

    private fun reclaimDeadBuffers() {
        for (entry in producer.registry) {
            val buffer = entry.buffer
            if (buffer.owner.get()?.isAlive != true && !buffer.hasPublishedPages() && !buffer.hasActiveData()) {
                if (producer.registry.remove(entry)) {
                    addSaturating(producer.retiredAttempted, buffer.attemptedCount())
                    addSaturating(producer.retiredAccepted, buffer.acceptedCount())
                    entry.releaseStorage()
                }
            }
        }
    }

    private fun clearRegistry() {
        while (true) producer.registry.poll()?.releaseStorage() ?: break
    }

    private fun retireAllProducerBuffers() {
        while (true) {
            val entry = producer.registry.poll() ?: break
            val buffer = entry.buffer
            addSaturating(producer.retiredAttempted, buffer.attemptedCount())
            addSaturating(producer.retiredAccepted, buffer.acceptedCount())
            entry.releaseStorage()
        }
        storageBudget.close()
    }

    private fun clearStoppedState() {
        if (!stateCleared.compareAndSet(false, true)) return
        clearRegistry()
        storageBudget.close()
        lifecycle.activeWriter = null
        lifecycle.flushRequest.set(0L)
        lifecycle.flushCompleted.set(0L)
        lifecycle.producerWake.clear()
        clearQuality()
        lifecycle.clearRequested.set(false)
        // Publish the stopped state only after all state shared with a future consumer is reset.
        lifecycle.consumerThread = null
    }

    private fun clearQuality() {
        producer.retiredAccepted.set(0L)
        producer.retiredAttempted.set(0L)
        consumer.emitted.set(0L)
        consumer.aggregatedEdgeKeys.set(0L)
        lifecycle.acceptedEventLoss.set(0L)
        lifecycle.shutdownLoss.set(0L)
        consumer.writerRejectionLoss.set(0L)
        producer.capacityLoss.set(0L)
        producer.stackMismatch.set(0L)
        producer.storageSkippedEntries.set(0L)
        producer.backpressureCount.set(0L)
        producer.backpressureNanos.set(0L)
        lifecycle.reportedAttempted.set(0L)
        lifecycle.reportedEmitted.set(0L)
    }

    private companion object {
        const val CONSUMER_NAME = "JankHunterGraph"
        val DEFAULT_ADMISSION_WAIT_NS: Long = TimeUnit.MILLISECONDS.toNanos(5L)
        const val DISABLED_TOKEN = 0L
        const val NO_FLUSH_REQUEST = -1L
        const val MAX_DRAIN_PAGES_PER_BUFFER = RUNTIME_GRAPH_PAGE_QUEUE_CAPACITY
        const val MAX_FLUSH_RECORDS = RUNTIME_GRAPH_MAX_FLUSH_RECORDS
        const val RUNTIME_BATCH_POOL_CAPACITY = 256
        // Runtime edges are already exact aggregates. A longer bounded window cuts duplicate
        // records and dictionary churn without sampling calls; explicit snapshots and shutdowns
        // still force an immediate frontier.
        const val DEFAULT_PERIODIC_FLUSH_INTERVAL_MS = 30_000L
        const val FLUSH_WAIT_POLL_NS = 1_000_000L
        const val SHUTDOWN_TIMEOUT_MS = 2_000L
        const val SHUTDOWN_JOIN_POLL_MS = 50L
        const val BACKPRESSURE_PARK_NS = 100_000L
        const val ROTATION_PARK_NS = 50_000L
        // The graph consumer drains only published pages. Matching normal app priority shortens
        // producer stalls under bursts; the former lowered priority directly amplified backlog.
        const val GRAPH_CONSUMER_PRIORITY = Process.THREAD_PRIORITY_DEFAULT
    }
}
