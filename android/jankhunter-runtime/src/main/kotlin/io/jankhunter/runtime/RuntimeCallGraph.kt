package io.jankhunter.runtime

import android.os.Process
import io.jankhunter.runtime.internal.saturatingAdd
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.QualityCounterId
import io.jankhunter.runtime.internal.io.RuntimeCallBatch
import java.util.concurrent.ConcurrentLinkedQueue
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
internal class RuntimeCallGraph(
    private val nowMs: RuntimeLongSource,
    private val captureScreen: () -> String?,
    private val captureFlow: () -> String?,
    private val captureStep: () -> String?,
    private val maxKeys: () -> Int,
    private val exactAdmission: () -> Boolean = { true },
    private val admissionWaitNanos: () -> Long = { DEFAULT_ADMISSION_WAIT_NS },
    private val periodicFlushIntervalMs: Long = DEFAULT_PERIODIC_FLUSH_INTERVAL_MS,
    private val consumerDelayNanos: Long = 0L,
    private val batchObserver: ((RuntimeCallBatch) -> Unit)? = null,
    private val publisherAdmissionObserver: (() -> Unit)? = null,
    private val consumerLoopObserver: (() -> Unit)? = null,
) {
    private val threadState = ThreadLocal<ProducerState>()
    private val buffers = ConcurrentLinkedQueue<RuntimeGraphAggregateBuffer>()
    private val epoch = AtomicLong(1L)
    private val running = AtomicBoolean(false)
    private val consumerFailed = AtomicBoolean(false)
    private val producerWakePending = AtomicBoolean()
    private val retiredProducerAccepted = AtomicLong()
    private val retiredProducerAttempted = AtomicLong()
    private val emitted = AtomicLong()
    private val aggregatedEdgeKeys = AtomicLong()
    private val acceptedEventLoss = AtomicLong()
    private val shutdownLoss = AtomicLong()
    private val writerRejectionLoss = AtomicLong()
    private val stackMismatch = AtomicLong()
    private val flushRequest = AtomicLong()
    private val flushCompleted = AtomicLong()
    private val backpressureCount = AtomicLong()
    private val backpressureNanos = AtomicLong()
    private val reportedAttempted = AtomicLong()
    private val reportedEmitted = AtomicLong()

    @Volatile
    private var consumer: Thread? = null

    @Volatile
    private var activeWriter: AsyncLogWriter? = null

    // Closing admission is a rare lifecycle write. An admitted producer is tracked by its owned
    // buffer, avoiding one globally contended atomic increment/decrement for every runtime edge.
    @Volatile
    private var acceptingPublishers = false

    fun resetFlushState(writer: AsyncLogWriter) {
        check(consumer?.isAlive != true) { "Runtime graph consumer is already running" }
        advanceRuntimeEpoch(epoch)
        clearRegistry()
        clearQuality()
        producerWakePending.set(false)
        activeWriter = writer
        consumerFailed.set(false)
        running.set(true)
        acceptingPublishers = true
        val graphThread = Thread(::runConsumerFailOpen, CONSUMER_NAME).apply { isDaemon = true }
        consumer = graphThread
        graphThread.start()
    }

    fun clear() {
        acceptingPublishers = false
        running.set(false)
        consumer?.let(LockSupport::unpark)
        clearRegistry()
        threadState.remove()
        activeWriter = null
        consumer = null
        flushRequest.set(0L)
        flushCompleted.set(0L)
        producerWakePending.set(false)
        clearQuality()
    }

    fun enter(methodId: Long, enabled: Boolean): Long = enter(methodId, null, enabled)

    fun enter(methodId: Long, methodName: String?, enabled: Boolean): Long {
        if (!enabled || !acceptingPublishers) return DISABLED_TOKEN
        val currentEpoch = epoch.get()
        var state = threadState.get()
        if (state == null || state.epoch != currentEpoch) {
            state = createProducerState(currentEpoch)
            threadState.set(state)
        }
        state.stack.push(
                methodId = methodId,
                methodName = methodName,
                startedAtMs = nowMs.getAsLong(),
                screen = captureScreen(),
                flow = captureFlow(),
                step = captureStep(),
            )
        return currentEpoch
    }

    fun exit(token: Long, methodId: Long) {
        if (token == DISABLED_TOKEN) return
        val state = threadState.get()
        if (state == null) {
            if (token == epoch.get() && acceptingPublishers) stackMismatch.incrementAndGet()
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
            if (!acceptingPublishers) consumer?.let(LockSupport::unpark)
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
        calleeName: String?,
        durationMs: Long,
        enabled: Boolean,
    ) {
        if (!enabled || !acceptingPublishers) return
        val currentEpoch = epoch.get()
        var state = threadState.get()
        if (state == null || state.epoch != currentEpoch) {
            state = createProducerState(currentEpoch)
            threadState.set(state)
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
                flow = captureFlow(),
                step = captureStep(),
                durationMs = durationMs.coerceAtLeast(0L),
            )
            if (published) buffer.recordAccepted()
        } finally {
            state.buffer.producerActive = false
            state.buffer.producerAdmitted = false
            if (!acceptingPublishers) consumer?.let(LockSupport::unpark)
        }
    }

    private fun exitAccepted(token: Long, methodId: Long, state: ProducerState) {
        val currentEpoch = epoch.get()
        if (state.epoch != currentEpoch) {
            state.stack.reset()
            return
        }
        if (token != currentEpoch) return
        if (!state.stack.pop(methodId)) {
            stackMismatch.incrementAndGet()
            return
        }
        if (!state.stack.hasPoppedParent) return
        val buffer = state.buffer
        buffer.recordAttempted()
        val durationMs = (nowMs.getAsLong() - state.stack.poppedStartedAtMs).coerceAtLeast(0L)
        val published = publishWithBackpressure(
            buffer = buffer,
            callerId = state.stack.poppedParentId,
            callerName = state.stack.poppedParentName,
            calleeId = methodId,
            calleeName = state.stack.poppedName,
            screen = state.stack.poppedScreen,
            flow = state.stack.poppedFlow,
            step = state.stack.poppedStep,
            durationMs = durationMs,
        )
        if (!published) return
        buffer.recordAccepted()
    }

    private fun publishWithBackpressure(
        buffer: RuntimeGraphAggregateBuffer,
        callerId: Long,
        callerName: String?,
        calleeId: Long,
        calleeName: String?,
        screen: String?,
        flow: String?,
        step: String?,
        durationMs: Long,
    ): Boolean {
        val exact = exactAdmission()
        val waitBudgetNs = if (exact) admissionWaitNanos().coerceAtLeast(0L) else 0L
        var blockedAtNs = 0L
        while (true) {
            val result = buffer.tryAdd(
                callerId = callerId,
                callerName = callerName,
                calleeId = calleeId,
                calleeName = calleeName,
                screen = screen,
                flow = flow,
                step = step,
                durationMs = durationMs,
            )
            if (result != RUNTIME_GRAPH_ADD_FULL) {
                if (result == RUNTIME_GRAPH_ADD_PAGE_PUBLISHED) wakeConsumer()
                break
            }
            val waitExpired = blockedAtNs != 0L &&
                System.nanoTime() - blockedAtNs >= waitBudgetNs
            if (consumerFailed.get() || !running.get() || !exact || waitBudgetNs == 0L || waitExpired) {
                buffer.producerWaiting = false
                if (consumerFailed.get() || !running.get()) shutdownLoss.incrementAndGet()
                return false
            }
            if (blockedAtNs == 0L) {
                blockedAtNs = System.nanoTime()
                backpressureCount.incrementAndGet()
            }
            buffer.producerWaiting = true
            wakeConsumer()
            if (buffer.rotationRequested) {
                buffer.producerActive = false
                if (!acquireProducer(buffer, allowClosedGate = true)) {
                    recordShutdownLoss()
                    return false
                }
            }
            val remainingNs = waitBudgetNs - (System.nanoTime() - blockedAtNs)
            if (remainingNs > 0L) LockSupport.parkNanos(minOf(BACKPRESSURE_PARK_NS, remainingNs))
        }
        buffer.producerWaiting = false
        if (blockedAtNs != 0L) {
            backpressureNanos.addAndGet((System.nanoTime() - blockedAtNs).coerceAtLeast(0L))
        }
        return true
    }

    fun flushBlocking(timeoutMs: Long): Boolean {
        if (!running.get()) return true
        val request = flushRequest.incrementAndGet()
        consumer?.let(LockSupport::unpark)
        val deadline = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(timeoutMs.coerceAtLeast(1L))
        while (flushCompleted.get() < request) {
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
        val graphThread = consumer ?: return
        acceptingPublishers = false
        running.set(false)
        LockSupport.unpark(graphThread)
        val exact = exactAdmission()
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
            shutdownLoss.addAndGet(lost)
            acceptedEventLoss.addAndGet(lost)
        }
        flushQuality(activeWriter)
    }

    internal fun acceptedForTest(): Long = producerAcceptedTotal()

    internal fun attemptedForTest(): Long = producerAttemptedTotal()

    internal fun emittedForTest(): Long = emitted.get()

    internal fun aggregatedEdgeKeysForTest(): Long = aggregatedEdgeKeys.get()

    internal fun acceptedEventLossForTest(): Long = acceptedEventLoss.get()

    internal fun currentThreadDepthForTest(): Int = threadState.get()?.stack?.depth ?: 0

    internal fun consumerForTest(): Thread? = consumer

    internal fun backpressureCountForTest(): Long = backpressureCount.get()

    internal fun acceptingPublishersForTest(): Boolean = acceptingPublishers

    internal fun fullyAccountedEventsForTest(): Long = emitted.get() + acceptedEventLoss.get()

    internal fun registeredProducerCountForTest(): Int {
        var result = 0
        buffers.forEach { result++ }
        return result
    }

    private fun createProducerState(currentEpoch: Long): ProducerState {
        val buffer = RuntimeGraphAggregateBuffer(Thread.currentThread())
        buffers.add(buffer)
        return ProducerState(currentEpoch, RuntimeCallStack(), buffer)
    }

    private fun runConsumerFailOpen() {
        val table = RuntimeGraphEdgeTable()
        try {
            try {
                Process.setThreadPriority(GRAPH_CONSUMER_PRIORITY)
            } catch (_: Throwable) {
            }
            var lastFlushAtMs = nowMs.getAsLong()
            while (running.get() || hasAdmittedProducers() || hasBufferedEvents()) {
                consumerLoopObserver?.invoke()
                producerWakePending.set(false)
                val drained = drainBuffers(table)
                if (drained > 0 && consumerDelayNanos > 0L) LockSupport.parkNanos(consumerDelayNanos)
                val request = flushRequest.get()
                val now = nowMs.getAsLong()
                val due = now - lastFlushAtMs >= periodicFlushIntervalMs.coerceAtLeast(1L)
                var completedRequest = NO_FLUSH_REQUEST
                if (request > flushCompleted.get() || due || !running.get()) {
                    rotateProducerPages()
                    drainAllBuffers(table)
                    emitAll(table)
                    flushQuality(activeWriter)
                    completedRequest = request
                    lastFlushAtMs = now
                }
                reclaimDeadBuffers()
                if (completedRequest != NO_FLUSH_REQUEST) flushCompleted.set(completedRequest)
                if (drained == 0 && running.get()) LockSupport.parkNanos(CONSUMER_PARK_NS)
            }
            rotateProducerPages()
            drainAllBuffers(table)
            emitAll(table)
            flushQuality(activeWriter)
            reclaimDeadBuffers()
            flushCompleted.set(flushRequest.get())
        } catch (_: Throwable) {
            acceptingPublishers = false
            consumerFailed.set(true)
            running.set(false)
            buffers.forEach { buffer -> buffer.owner.get()?.let(LockSupport::unpark) }
            while (hasAdmittedProducers()) LockSupport.parkNanos(FLUSH_WAIT_POLL_NS)
            val lost = saturatingAdd(bufferedEventCount(), table.logicalEventCount())
            shutdownLoss.addAndGet(lost.coerceAtLeast(1L))
            acceptedEventLoss.addAndGet(lost.coerceAtLeast(1L))
            flushQuality(activeWriter)
        } finally {
            buffers.forEach { buffer -> buffer.owner.get()?.let(LockSupport::unpark) }
            consumer = null
        }
    }

    private fun drainBuffers(table: RuntimeGraphEdgeTable): Int {
        var total = 0
        for (buffer in buffers) {
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
                if (consumerFailed.get() || (!allowClosedGate && !acceptingPublishers)) return false
                LockSupport.parkNanos(ROTATION_PARK_NS)
            }
            if (consumerFailed.get() || (!allowClosedGate && !acceptingPublishers)) return false
            buffer.producerActive = true
            if (!buffer.rotationRequested && (allowClosedGate || acceptingPublishers)) return true
            buffer.producerActive = false
            if (!allowClosedGate && !acceptingPublishers) return false
        }
    }

    private fun recordShutdownLoss() {
        shutdownLoss.incrementAndGet()
        acceptedEventLoss.incrementAndGet()
    }

    private fun rotateProducerPages() {
        for (buffer in buffers) {
            buffer.rotationRequested = true
            while (buffer.producerActive) LockSupport.parkNanos(ROTATION_PARK_NS)
            buffer.publishActivePage()
            buffer.rotationRequested = false
            buffer.owner.get()?.let(LockSupport::unpark)
        }
    }

    private fun wakeConsumer() {
        if (producerWakePending.compareAndSet(false, true)) consumer?.let(LockSupport::unpark)
    }

    private fun effectiveMaxKeys(): Int = maxKeys().coerceAtLeast(1)

    private fun drainAllBuffers(table: RuntimeGraphEdgeTable) {
        while (drainBuffers(table) > 0) Unit
    }

    private fun emitAll(table: RuntimeGraphEdgeTable) {
        val writer = activeWriter
        while (table.size > 0) {
            val batch = RuntimeCallBatch(MAX_FLUSH_RECORDS)
            table.drainInto(batch)
            val logicalEvents = batch.logicalEventCount()
            aggregatedEdgeKeys.addAndGet(batch.size.toLong())
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
                writerRejectionLoss.addAndGet(logicalEvents)
                acceptedEventLoss.addAndGet(logicalEvents)
            } else {
                emitted.addAndGet(logicalEvents)
            }
        }
    }

    private fun flushQuality(writer: AsyncLogWriter?) {
        val target = writer ?: return
        recordAndResetQuality(target, QualityCounterId.RUNTIME_GRAPH_SHUTDOWN_LOSS, shutdownLoss)
        recordAndResetQuality(target, QualityCounterId.RUNTIME_GRAPH_WRITER_REJECTION_LOSS, writerRejectionLoss)
        recordAndResetQuality(target, QualityCounterId.RUNTIME_STACK_MISMATCH, stackMismatch)
        recordAndResetQuality(target, QualityCounterId.RUNTIME_GRAPH_BACKPRESSURE_COUNT, backpressureCount)
        recordAndResetQuality(target, QualityCounterId.RUNTIME_GRAPH_BACKPRESSURE_NANOS, backpressureNanos)
        recordDelta(target, QualityCounterId.RUNTIME_GRAPH_INPUT_TOTAL, producerAttemptedTotal(), reportedAttempted)
        recordDelta(target, QualityCounterId.RUNTIME_GRAPH_EMITTED_TOTAL, emitted.get(), reportedEmitted)
    }

    private fun recordDelta(writer: AsyncLogWriter, counterId: Int, current: Long, reported: AtomicLong) {
        val previous = reported.getAndSet(current)
        if (current > previous) writer.recordQuality(counterId, current - previous)
    }

    private fun producerAttemptedTotal(): Long {
        var total = retiredProducerAttempted.get()
        buffers.forEach { buffer -> total = saturatingAdd(total, buffer.attemptedCount()) }
        return total
    }

    private fun producerAcceptedTotal(): Long {
        var total = retiredProducerAccepted.get()
        buffers.forEach { buffer -> total = saturatingAdd(total, buffer.acceptedCount()) }
        return total
    }

    private fun hasBufferedEvents(): Boolean {
        for (buffer in buffers) {
            if (buffer.hasPublishedPages()) return true
        }
        return false
    }

    private fun hasAdmittedProducers(): Boolean {
        for (buffer in buffers) {
            if (buffer.producerActive || buffer.producerAdmitted) return true
        }
        return false
    }

    private fun bufferedEventCount(): Long {
        var count = 0L
        for (buffer in buffers) {
            count = saturatingAdd(count, buffer.bufferedLogicalEventCount())
        }
        return count
    }

    private fun reclaimDeadBuffers() {
        for (buffer in buffers) {
            if (buffer.owner.get() == null && !buffer.hasPublishedPages() && !buffer.hasActiveData()) {
                if (buffers.remove(buffer)) {
                    addSaturating(retiredProducerAttempted, buffer.attemptedCount())
                    addSaturating(retiredProducerAccepted, buffer.acceptedCount())
                }
            }
        }
    }

    private fun clearRegistry() {
        while (true) buffers.poll()?.clear() ?: break
    }

    private fun clearQuality() {
        retiredProducerAccepted.set(0L)
        retiredProducerAttempted.set(0L)
        emitted.set(0L)
        aggregatedEdgeKeys.set(0L)
        acceptedEventLoss.set(0L)
        shutdownLoss.set(0L)
        writerRejectionLoss.set(0L)
        stackMismatch.set(0L)
        backpressureCount.set(0L)
        backpressureNanos.set(0L)
        reportedAttempted.set(0L)
        reportedEmitted.set(0L)
    }

    private class ProducerState(
        val epoch: Long,
        val stack: RuntimeCallStack,
        val buffer: RuntimeGraphAggregateBuffer,
    )

    private companion object {
        const val CONSUMER_NAME = "JankHunterGraph"
        val DEFAULT_ADMISSION_WAIT_NS: Long = TimeUnit.MILLISECONDS.toNanos(5L)
        const val DISABLED_TOKEN = 0L
        const val NO_FLUSH_REQUEST = -1L
        const val MAX_DRAIN_PAGES_PER_BUFFER = RUNTIME_GRAPH_PAGE_QUEUE_CAPACITY
        const val MAX_FLUSH_RECORDS = RUNTIME_GRAPH_MAX_FLUSH_RECORDS
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
        const val GRAPH_CONSUMER_PRIORITY = Process.THREAD_PRIORITY_DEFAULT + 1
    }
}
