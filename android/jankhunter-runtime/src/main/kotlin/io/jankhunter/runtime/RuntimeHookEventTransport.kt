package io.jankhunter.runtime

import android.os.Process
import io.jankhunter.runtime.internal.saturatingAdd
import io.jankhunter.runtime.internal.concurrent.SpscSlotSequencer
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.QualityCounterId
import io.jankhunter.runtime.internal.io.StableCounterBatch
import java.lang.ref.WeakReference
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.locks.LockSupport

internal class RuntimeHookEventTransport(
    private val maxCounterKeys: RuntimeIntSource,
    private val maxLogSpamKeys: RuntimeIntSource,
    private val exactAdmission: RuntimeBooleanSource = RuntimeBooleanSource { true },
    private val consumerDelayNanos: Long = 0L,
    private val publisherAdmissionObserver: (() -> Unit)? = null,
    private val consumerLoopObserver: (() -> Unit)? = null,
) {
    private val threadBuffer = ThreadLocal<WeakReference<EventBuffer>>()
    private val buffers = ConcurrentLinkedQueue<EventBuffer>()
    private val epoch = AtomicLong(1L)
    private val running = AtomicBoolean(false)
    private val consumerFailed = AtomicBoolean(false)
    private val publisherState = AtomicLong()
    private val producerWakePending = AtomicBoolean()
    private val bufferLoss = AtomicLong()
    private val writerLoss = AtomicLong()
    private val backpressureCount = AtomicLong()
    private val backpressureNanos = AtomicLong()
    private val preAdmissionLossTotal = AtomicLong()
    private val acceptedLossTotal = AtomicLong()
    private val retiredProducerAccepted = AtomicLong()
    private val emitted = AtomicLong()
    private val flushRequest = AtomicLong()
    private val flushCompleted = AtomicLong()

    @Volatile private var writer: AsyncLogWriter? = null
    @Volatile private var consumer: Thread? = null

    fun start(writer: AsyncLogWriter) {
        check(consumer?.isAlive != true) { "Runtime hook event consumer is already running" }
        advanceRuntimeEpoch(epoch)
        clearRegistry()
        clearCounters()
        this.writer = writer
        consumerFailed.set(false)
        running.set(true)
        publisherState.set(PUBLISHER_GATE_OPEN)
        consumer = Thread(::runConsumerFailOpen, CONSUMER_NAME).apply {
            isDaemon = true
            start()
        }
    }

    fun recordMethod(methodId: Long, methodName: String): Boolean {
        return publish { buffer, slot ->
            buffer.types[slot] = TYPE_METHOD
            buffer.ids[slot] = methodId
            buffer.names[slot] = methodName
        }
    }

    fun recordLogSpam(
        screen: String?, owner: String?, source: String?, level: Int, operationId: Long = 0L,
    ): Boolean {
        return publish { buffer, slot ->
            buffer.types[slot] = TYPE_LOG_SPAM
            buffer.screens[slot] = screen
            buffer.owners[slot] = owner
            buffer.names[slot] = source
            buffer.levels[slot] = level
            buffer.ids[slot] = operationId.coerceAtLeast(0L)
        }
    }

    fun flushBlocking(timeoutMs: Long): Boolean {
        if (!running.get()) return true
        val request = flushRequest.incrementAndGet()
        consumer?.let(LockSupport::unpark)
        val deadline = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(timeoutMs.coerceAtLeast(1L))
        while (flushCompleted.get() < request) {
            val remaining = deadline - System.nanoTime()
            if (remaining <= 0L) return false
            LockSupport.parkNanos(minOf(remaining, WAIT_POLL_NS))
            if (Thread.interrupted()) {
                Thread.currentThread().interrupt()
                return false
            }
        }
        return true
    }

    fun stopAndFlush(timeoutMs: Long): Boolean {
        val active = consumer ?: return true
        closePublisherGate()
        running.set(false)
        LockSupport.unpark(active)
        val exact = exactAdmission.getAsBoolean()
        val deadline = if (exact) Long.MAX_VALUE else {
            System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(timeoutMs.coerceAtLeast(1L))
        }
        var interrupted = false
        while (active.isAlive) {
            val waitMs = if (exact) {
                JOIN_POLL_MS
            } else {
                val remaining = deadline - System.nanoTime()
                if (remaining <= 0L) break
                TimeUnit.NANOSECONDS.toMillis(remaining).coerceIn(1L, JOIN_POLL_MS)
            }
            try {
                active.join(waitMs)
            } catch (_: InterruptedException) {
                interrupted = true
                if (!exact) break
            }
        }
        if (interrupted) Thread.currentThread().interrupt()
        return !active.isAlive
    }

    fun clear() {
        closePublisherGate()
        running.set(false)
        consumer?.let(LockSupport::unpark)
        clearRegistry()
        threadBuffer.remove()
        writer = null
        consumer = null
        clearCounters()
    }

    internal fun acceptedForTest(): Long = producerAcceptedTotal()
    internal fun emittedForTest(): Long = emitted.get()
    internal fun acceptedLossForTest(): Long = acceptedLossTotal.get()
    internal fun backpressureCountForTest(): Long = backpressureCount.get()
    internal fun attemptedForTest(): Long = saturatingAdd(producerAcceptedTotal(), preAdmissionLossTotal.get())
    internal fun consumerForTest(): Thread? = consumer
    internal fun acceptingPublishersForTest(): Boolean = publisherState.get() and PUBLISHER_GATE_OPEN != 0L
    internal fun registeredProducerCountForTest(): Int {
        var count = 0
        buffers.forEach { count++ }
        return count
    }

    private inline fun publish(write: (EventBuffer, Int) -> Unit): Boolean {
        if (!acquirePublisher()) return false
        try {
            publisherAdmissionObserver?.invoke()
            if (consumerFailed.get()) {
                recordPreAdmissionLoss(bufferLoss)
                return false
            }
            val buffer = producerBuffer()
            val exact = exactAdmission.getAsBoolean()
            var position = buffer.sequencer.tryClaimProducer()
            if (position == SpscSlotSequencer.NO_POSITION && !exact) {
                recordPreAdmissionLoss(bufferLoss)
                return false
            }
            var blockedAtNs = 0L
            while (position == SpscSlotSequencer.NO_POSITION) {
                if (consumerFailed.get() || (!running.get() && !exact)) {
                    buffer.producerWaiting = false
                    recordPreAdmissionLoss(bufferLoss)
                    return false
                }
                if (blockedAtNs == 0L) {
                    blockedAtNs = System.nanoTime()
                    backpressureCount.incrementAndGet()
                }
                buffer.producerWaiting = true
                wakeConsumer()
                LockSupport.parkNanos(BACKPRESSURE_PARK_NS)
                position = buffer.sequencer.tryClaimProducer()
            }
            buffer.producerWaiting = false
            if (blockedAtNs != 0L) {
                backpressureNanos.addAndGet((System.nanoTime() - blockedAtNs).coerceAtLeast(1L))
            }
            val slot = buffer.sequencer.slotIndex(position)
            buffer.epochs[slot] = buffer.epoch
            write(buffer, slot)
            buffer.sequencer.publish(position)
            buffer.recordAccepted()
            wakeConsumer()
            return true
        } finally {
            releasePublisher()
        }
    }

    private fun acquirePublisher(): Boolean {
        while (true) {
            val state = publisherState.get()
            if (state and PUBLISHER_GATE_OPEN == 0L) return false
            if (publisherState.compareAndSet(state, state + 1L)) return true
        }
    }

    private fun releasePublisher() {
        val state = publisherState.decrementAndGet()
        if (state and PUBLISHER_COUNT_MASK == 0L) consumer?.let(LockSupport::unpark)
    }

    private fun closePublisherGate() {
        while (true) {
            val state = publisherState.get()
            if (state and PUBLISHER_GATE_OPEN == 0L) return
            if (publisherState.compareAndSet(state, state and PUBLISHER_COUNT_MASK)) return
        }
    }

    private fun activePublisherCount(): Long = publisherState.get() and PUBLISHER_COUNT_MASK

    private fun producerBuffer(): EventBuffer {
        val currentEpoch = epoch.get()
        threadBuffer.get()?.get()?.takeIf { it.epoch == currentEpoch }?.let { return it }
        val buffer = EventBuffer(Thread.currentThread(), currentEpoch)
        buffers.add(buffer)
        threadBuffer.set(WeakReference(buffer))
        return buffer
    }

    private fun runConsumerFailOpen() {
        val methods = MethodCounterAccumulator(maxCounterKeys.getAsInt().coerceAtLeast(1))
        val logs = LogSpamAccumulator(maxLogSpamKeys.getAsInt().coerceAtLeast(1))
        try {
            try {
                Process.setThreadPriority(EVENT_CONSUMER_PRIORITY)
            } catch (_: Throwable) {
            }
            var lastFlushAtNs = System.nanoTime()
            while (running.get() || activePublisherCount() > 0L || hasBufferedEvents()) {
                consumerLoopObserver?.invoke()
                producerWakePending.set(false)
                delayConsumerForTest()
                val drained = drain(methods, logs)
                val request = flushRequest.get()
                val now = System.nanoTime()
                var completedRequest = NO_FLUSH_REQUEST
                if (request > flushCompleted.get() || now - lastFlushAtNs >= FLUSH_INTERVAL_NS || !running.get()) {
                    if (request > flushCompleted.get() || !running.get()) {
                        drainAll(methods, logs)
                    }
                    emit(methods, logs)
                    flushQuality()
                    completedRequest = request
                    lastFlushAtNs = now
                }
                reclaimDeadBuffers()
                if (completedRequest != NO_FLUSH_REQUEST) flushCompleted.set(completedRequest)
                if (drained == 0 && running.get()) LockSupport.parkNanos(CONSUMER_PARK_NS)
            }
            emit(methods, logs)
            flushQuality()
            reclaimDeadBuffers()
            flushCompleted.set(flushRequest.get())
        } catch (_: Throwable) {
            closePublisherGate()
            consumerFailed.set(true)
            running.set(false)
            buffers.forEach { buffer -> buffer.owner.get()?.let(LockSupport::unpark) }
            while (activePublisherCount() > 0L) LockSupport.parkNanos(WAIT_POLL_NS)
            val stranded = saturatingAdd(bufferedEventCount(), aggregateCount(methods, logs))
            recordAcceptedLoss(writerLoss, stranded.coerceAtLeast(1L))
            flushQuality()
        } finally {
            buffers.forEach { buffer -> buffer.owner.get()?.let(LockSupport::unpark) }
            consumer = null
        }
    }

    private fun drain(methods: MethodCounterAccumulator, logs: LogSpamAccumulator): Int {
        var drained = 0
        for (buffer in buffers) {
            var fromBuffer = 0
            while (fromBuffer < BUFFER_CAPACITY) {
                val position = buffer.sequencer.tryClaimConsumer()
                if (position == SpscSlotSequencer.NO_POSITION) break
                val slot = buffer.sequencer.slotIndex(position)
                if (buffer.epochs[slot] == epoch.get()) {
                    when (buffer.types[slot]) {
                        TYPE_METHOD -> aggregateMethod(buffer, slot, methods)
                        TYPE_LOG_SPAM -> aggregateLog(buffer, slot, logs)
                    }
                } else {
                    recordAcceptedLoss(writerLoss)
                }
                buffer.clear(slot)
                buffer.sequencer.release(position)
                if (buffer.producerWaiting) buffer.owner.get()?.let(LockSupport::unpark)
                drained++
                fromBuffer++
            }
        }
        return drained
    }

    private fun wakeConsumer() {
        if (producerWakePending.compareAndSet(false, true)) consumer?.let(LockSupport::unpark)
    }

    private fun delayConsumerForTest() {
        if (consumerDelayNanos <= 0L) return
        val deadline = System.nanoTime() + consumerDelayNanos
        var remaining = consumerDelayNanos
        while (remaining > 0L) {
            LockSupport.parkNanos(remaining)
            remaining = deadline - System.nanoTime()
        }
    }

    private fun drainAll(methods: MethodCounterAccumulator, logs: LogSpamAccumulator) {
        while (drain(methods, logs) > 0) Unit
    }

    private fun aggregateMethod(buffer: EventBuffer, slot: Int, target: MethodCounterAccumulator) {
        val id = buffer.ids[slot]
        val name = checkNotNull(buffer.names[slot])
        if (target.add(id, name)) return
        emitMethods(target)
        if (!target.add(id, name)) recordAcceptedLoss(writerLoss)
    }

    private fun aggregateLog(buffer: EventBuffer, slot: Int, target: LogSpamAccumulator) {
        if (target.add(
                buffer.screens[slot], buffer.owners[slot], buffer.names[slot],
                buffer.levels[slot], buffer.ids[slot],
            )
        ) {
            return
        }
        emitLogs(target)
        if (!target.add(
                buffer.screens[slot], buffer.owners[slot], buffer.names[slot],
                buffer.levels[slot], buffer.ids[slot],
            )
        ) {
            recordAcceptedLoss(writerLoss)
        }
    }

    private fun emit(methods: MethodCounterAccumulator, logs: LogSpamAccumulator) {
        emitMethods(methods)
        emitLogs(logs)
    }

    private fun emitMethods(methods: MethodCounterAccumulator) {
        var batch = StableCounterBatch(MAX_BATCH_SIZE)
        methods.drain { id, name, count ->
            if (batch.size == MAX_BATCH_SIZE) {
                emitMethodBatch(batch)
                batch = StableCounterBatch(MAX_BATCH_SIZE)
            }
            batch.add(id, name, count)
        }
        if (batch.size > 0) emitMethodBatch(batch)
    }

    private fun emitMethodBatch(batch: StableCounterBatch) {
        val logicalCount = batch.logicalEventCount()
        val admitted = try { writer?.stableCounters(batch) == true } catch (_: Throwable) { false }
        if (admitted) emitted.addAndGet(logicalCount) else recordAcceptedLoss(writerLoss, logicalCount)
    }

    private fun emitLogs(logs: LogSpamAccumulator) {
        val activeWriter = writer
        logs.drain { screen, owner, source, level, operationId, count ->
            val admitted = try {
                activeWriter?.logSpam(
                    screen,
                    owner,
                    operationId,
                    source,
                    level,
                    count,
                ) == true
            } catch (_: Throwable) {
                false
            }
            if (admitted) emitted.addAndGet(count) else recordAcceptedLoss(writerLoss, count)
        }
    }

    private fun flushQuality() {
        val activeWriter = writer ?: return
        recordAndResetQuality(activeWriter, QualityCounterId.RUNTIME_EVENT_BUFFER_CAPACITY_LOSS, bufferLoss)
        recordAndResetQuality(activeWriter, QualityCounterId.RUNTIME_EVENT_WRITER_REJECTION_LOSS, writerLoss)
        recordAndResetQuality(activeWriter, QualityCounterId.RUNTIME_EVENT_BACKPRESSURE_COUNT, backpressureCount)
        recordAndResetQuality(activeWriter, QualityCounterId.RUNTIME_EVENT_BACKPRESSURE_NANOS, backpressureNanos)
    }
    private fun recordPreAdmissionLoss(counter: AtomicLong, delta: Long = 1L) {
        counter.addAndGet(delta)
        preAdmissionLossTotal.addAndGet(delta)
    }

    private fun recordAcceptedLoss(counter: AtomicLong, delta: Long = 1L) {
        counter.addAndGet(delta)
        acceptedLossTotal.addAndGet(delta)
    }

    private fun hasBufferedEvents(): Boolean {
        for (buffer in buffers) if (!buffer.sequencer.isEmpty()) return true
        return false
    }

    private fun bufferedEventCount(): Long {
        var result = 0L
        for (buffer in buffers) {
            result = saturatingAdd(result, buffer.sequencer.pendingCount())
        }
        return result
    }

    private fun aggregateCount(methods: MethodCounterAccumulator, logs: LogSpamAccumulator): Long {
        return saturatingAdd(methods.logicalEventCount(), logs.logicalEventCount())
    }

    private fun reclaimDeadBuffers() {
        for (buffer in buffers) {
            if (buffer.owner.get() == null && buffer.sequencer.isEmpty() && buffers.remove(buffer)) {
                addSaturating(retiredProducerAccepted, buffer.acceptedCount())
            }
        }
    }

    private fun producerAcceptedTotal(): Long {
        var total = retiredProducerAccepted.get()
        buffers.forEach { buffer -> total = saturatingAdd(total, buffer.acceptedCount()) }
        return total
    }

    private fun clearRegistry() {
        while (true) buffers.poll()?.clearAll() ?: return
    }

    private fun clearCounters() {
        bufferLoss.set(0L)
        writerLoss.set(0L)
        backpressureCount.set(0L)
        backpressureNanos.set(0L)
        preAdmissionLossTotal.set(0L)
        acceptedLossTotal.set(0L)
        retiredProducerAccepted.set(0L)
        emitted.set(0L)
        flushRequest.set(0L)
        flushCompleted.set(0L)
        producerWakePending.set(false)
    }

    private class EventBuffer(thread: Thread, val epoch: Long) {
        val owner = WeakReference(thread)
        val sequencer = SpscSlotSequencer(BUFFER_CAPACITY)
        val types = ByteArray(BUFFER_CAPACITY)
        val epochs = LongArray(BUFFER_CAPACITY)
        val ids = LongArray(BUFFER_CAPACITY)
        val levels = IntArray(BUFFER_CAPACITY)
        val names = arrayOfNulls<String>(BUFFER_CAPACITY)
        val screens = arrayOfNulls<String>(BUFFER_CAPACITY)
        val owners = arrayOfNulls<String>(BUFFER_CAPACITY)

        @Volatile var producerWaiting = false
        @Volatile private var producerAccepted = 0L

        fun recordAccepted() {
            producerAccepted = saturatingAdd(producerAccepted, 1L)
        }

        fun acceptedCount(): Long = producerAccepted

        fun clear(index: Int) {
            names[index] = null
            screens[index] = null
            owners[index] = null
        }

        fun clearAll() {
            names.fill(null)
            screens.fill(null)
            owners.fill(null)
            producerAccepted = 0L
        }
    }

    private companion object {
        const val CONSUMER_NAME = "JankHunterEvents"
        const val NO_FLUSH_REQUEST = -1L
        const val BUFFER_CAPACITY = 256
        const val MAX_BATCH_SIZE = 128
        const val TYPE_METHOD: Byte = 1
        const val TYPE_LOG_SPAM: Byte = 2
        const val CONSUMER_PARK_NS = 50_000_000L
        const val WAIT_POLL_NS = 1_000_000L
        const val JOIN_POLL_MS = 50L
        const val FLUSH_INTERVAL_NS = 5_000_000_000L
        const val BACKPRESSURE_PARK_NS = 100_000L
        const val EVENT_CONSUMER_PRIORITY = Process.THREAD_PRIORITY_DEFAULT + 1
        const val PUBLISHER_GATE_OPEN = Long.MIN_VALUE
        const val PUBLISHER_COUNT_MASK = Long.MAX_VALUE
    }
}
