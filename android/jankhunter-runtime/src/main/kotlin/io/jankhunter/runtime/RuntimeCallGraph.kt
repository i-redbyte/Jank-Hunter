package io.jankhunter.runtime

import android.os.Process
import io.jankhunter.runtime.internal.saturatingAdd
import io.jankhunter.runtime.internal.concurrent.SpscSlotSequencer
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.QualityCounterId
import io.jankhunter.runtime.internal.io.RuntimeCallBatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReferenceArray
import java.util.concurrent.locks.LockSupport

/**
 * Runtime call graph with a wait-free application-thread publication path.
 *
 * Each producer owns its stack and SPSC ring. Only [JankHunterGraph] touches aggregation or the
 * asynchronous writer. Context is captured at callee entry and is part of the edge identity.
 */
internal class RuntimeCallGraph(
    private val nowMs: RuntimeLongSource,
    private val captureScreen: () -> String?,
    private val captureFlow: () -> String?,
    private val captureStep: () -> String?,
    private val maxKeys: () -> Int,
    private val consumerDelayNanos: Long = 0L,
    private val batchObserver: ((RuntimeCallBatch) -> Unit)? = null,
) {
    private val threadState = ThreadLocal<ProducerState>()
    private val buffers = AtomicReferenceArray<RuntimeGraphEdgeBuffer>(MAX_PRODUCERS)
    private val epoch = AtomicLong(1L)
    private val running = AtomicBoolean(false)
    private val producerWakePending = AtomicBoolean()
    private val accepted = AtomicLong()
    private val attempted = AtomicLong()
    private val emitted = AtomicLong()
    private val aggregatedEdgeKeys = AtomicLong()
    private val acceptedEventLoss = AtomicLong()
    private val bufferCapacityLoss = AtomicLong()
    private val producerRegistryLoss = AtomicLong()
    private val edgeCapacityLoss = AtomicLong()
    private val staleEpochLoss = AtomicLong()
    private val shutdownLoss = AtomicLong()
    private val writerRejectionLoss = AtomicLong()
    private val stackMismatch = AtomicLong()
    private val stackCapacityLoss = AtomicLong()
    private val flushRequest = AtomicLong()
    private val flushCompleted = AtomicLong()
    private val shadowCapacityLoss = AtomicLong()
    private val circuitBreakerOpen = AtomicBoolean(false)
    private val circuitBreakerLossBudget = AtomicLong()
    private val circuitBreakerTrips = AtomicLong()
    private val circuitBreakerDrops = AtomicLong()
    private val reportedAttempted = AtomicLong()
    private val reportedEmitted = AtomicLong()
    private val preAdmissionLossTotal = AtomicLong()
    private val circuitBreakerDropTotal = AtomicLong()

    @Volatile
    private var activeMode = JankHunterRuntimeGraphMode.BUFFERED

    @Volatile
    private var lastShadowComparison = RuntimeGraphShadowComparisonResult.EMPTY

    @Volatile
    private var consumer: Thread? = null

    @Volatile
    private var activeWriter: AsyncLogWriter? = null

    fun resetFlushState(
        writer: AsyncLogWriter,
        mode: JankHunterRuntimeGraphMode = JankHunterRuntimeGraphMode.BUFFERED,
    ) {
        check(consumer?.isAlive != true) { "Runtime graph consumer is already running" }
        advanceRuntimeEpoch(epoch)
        clearRegistry()
        clearQuality()
        producerWakePending.set(false)
        activeMode = mode
        lastShadowComparison = RuntimeGraphShadowComparisonResult.EMPTY
        activeWriter = writer
        running.set(true)
        val graphThread = Thread(::runConsumerFailOpen, CONSUMER_NAME).apply { isDaemon = true }
        consumer = graphThread
        graphThread.start()
    }

    fun clear() {
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
        if (!enabled || !running.get()) return DISABLED_TOKEN
        val currentEpoch = epoch.get()
        var state = threadState.get()
        if (state == null || state.epoch != currentEpoch) {
            state = createProducerState(currentEpoch)
            threadState.set(state)
        }
        if (!state.stack.push(
                methodId = methodId,
                methodName = methodName,
                startedAtMs = nowMs.getAsLong(),
                screen = captureScreen(),
                flow = captureFlow(),
                step = captureStep(),
            )
        ) {
            stackCapacityLoss.incrementAndGet()
            return DISABLED_TOKEN
        }
        return currentEpoch
    }

    fun exit(token: Long, methodId: Long) {
        if (token == DISABLED_TOKEN) return
        val currentEpoch = epoch.get()
        val state = threadState.get()
        if (state == null) {
            if (token == currentEpoch) stackMismatch.incrementAndGet()
            return
        }
        if (state.epoch != currentEpoch) {
            state.stack.reset()
            return
        }
        if (token != currentEpoch) return
        if (!state.stack.pop(methodId)) {
            stackMismatch.incrementAndGet()
            return
        }
        if (!state.stack.hasPoppedParent || !running.get()) return
        attempted.incrementAndGet()
        if (circuitBreakerOpen.get()) {
            circuitBreakerDrops.incrementAndGet()
            circuitBreakerDropTotal.incrementAndGet()
            return
        }
        val buffer = state.buffer
        if (buffer == null) {
            producerRegistryLoss.incrementAndGet()
            preAdmissionLossTotal.incrementAndGet()
            recordCircuitBreakerLoss()
            return
        }
        val durationMs = (nowMs.getAsLong() - state.stack.poppedStartedAtMs).coerceAtLeast(0L)
        if (!buffer.publish(
                eventEpoch = currentEpoch,
                callerId = state.stack.poppedParentId,
                callerName = state.stack.poppedParentName,
                calleeId = methodId,
                calleeName = state.stack.poppedName,
                screen = state.stack.poppedScreen,
                flow = state.stack.poppedFlow,
                step = state.stack.poppedStep,
                durationMs = durationMs,
            )
        ) {
            bufferCapacityLoss.incrementAndGet()
            preAdmissionLossTotal.incrementAndGet()
            recordCircuitBreakerLoss()
            return
        }
        accepted.incrementAndGet()
        wakeConsumer()
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
        running.set(false)
        LockSupport.unpark(graphThread)
        val deadline = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(SHUTDOWN_TIMEOUT_MS)
        var interrupted = false
        while (graphThread.isAlive) {
            val remaining = deadline - System.nanoTime()
            if (remaining <= 0L) break
            try {
                graphThread.join(
                    TimeUnit.NANOSECONDS.toMillis(remaining).coerceIn(1L, SHUTDOWN_JOIN_POLL_MS),
                )
            } catch (_: InterruptedException) {
                interrupted = true
                break
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

    internal fun acceptedForTest(): Long = accepted.get()

    internal fun attemptedForTest(): Long = attempted.get()

    internal fun emittedForTest(): Long = emitted.get()

    internal fun aggregatedEdgeKeysForTest(): Long = aggregatedEdgeKeys.get()

    internal fun acceptedEventLossForTest(): Long = acceptedEventLoss.get()

    internal fun currentThreadDepthForTest(): Int = threadState.get()?.stack?.depth ?: 0

    internal fun consumerForTest(): Thread? = consumer

    internal fun shadowComparisonForTest(): RuntimeGraphShadowComparisonResult = lastShadowComparison

    internal fun circuitBreakerOpenForTest(): Boolean = circuitBreakerOpen.get()

    internal fun circuitBreakerDropsForTest(): Long = circuitBreakerDrops.get()

    internal fun fullyAccountedEventsForTest(): Long = emitted.get() + acceptedEventLoss.get() +
        preAdmissionLossTotal.get() + circuitBreakerDropTotal.get()

    internal fun registeredProducerCountForTest(): Int {
        var result = 0
        for (index in 0 until buffers.length()) if (buffers.get(index) != null) result++
        return result
    }

    private fun createProducerState(currentEpoch: Long): ProducerState {
        val buffer = registerFirstAvailable(buffers) { RuntimeGraphEdgeBuffer(Thread.currentThread()) }
        return ProducerState(currentEpoch, RuntimeCallStack(), buffer)
    }

    private fun runConsumerFailOpen() {
        val mode = activeMode
        val table = RuntimeGraphEdgeTable(contextAware = mode != JankHunterRuntimeGraphMode.LEGACY)
        val shadow = if (mode == JankHunterRuntimeGraphMode.SHADOW) RuntimeGraphShadowComparison() else null
        try {
            try {
                Process.setThreadPriority(Process.THREAD_PRIORITY_BACKGROUND)
            } catch (_: Throwable) {
            }
            var lastFlushAtMs = nowMs.getAsLong()
            while (running.get() || hasBufferedEvents()) {
                producerWakePending.set(false)
                val drained = drainBuffers(table, shadow)
                if (drained > 0 && consumerDelayNanos > 0L) LockSupport.parkNanos(consumerDelayNanos)
                val request = flushRequest.get()
                val now = nowMs.getAsLong()
                val due = now - lastFlushAtMs >= FLUSH_INTERVAL_MS
                var completedRequest = NO_FLUSH_REQUEST
                if (request > flushCompleted.get() || due || !running.get()) {
                    if (request > flushCompleted.get() || !running.get()) {
                        drainAllBuffers(table, shadow)
                    }
                    flushShadowComparison(shadow)
                    emitAll(table)
                    flushQuality(activeWriter)
                    completedRequest = request
                    lastFlushAtMs = now
                }
                reclaimDeadBuffers()
                if (completedRequest != NO_FLUSH_REQUEST) flushCompleted.set(completedRequest)
                if (drained == 0 && running.get()) LockSupport.parkNanos(CONSUMER_PARK_NS)
            }
            flushShadowComparison(shadow)
            emitAll(table)
            flushQuality(activeWriter)
            reclaimDeadBuffers()
            flushCompleted.set(flushRequest.get())
        } catch (_: Throwable) {
            val lost = saturatingAdd(bufferedEventCount(), table.logicalEventCount())
            shutdownLoss.addAndGet(lost)
            acceptedEventLoss.addAndGet(lost)
            flushQuality(activeWriter)
        } finally {
            consumer = null
        }
    }

    private fun drainBuffers(table: RuntimeGraphEdgeTable, shadow: RuntimeGraphShadowComparison?): Int {
        var total = 0
        for (index in 0 until buffers.length()) {
            val buffer = buffers.get(index) ?: continue
            var drainedFromBuffer = 0
            while (drainedFromBuffer < MAX_DRAIN_PER_BUFFER) {
                val position = buffer.sequencer.tryClaimConsumer()
                if (position == SpscSlotSequencer.NO_POSITION) break
                val slot = buffer.sequencer.slotIndex(position)
                if (buffer.epochs[slot] != epoch.get()) {
                    staleEpochLoss.incrementAndGet()
                    acceptedEventLoss.incrementAndGet()
                } else {
                    val limit = maxKeys().coerceAtLeast(0)
                    if (!table.add(buffer, slot, limit)) {
                        edgeCapacityLoss.incrementAndGet()
                        acceptedEventLoss.incrementAndGet()
                        recordCircuitBreakerLoss()
                    } else if (shadow != null && !shadow.record(buffer, slot, limit)) {
                        shadowCapacityLoss.incrementAndGet()
                    }
                }
                buffer.clearReferences(slot)
                buffer.sequencer.release(position)
                total++
                drainedFromBuffer++
            }
        }
        return total
    }

    private fun wakeConsumer() {
        if (producerWakePending.compareAndSet(false, true)) consumer?.let(LockSupport::unpark)
    }

    private fun drainAllBuffers(table: RuntimeGraphEdgeTable, shadow: RuntimeGraphShadowComparison?) {
        while (drainBuffers(table, shadow) > 0) Unit
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
                recordCircuitBreakerLoss(logicalEvents)
            } else {
                emitted.addAndGet(logicalEvents)
            }
        }
    }

    private fun flushShadowComparison(shadow: RuntimeGraphShadowComparison?) {
        val comparison = shadow?.compareAndClear() ?: return
        lastShadowComparison = comparison
        val writer = activeWriter ?: return
        writer.counter("jankhunter.runtime_graph.shadow.missing_edges.count", comparison.missingEdges)
        writer.counter("jankhunter.runtime_graph.shadow.extra_edges.count", comparison.extraEdges)
        writer.counter("jankhunter.runtime_graph.shadow.count_differences.count", comparison.countDifferences)
        writer.counter("jankhunter.runtime_graph.shadow.duration_differences.count", comparison.durationDifferences)
        writer.counter("jankhunter.runtime_graph.shadow.context_splits.count", comparison.contextSplits)
    }

    private fun flushQuality(writer: AsyncLogWriter?) {
        val target = writer ?: return
        recordAndResetQuality(target, QualityCounterId.RUNTIME_GRAPH_BUFFER_CAPACITY_LOSS, bufferCapacityLoss)
        recordAndResetQuality(target, QualityCounterId.RUNTIME_GRAPH_REGISTRY_CAPACITY_LOSS, producerRegistryLoss)
        recordAndResetQuality(target, QualityCounterId.RUNTIME_GRAPH_CAPACITY_LOSS, edgeCapacityLoss)
        recordAndResetQuality(target, QualityCounterId.RUNTIME_GRAPH_STALE_EPOCH_LOSS, staleEpochLoss)
        recordAndResetQuality(target, QualityCounterId.RUNTIME_GRAPH_SHUTDOWN_LOSS, shutdownLoss)
        recordAndResetQuality(target, QualityCounterId.RUNTIME_GRAPH_WRITER_REJECTION_LOSS, writerRejectionLoss)
        recordAndResetQuality(target, QualityCounterId.RUNTIME_STACK_MISMATCH, stackMismatch)
        recordAndResetQuality(target, QualityCounterId.RUNTIME_STACK_CAPACITY_LOSS, stackCapacityLoss)
        recordAndResetQuality(target, QualityCounterId.RUNTIME_GRAPH_SHADOW_CAPACITY_LOSS, shadowCapacityLoss)
        recordAndResetQuality(target, QualityCounterId.RUNTIME_GRAPH_CIRCUIT_BREAKER_TRIP, circuitBreakerTrips)
        recordAndResetQuality(target, QualityCounterId.RUNTIME_GRAPH_CIRCUIT_BREAKER_DROP, circuitBreakerDrops)
        recordDelta(target, QualityCounterId.RUNTIME_GRAPH_INPUT_TOTAL, attempted, reportedAttempted)
        recordDelta(target, QualityCounterId.RUNTIME_GRAPH_EMITTED_TOTAL, emitted, reportedEmitted)
    }

    private fun recordDelta(writer: AsyncLogWriter, counterId: Int, total: AtomicLong, reported: AtomicLong) {
        val current = total.get()
        val previous = reported.getAndSet(current)
        if (current > previous) writer.recordQuality(counterId, current - previous)
    }

    private fun recordCircuitBreakerLoss(delta: Long = 1L) {
        if (circuitBreakerOpen.get() || delta <= 0L) return
        var loss: Long
        while (true) {
            val current = circuitBreakerLossBudget.get()
            loss = saturatingAdd(current, delta)
            if (circuitBreakerLossBudget.compareAndSet(current, loss)) break
        }
        if (loss >= CIRCUIT_BREAKER_LOSS_THRESHOLD && circuitBreakerOpen.compareAndSet(false, true)) {
            circuitBreakerTrips.incrementAndGet()
        }
    }

    private fun hasBufferedEvents(): Boolean {
        for (index in 0 until buffers.length()) {
            if (buffers.get(index)?.sequencer?.isEmpty() == false) return true
        }
        return false
    }

    private fun bufferedEventCount(): Long {
        var count = 0L
        for (index in 0 until buffers.length()) {
            count = saturatingAdd(count, buffers.get(index)?.sequencer?.pendingCount() ?: continue)
        }
        return count
    }

    private fun reclaimDeadBuffers() {
        for (index in 0 until buffers.length()) {
            val buffer = buffers.get(index) ?: continue
            if (buffer.owner.get() == null && buffer.sequencer.isEmpty()) {
                buffers.compareAndSet(index, buffer, null)
            }
        }
    }

    private fun clearRegistry() {
        for (index in 0 until buffers.length()) {
            buffers.getAndSet(index, null)?.clear()
        }
    }

    private fun clearQuality() {
        accepted.set(0L)
        attempted.set(0L)
        emitted.set(0L)
        aggregatedEdgeKeys.set(0L)
        acceptedEventLoss.set(0L)
        bufferCapacityLoss.set(0L)
        producerRegistryLoss.set(0L)
        edgeCapacityLoss.set(0L)
        staleEpochLoss.set(0L)
        shutdownLoss.set(0L)
        writerRejectionLoss.set(0L)
        stackMismatch.set(0L)
        stackCapacityLoss.set(0L)
        shadowCapacityLoss.set(0L)
        circuitBreakerOpen.set(false)
        circuitBreakerLossBudget.set(0L)
        circuitBreakerTrips.set(0L)
        circuitBreakerDrops.set(0L)
        reportedAttempted.set(0L)
        reportedEmitted.set(0L)
        preAdmissionLossTotal.set(0L)
        circuitBreakerDropTotal.set(0L)
    }

    private class ProducerState(
        val epoch: Long,
        val stack: RuntimeCallStack,
        val buffer: RuntimeGraphEdgeBuffer?,
    )

    private companion object {
        const val CONSUMER_NAME = "JankHunterGraph"
        const val DISABLED_TOKEN = 0L
        const val NO_FLUSH_REQUEST = -1L
        const val MAX_PRODUCERS = 128
        const val CIRCUIT_BREAKER_LOSS_THRESHOLD = 256L
        const val MAX_DRAIN_PER_BUFFER = RUNTIME_GRAPH_BUFFER_CAPACITY
        const val MAX_FLUSH_RECORDS = RUNTIME_GRAPH_MAX_FLUSH_RECORDS
        const val FLUSH_INTERVAL_MS = 5_000L
        const val CONSUMER_PARK_NS = 50_000_000L
        const val FLUSH_WAIT_POLL_NS = 1_000_000L
        const val SHUTDOWN_TIMEOUT_MS = 2_000L
        const val SHUTDOWN_JOIN_POLL_MS = 50L
    }
}
