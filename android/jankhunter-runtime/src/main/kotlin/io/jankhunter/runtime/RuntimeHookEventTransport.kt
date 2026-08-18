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
    private val maxCounterKeys: () -> Int,
    private val maxLogSpamKeys: () -> Int,
    private val exactAdmission: () -> Boolean = { true },
    private val consumerDelayNanos: Long = 0L,
    private val publisherAdmissionObserver: (() -> Unit)? = null,
    private val consumerLoopObserver: (() -> Unit)? = null,
) {
    private val threadState = ThreadLocal<ProducerState>()
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

    fun recordMethod(methodId: Long, methodName: String?): Boolean {
        return publish { buffer, slot ->
            buffer.types[slot] = TYPE_METHOD
            buffer.ids[slot] = methodId
            buffer.names[slot] = methodName
        }
    }

    fun recordLogSpam(
        screen: String?, owner: String?, flow: String?, step: String?, source: String?, level: Int,
    ): Boolean {
        return publish { buffer, slot ->
            buffer.types[slot] = TYPE_LOG_SPAM
            buffer.screens[slot] = screen
            buffer.owners[slot] = owner
            buffer.flows[slot] = flow
            buffer.steps[slot] = step
            buffer.names[slot] = source
            buffer.levels[slot] = level
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
        val exact = exactAdmission()
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
        threadState.remove()
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
            val state = producerState()
            val buffer = state.buffer
            val exact = exactAdmission()
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
            buffer.epochs[slot] = state.epoch
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

    private fun producerState(): ProducerState {
        val currentEpoch = epoch.get()
        threadState.get()?.takeIf { it.epoch == currentEpoch }?.let { return it }
        val buffer = EventBuffer(Thread.currentThread())
        buffers.add(buffer)
        return ProducerState(currentEpoch, buffer).also(threadState::set)
    }

    private fun runConsumerFailOpen() {
        val methods = HashMap<Long, MethodCounter>()
        val logs = HashMap<LogSpamKey, Long>()
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

    private fun drain(methods: MutableMap<Long, MethodCounter>, logs: MutableMap<LogSpamKey, Long>): Int {
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

    private fun drainAll(methods: MutableMap<Long, MethodCounter>, logs: MutableMap<LogSpamKey, Long>) {
        while (drain(methods, logs) > 0) Unit
    }

    private fun aggregateMethod(buffer: EventBuffer, slot: Int, target: MutableMap<Long, MethodCounter>) {
        val id = buffer.ids[slot]
        target[id]?.let {
            if (it.name == null && buffer.names[slot] != null) it.name = buffer.names[slot]
            it.count = saturatingAdd(it.count, 1L)
            return
        }
        if (target.size >= maxCounterKeys().coerceAtLeast(1)) emitMethods(target)
        target[id] = MethodCounter(buffer.names[slot], 1L)
    }

    private fun aggregateLog(buffer: EventBuffer, slot: Int, target: MutableMap<LogSpamKey, Long>) {
        val key = LogSpamKey(
            buffer.screens[slot], buffer.owners[slot], buffer.flows[slot], buffer.steps[slot],
            buffer.names[slot], buffer.levels[slot],
        )
        target[key]?.let {
            target[key] = saturatingAdd(it, 1L)
            return
        }
        if (target.size >= maxLogSpamKeys().coerceAtLeast(1)) emitLogs(target)
        target[key] = 1L
    }

    private fun emit(methods: MutableMap<Long, MethodCounter>, logs: MutableMap<LogSpamKey, Long>) {
        emitMethods(methods)
        emitLogs(logs)
    }

    private fun emitMethods(methods: MutableMap<Long, MethodCounter>) {
        val activeWriter = writer
        val iterator = methods.iterator()
        while (iterator.hasNext()) {
            val batch = StableCounterBatch(MAX_BATCH_SIZE)
            while (iterator.hasNext() && batch.size < MAX_BATCH_SIZE) {
                val entry = iterator.next()
                batch.add(entry.key, entry.value.name, entry.value.count)
                iterator.remove()
            }
            val logicalCount = batch.logicalEventCount()
            val admitted = try { activeWriter?.stableCounters(batch) == true } catch (_: Throwable) { false }
            if (admitted) emitted.addAndGet(logicalCount) else recordAcceptedLoss(writerLoss, logicalCount)
        }
    }

    private fun emitLogs(logs: MutableMap<LogSpamKey, Long>) {
        val activeWriter = writer
        logs.forEach { (key, count) ->
            val admitted = try {
                activeWriter?.logSpam(key.screen, key.owner, key.flow, key.step, key.source, key.level, count) == true
            } catch (_: Throwable) {
                false
            }
            if (admitted) emitted.addAndGet(count) else recordAcceptedLoss(writerLoss, count)
        }
        logs.clear()
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

    private fun aggregateCount(methods: Map<Long, MethodCounter>, logs: Map<LogSpamKey, Long>): Long {
        var result = 0L
        methods.values.forEach { result = saturatingAdd(result, it.count) }
        logs.values.forEach { result = saturatingAdd(result, it) }
        return result
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

    private class ProducerState(val epoch: Long, val buffer: EventBuffer)

    private class EventBuffer(thread: Thread) {
        val owner = WeakReference(thread)
        val sequencer = SpscSlotSequencer(BUFFER_CAPACITY)
        val types = ByteArray(BUFFER_CAPACITY)
        val epochs = LongArray(BUFFER_CAPACITY)
        val ids = LongArray(BUFFER_CAPACITY)
        val levels = IntArray(BUFFER_CAPACITY)
        val names = arrayOfNulls<String>(BUFFER_CAPACITY)
        val screens = arrayOfNulls<String>(BUFFER_CAPACITY)
        val owners = arrayOfNulls<String>(BUFFER_CAPACITY)
        val flows = arrayOfNulls<String>(BUFFER_CAPACITY)
        val steps = arrayOfNulls<String>(BUFFER_CAPACITY)

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
            flows[index] = null
            steps[index] = null
        }

        fun clearAll() {
            names.fill(null)
            screens.fill(null)
            owners.fill(null)
            flows.fill(null)
            steps.fill(null)
            producerAccepted = 0L
        }
    }

    private class MethodCounter(var name: String?, var count: Long)
    private data class LogSpamKey(
        val screen: String?, val owner: String?, val flow: String?, val step: String?,
        val source: String?, val level: Int,
    )

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
