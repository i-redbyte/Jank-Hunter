package io.jankhunter.runtime

import android.os.Process
import io.jankhunter.runtime.internal.saturatingAdd
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.QualityCounterId
import io.jankhunter.runtime.internal.io.RuntimeCallBatch
import java.lang.ref.WeakReference
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.locks.LockSupport

/**
 * Runtime call graph with an allocation-free application-thread publication path.
 *
 * Each producer owns its stack and exact aggregate pages. Only [JankHunterGraph] merges published
 * pages or touches the asynchronous writer. Context is captured at callee entry and is part of the
 * edge identity.
 */
internal class RuntimeCallGraph(
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
) {
    private val producer = RuntimeGraphProducer()
    private val consumer = RuntimeGraphConsumer(RUNTIME_BATCH_POOL_CAPACITY, MAX_FLUSH_RECORDS)
    private val lifecycle = RuntimeGraphLifecycle()

    fun resetFlushState(writer: AsyncLogWriter) {
        check(lifecycle.consumerThread?.isAlive != true) { "Runtime graph consumer is already running" }
        advanceRuntimeEpoch(producer.epoch)
        clearRegistry()
        clearQuality()
        lifecycle.producerWake.clear()
        lifecycle.activeWriter = writer
        lifecycle.consumerFailed.set(false)
        lifecycle.running.set(true)
        lifecycle.acceptingPublishers = true
        val graphThread = Thread(::runConsumerFailOpen, CONSUMER_NAME).apply { isDaemon = true }
        lifecycle.consumerThread = graphThread
        graphThread.start()
    }

    fun clear() {
        lifecycle.acceptingPublishers = false
        lifecycle.running.set(false)
        lifecycle.consumerThread?.let(LockSupport::unpark)
        clearRegistry()
        producer.threadState.remove()
        lifecycle.activeWriter = null
        lifecycle.consumerThread = null
        lifecycle.flushRequest.set(0L)
        lifecycle.flushCompleted.set(0L)
        lifecycle.producerWake.clear()
        clearQuality()
    }

    fun enter(methodId: Long, methodName: String, enabled: Boolean): Long {
        if (!enabled || !lifecycle.acceptingPublishers) return DISABLED_TOKEN
        val currentEpoch = producer.epoch.get()
        var state = producer.threadState.get()?.get()
        if (state == null || state.epoch != currentEpoch) {
            state = createProducerState(currentEpoch)
            producer.threadState.set(WeakReference(state))
        }
        state.stack.push(
                methodId = methodId,
                methodName = methodName,
                startedAtMs = nowMs.getAsLong(),
                screen = captureScreen(),
                operationId = captureOperationId.getAsLong(),
            )
        return currentEpoch
    }

    fun hasCurrentMethod(): Boolean = producer.threadState.get()?.get()?.stack?.hasCurrentMethod() == true

    fun currentMethodId(): Long = producer.threadState.get()?.get()?.stack?.currentMethodId() ?: 0L

    fun currentMethodName(): String? = producer.threadState.get()?.get()?.stack?.currentMethodName()

    fun exit(token: Long, methodId: Long) {
        if (token == DISABLED_TOKEN) return
        val state = producer.threadState.get()?.get()
        if (state == null) {
            if (token == producer.epoch.get() && lifecycle.acceptingPublishers) producer.stackMismatch.incrementAndGet()
            return
        }
        if (!acquireProducer(state.buffer, allowClosedGate = false)) return
        state.buffer.producerAdmitted = true
        try {
            publisherAdmissionObserver?.invoke()
            exitAccepted(token, methodId, state)
        } finally {
            state.buffer.producerActive = false
            state.buffer.producerAdmitted = false
            if (!lifecycle.acceptingPublishers) lifecycle.consumerThread?.let(LockSupport::unpark)
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
        var state = producer.threadState.get()?.get()
        if (state == null || state.epoch != currentEpoch) {
            state = createProducerState(currentEpoch)
            producer.threadState.set(WeakReference(state))
        }
        if (!acquireProducer(state.buffer, allowClosedGate = false)) return
        state.buffer.producerAdmitted = true
        try {
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
            if (!lifecycle.acceptingPublishers) lifecycle.consumerThread?.let(LockSupport::unpark)
        }
    }

    private fun exitAccepted(token: Long, methodId: Long, state: RuntimeGraphProducerState) {
        val currentEpoch = producer.epoch.get()
        if (state.epoch != currentEpoch) {
            state.stack.reset()
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
        val exact = exactAdmission.getAsBoolean()
        val waitBudgetNs = if (exact) admissionWaitNanos.getAsLong().coerceAtLeast(0L) else 0L
        var blockedAtNs = 0L
        while (true) {
            val result = buffer.tryAdd(
                callerId = callerId,
                callerName = callerName,
                calleeId = calleeId,
                calleeName = calleeName,
                screen = screen,
                operationId = operationId,
                durationMs = durationMs,
            )
            if (result != RUNTIME_GRAPH_ADD_FULL) {
                if (result == RUNTIME_GRAPH_ADD_PAGE_PUBLISHED) wakeConsumer()
                break
            }
            val waitExpired = blockedAtNs != 0L &&
                System.nanoTime() - blockedAtNs >= waitBudgetNs
            if (lifecycle.consumerFailed.get() || !lifecycle.running.get() || !exact || waitBudgetNs == 0L || waitExpired) {
                buffer.producerWaiting = false
                recordGraphBackpressure(blockedAtNs)
                if (lifecycle.consumerFailed.get() || !lifecycle.running.get()) {
                    lifecycle.shutdownLoss.incrementAndGet()
                } else {
                    producer.capacityLoss.incrementAndGet()
                }
                return false
            }
            if (blockedAtNs == 0L) {
                blockedAtNs = System.nanoTime()
                producer.backpressureCount.incrementAndGet()
            }
            buffer.producerWaiting = true
            wakeConsumer()
            if (buffer.rotationRequested) {
                buffer.producerActive = false
                if (!acquireProducer(buffer, allowClosedGate = true)) {
                    recordGraphBackpressure(blockedAtNs)
                    recordShutdownLoss()
                    return false
                }
            }
            val remainingNs = waitBudgetNs - (System.nanoTime() - blockedAtNs)
            if (remainingNs > 0L) LockSupport.parkNanos(minOf(BACKPRESSURE_PARK_NS, remainingNs))
        }
        buffer.producerWaiting = false
        recordGraphBackpressure(blockedAtNs)
        return true
    }

    private fun recordGraphBackpressure(blockedAtNs: Long) {
        if (blockedAtNs != 0L) {
            producer.backpressureNanos.addAndGet((System.nanoTime() - blockedAtNs).coerceAtLeast(1L))
        }
    }

    fun flushBlocking(timeoutMs: Long): Boolean {
        if (!lifecycle.running.get()) return true
        val request = lifecycle.flushRequest.incrementAndGet()
        lifecycle.consumerThread?.let(LockSupport::unpark)
        val deadline = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(timeoutMs.coerceAtLeast(1L))
        while (lifecycle.flushCompleted.get() < request) {
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

    fun flushForShutdown() {
        val graphThread = lifecycle.consumerThread ?: return
        lifecycle.acceptingPublishers = false
        lifecycle.running.set(false)
        LockSupport.unpark(graphThread)
        val exact = exactAdmission.getAsBoolean()
        val deadline = if (exact) Long.MAX_VALUE else {
            System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(SHUTDOWN_TIMEOUT_MS)
        }
        var interrupted = false
        while (graphThread.isAlive) {
            val waitMs = if (exact) {
                SHUTDOWN_JOIN_POLL_MS
            } else {
                val remaining = deadline - System.nanoTime()
                if (remaining <= 0L) break
                TimeUnit.NANOSECONDS.toMillis(remaining).coerceIn(1L, SHUTDOWN_JOIN_POLL_MS)
            }
            try {
                graphThread.join(waitMs)
            } catch (_: InterruptedException) {
                interrupted = true
                if (!exact) break
            }
        }
        if (interrupted) Thread.currentThread().interrupt()
        if (graphThread.isAlive) {
            val lost = bufferedEventCount()
            lifecycle.shutdownLoss.addAndGet(lost)
            lifecycle.acceptedEventLoss.addAndGet(lost)
        }
        flushQuality(lifecycle.activeWriter)
    }

    internal fun acceptedForTest(): Long = producerAcceptedTotal()

    internal fun attemptedForTest(): Long = producerAttemptedTotal()

    internal fun emittedForTest(): Long = consumer.emitted.get()

    internal fun aggregatedEdgeKeysForTest(): Long = consumer.aggregatedEdgeKeys.get()

    internal fun acceptedEventLossForTest(): Long = lifecycle.acceptedEventLoss.get()

    internal fun currentThreadDepthForTest(): Int = producer.threadState.get()?.get()?.stack?.depth ?: 0

    internal fun consumerForTest(): Thread? = lifecycle.consumerThread

    internal fun backpressureCountForTest(): Long = producer.backpressureCount.get()

    internal fun producerCapacityLossForTest(): Long = producer.capacityLoss.get()

    internal fun acceptingPublishersForTest(): Boolean = lifecycle.acceptingPublishers

    internal fun registeredProducerCountForTest(): Int {
        var result = 0
        producer.registry.forEach { result++ }
        return result
    }

    private fun createProducerState(currentEpoch: Long): RuntimeGraphProducerState {
        val buffer = RuntimeGraphAggregateBuffer(Thread.currentThread())
        return RuntimeGraphProducerState(currentEpoch, RuntimeCallStack(), buffer).also(producer.registry::add)
    }

    private fun runConsumerFailOpen() {
        val table = RuntimeGraphEdgeTable()
        try {
            try {
                Process.setThreadPriority(GRAPH_CONSUMER_PRIORITY)
            } catch (_: Throwable) {
            }
            var lastFlushAtMs = nowMs.getAsLong()
            while (lifecycle.running.get() || hasAdmittedProducers() || hasBufferedEvents()) {
                consumerLoopObserver?.invoke()
                lifecycle.producerWake.clear()
                val drained = drainBuffers(table)
                if (drained > 0 && consumerDelayNanos > 0L) LockSupport.parkNanos(consumerDelayNanos)
                val request = lifecycle.flushRequest.get()
                val now = nowMs.getAsLong()
                val due = now - lastFlushAtMs >= periodicFlushIntervalMs.coerceAtLeast(1L)
                var completedRequest = NO_FLUSH_REQUEST
                if (request > lifecycle.flushCompleted.get() || due || !lifecycle.running.get()) {
                    rotateProducerPages()
                    drainAllBuffers(table)
                    emitAll(table)
                    flushQuality(lifecycle.activeWriter)
                    completedRequest = request
                    lastFlushAtMs = now
                }
                reclaimDeadBuffers()
                if (completedRequest != NO_FLUSH_REQUEST) lifecycle.flushCompleted.set(completedRequest)
                if (drained == 0 && lifecycle.running.get()) LockSupport.parkNanos(CONSUMER_PARK_NS)
            }
            rotateProducerPages()
            drainAllBuffers(table)
            emitAll(table)
            flushQuality(lifecycle.activeWriter)
            reclaimDeadBuffers()
            lifecycle.flushCompleted.set(lifecycle.flushRequest.get())
        } catch (_: Throwable) {
            lifecycle.acceptingPublishers = false
            lifecycle.consumerFailed.set(true)
            lifecycle.running.set(false)
            producer.registry.forEach { entry -> entry.buffer.owner.get()?.let(LockSupport::unpark) }
            while (hasAdmittedProducers()) LockSupport.parkNanos(FLUSH_WAIT_POLL_NS)
            val lost = saturatingAdd(bufferedEventCount(), table.logicalEventCount())
            lifecycle.shutdownLoss.addAndGet(lost.coerceAtLeast(1L))
            lifecycle.acceptedEventLoss.addAndGet(lost.coerceAtLeast(1L))
            flushQuality(lifecycle.activeWriter)
        } finally {
            producer.registry.forEach { entry -> entry.buffer.owner.get()?.let(LockSupport::unpark) }
            lifecycle.consumerThread = null
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

    private fun acquireProducer(buffer: RuntimeGraphAggregateBuffer, allowClosedGate: Boolean): Boolean {
        while (true) {
            while (buffer.rotationRequested) {
                if (lifecycle.consumerFailed.get() || (!allowClosedGate && !lifecycle.acceptingPublishers)) return false
                LockSupport.parkNanos(ROTATION_PARK_NS)
            }
            if (lifecycle.consumerFailed.get() || (!allowClosedGate && !lifecycle.acceptingPublishers)) return false
            buffer.producerActive = true
            if (!buffer.rotationRequested && (allowClosedGate || lifecycle.acceptingPublishers)) return true
            buffer.producerActive = false
            if (!allowClosedGate && !lifecycle.acceptingPublishers) return false
        }
    }

    private fun recordShutdownLoss() {
        lifecycle.shutdownLoss.incrementAndGet()
        lifecycle.acceptedEventLoss.incrementAndGet()
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
                } catch (_: Throwable) {
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

    private fun hasBufferedEvents(): Boolean {
        for (entry in producer.registry) {
            if (entry.buffer.hasPublishedPages()) return true
        }
        return false
    }

    private fun hasAdmittedProducers(): Boolean {
        for (entry in producer.registry) {
            val buffer = entry.buffer
            if (buffer.producerActive || buffer.producerAdmitted) return true
        }
        return false
    }

    private fun bufferedEventCount(): Long {
        var count = 0L
        for (entry in producer.registry) {
            count = saturatingAdd(count, entry.buffer.bufferedLogicalEventCount())
        }
        return count
    }

    private fun reclaimDeadBuffers() {
        for (entry in producer.registry) {
            val buffer = entry.buffer
            if (buffer.owner.get() == null && !buffer.hasPublishedPages() && !buffer.hasActiveData()) {
                if (producer.registry.remove(entry)) {
                    addSaturating(producer.retiredAttempted, buffer.attemptedCount())
                    addSaturating(producer.retiredAccepted, buffer.acceptedCount())
                }
            }
        }
    }

    private fun clearRegistry() {
        while (true) producer.registry.poll()?.buffer?.clear() ?: break
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
        const val CONSUMER_PARK_NS = 50_000_000L
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
