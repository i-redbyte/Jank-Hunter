package io.jankhunter.runtime

import android.os.Process
import io.jankhunter.runtime.internal.saturatingAdd
import io.jankhunter.runtime.internal.concurrent.SpscSlotSequencer
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.QualityCounterId
import io.jankhunter.runtime.internal.io.StableCounterBatch
import java.lang.ref.WeakReference
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReferenceArray
import java.util.concurrent.locks.LockSupport

internal class RuntimeHookEventTransport(
    private val maxCounterKeys: () -> Int,
    private val maxLogSpamKeys: () -> Int,
) {
    private val threadState = ThreadLocal<ProducerState>()
    private val buffers = AtomicReferenceArray<EventBuffer>(MAX_PRODUCERS)
    private val epoch = AtomicLong(1L)
    private val running = AtomicBoolean(false)
    private val producerWakePending = AtomicBoolean()
    private val bufferLoss = AtomicLong()
    private val registryLoss = AtomicLong()
    private val counterCardinalityLoss = AtomicLong()
    private val logCardinalityLoss = AtomicLong()
    private val writerLoss = AtomicLong()
    private val preAdmissionLossTotal = AtomicLong()
    private val acceptedLossTotal = AtomicLong()
    private val accepted = AtomicLong()
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
        running.set(true)
        consumer = Thread(::runConsumerFailOpen, CONSUMER_NAME).apply {
            isDaemon = true
            start()
        }
    }

    fun recordMethod(methodId: Long, methodName: String?): Boolean {
        val state = producerState() ?: return false
        return publish(state) { buffer, slot ->
            buffer.types[slot] = TYPE_METHOD
            buffer.ids[slot] = methodId
            buffer.names[slot] = methodName
        }
    }

    fun recordLogSpam(
        screen: String?, owner: String?, flow: String?, step: String?, source: String?, level: Int,
    ): Boolean {
        val state = producerState() ?: return false
        return publish(state) { buffer, slot ->
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
        running.set(false)
        LockSupport.unpark(active)
        val deadline = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(timeoutMs.coerceAtLeast(1L))
        var interrupted = false
        while (active.isAlive) {
            val remaining = deadline - System.nanoTime()
            if (remaining <= 0L) break
            try {
                active.join(TimeUnit.NANOSECONDS.toMillis(remaining).coerceIn(1L, JOIN_POLL_MS))
            } catch (_: InterruptedException) {
                interrupted = true
                break
            }
        }
        if (interrupted) Thread.currentThread().interrupt()
        return !active.isAlive
    }

    fun clear() {
        running.set(false)
        consumer?.let(LockSupport::unpark)
        clearRegistry()
        threadState.remove()
        writer = null
        consumer = null
        clearCounters()
    }

    internal fun acceptedForTest(): Long = accepted.get()
    internal fun emittedForTest(): Long = emitted.get()
    internal fun acceptedLossForTest(): Long = acceptedLossTotal.get()
    internal fun attemptedForTest(): Long = accepted.get() + preAdmissionLossTotal.get()
    internal fun consumerForTest(): Thread? = consumer
    internal fun registeredProducerCountForTest(): Int {
        var count = 0
        for (index in 0 until buffers.length()) if (buffers.get(index) != null) count++
        return count
    }

    private inline fun publish(state: ProducerState, write: (EventBuffer, Int) -> Unit): Boolean {
        val buffer = state.buffer ?: run {
            recordPreAdmissionLoss(registryLoss)
            return false
        }
        val position = buffer.sequencer.tryClaimProducer()
        if (position == SpscSlotSequencer.NO_POSITION) {
            recordPreAdmissionLoss(bufferLoss)
            return false
        }
        val slot = buffer.sequencer.slotIndex(position)
        buffer.epochs[slot] = state.epoch
        write(buffer, slot)
        buffer.sequencer.publish(position)
        accepted.incrementAndGet()
        wakeConsumer()
        return true
    }

    private fun producerState(): ProducerState? {
        if (!running.get()) return null
        val currentEpoch = epoch.get()
        threadState.get()?.takeIf { it.epoch == currentEpoch }?.let { return it }
        val buffer = registerFirstAvailable(buffers) { EventBuffer(Thread.currentThread()) }
        return ProducerState(currentEpoch, buffer).also(threadState::set)
    }

    private fun runConsumerFailOpen() {
        val methods = HashMap<Long, MethodCounter>()
        val logs = HashMap<LogSpamKey, Long>()
        try {
            try {
                Process.setThreadPriority(Process.THREAD_PRIORITY_BACKGROUND)
            } catch (_: Throwable) {
            }
            var lastFlushAtNs = System.nanoTime()
            while (running.get() || hasBufferedEvents()) {
                producerWakePending.set(false)
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
            recordAcceptedLoss(writerLoss, saturatingAdd(bufferedEventCount(), aggregateCount(methods, logs)))
            flushQuality()
        } finally {
            consumer = null
        }
    }

    private fun drain(methods: MutableMap<Long, MethodCounter>, logs: MutableMap<LogSpamKey, Long>): Int {
        var drained = 0
        for (index in 0 until buffers.length()) {
            val buffer = buffers.get(index) ?: continue
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
                drained++
                fromBuffer++
            }
        }
        return drained
    }

    private fun wakeConsumer() {
        if (producerWakePending.compareAndSet(false, true)) consumer?.let(LockSupport::unpark)
    }

    private fun drainAll(methods: MutableMap<Long, MethodCounter>, logs: MutableMap<LogSpamKey, Long>) {
        while (drain(methods, logs) > 0) Unit
    }

    private fun aggregateMethod(buffer: EventBuffer, slot: Int, target: MutableMap<Long, MethodCounter>) {
        val id = buffer.ids[slot]
        target[id]?.let {
            it.count = saturatingAdd(it.count, 1L)
            return
        }
        if (target.size >= maxCounterKeys().coerceAtLeast(0)) {
            recordAcceptedLoss(counterCardinalityLoss)
        } else {
            target[id] = MethodCounter(buffer.names[slot], 1L)
        }
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
        if (target.size >= maxLogSpamKeys().coerceAtLeast(0)) {
            recordAcceptedLoss(logCardinalityLoss)
        } else {
            target[key] = 1L
        }
    }

    private fun emit(methods: MutableMap<Long, MethodCounter>, logs: MutableMap<LogSpamKey, Long>) {
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
        recordAndResetQuality(activeWriter, QualityCounterId.RUNTIME_EVENT_REGISTRY_CAPACITY_LOSS, registryLoss)
        recordAndResetQuality(activeWriter, QualityCounterId.METHOD_COUNTER_CARDINALITY_LOSS, counterCardinalityLoss)
        recordAndResetQuality(activeWriter, QualityCounterId.LOG_SPAM_CARDINALITY_LOSS, logCardinalityLoss)
        recordAndResetQuality(activeWriter, QualityCounterId.RUNTIME_EVENT_WRITER_REJECTION_LOSS, writerLoss)
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
        for (index in 0 until buffers.length()) if (buffers.get(index)?.sequencer?.isEmpty() == false) return true
        return false
    }

    private fun bufferedEventCount(): Long {
        var result = 0L
        for (index in 0 until buffers.length()) {
            result = saturatingAdd(result, buffers.get(index)?.sequencer?.pendingCount() ?: continue)
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
        for (index in 0 until buffers.length()) {
            val buffer = buffers.get(index) ?: continue
            if (buffer.owner.get() == null && buffer.sequencer.isEmpty()) buffers.compareAndSet(index, buffer, null)
        }
    }

    private fun clearRegistry() {
        for (index in 0 until buffers.length()) buffers.getAndSet(index, null)?.clearAll()
    }

    private fun clearCounters() {
        bufferLoss.set(0L)
        registryLoss.set(0L)
        counterCardinalityLoss.set(0L)
        logCardinalityLoss.set(0L)
        writerLoss.set(0L)
        preAdmissionLossTotal.set(0L)
        acceptedLossTotal.set(0L)
        accepted.set(0L)
        emitted.set(0L)
        flushRequest.set(0L)
        flushCompleted.set(0L)
        producerWakePending.set(false)
    }

    private class ProducerState(val epoch: Long, val buffer: EventBuffer?)

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
        }
    }

    private class MethodCounter(val name: String?, var count: Long)
    private data class LogSpamKey(
        val screen: String?, val owner: String?, val flow: String?, val step: String?,
        val source: String?, val level: Int,
    )

    private companion object {
        const val CONSUMER_NAME = "JankHunterEvents"
        const val NO_FLUSH_REQUEST = -1L
        const val MAX_PRODUCERS = 128
        const val BUFFER_CAPACITY = 256
        const val MAX_BATCH_SIZE = 128
        const val TYPE_METHOD: Byte = 1
        const val TYPE_LOG_SPAM: Byte = 2
        const val CONSUMER_PARK_NS = 50_000_000L
        const val WAIT_POLL_NS = 1_000_000L
        const val JOIN_POLL_MS = 50L
        const val FLUSH_INTERVAL_NS = 5_000_000_000L

    }
}
