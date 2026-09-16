package io.jankhunter.runtime

import android.os.Process
import io.jankhunter.runtime.internal.saturatingAdd
import io.jankhunter.runtime.internal.monotonicDeadlineAfterMillis
import io.jankhunter.runtime.internal.concurrent.CoalescedWakeSignal
import io.jankhunter.runtime.internal.concurrent.SpscSlotSequencer
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.QualityCounterId
import io.jankhunter.runtime.internal.io.StableCounterBatch
import io.jankhunter.runtime.internal.io.StableCounterBatchPool
import java.lang.ref.WeakReference
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.locks.LockSupport

internal class RuntimeHookEventSession(
    private val maxCounterKeys: RuntimeIntSource,
    private val maxLogSpamKeys: RuntimeIntSource,
    private val exactAdmission: RuntimeBooleanSource = RuntimeBooleanSource { true },
    private val admissionWaitNanos: RuntimeLongSource = RuntimeLongSource { DEFAULT_ADMISSION_WAIT_NS },
    private val consumerDelayNanos: Long = 0L,
    private val publisherAdmissionObserver: (() -> Unit)? = null,
    private val consumerLoopObserver: (() -> Unit)? = null,
    private val consumerThreadFactory: (Runnable, String) -> Thread = ::Thread,
    private val producerRegistrationObserver: (() -> Unit)? = null,
    private val onStopped: () -> Unit,
) {
    private val completion = RuntimeConsumerCompletion()
    private val startupCleanupPending = AtomicBoolean()
    private val stateCleared = AtomicBoolean()
    @Volatile var hasConsumerOwner = true
        private set

    val isAccepting: Boolean get() = acceptingPublishers

    fun whenDrained(action: () -> Unit) = completion.whenComplete(action)

    private val threadBuffer = ThreadLocal<WeakReference<EventBuffer>>()
    private val buffers = ConcurrentLinkedQueue<EventBuffer>()
    private val epoch = AtomicLong(1L)
    private val running = AtomicBoolean(false)
    private val clearRequested = AtomicBoolean(false)
    private val consumerFailed = AtomicBoolean(false)
    private val producerWake = CoalescedWakeSignal()
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
    private val stableCounterBatchPool = StableCounterBatchPool(BATCH_POOL_CAPACITY, MAX_BATCH_SIZE)

    @Volatile private var writer: AsyncLogWriter? = null
    @Volatile private var consumer: Thread? = null
    @Volatile private var acceptingPublishers = false

    fun start(writer: AsyncLogWriter, publish: () -> Unit) {
        check(consumer?.isAlive != true) { "Runtime hook event consumer is already running" }
        advanceRuntimeEpoch(epoch)
        clearRegistry()
        clearCounters()
        clearRequested.set(false)
        this.writer = writer
        consumerFailed.set(false)
        running.set(true)
        acceptingPublishers = true
        try {
            val startedConsumer = consumerThreadFactory(Runnable(::runConsumerFailOpen), CONSUMER_NAME).apply {
                isDaemon = true
            }
            consumer = startedConsumer
            publish()
            startedConsumer.start()
        } catch (error: Throwable) {
            closePublisherGate()
            running.set(false)
            consumerFailed.set(true)
            buffers.forEach { buffer -> buffer.owner.get()?.let(LockSupport::unpark) }
            if (consumer?.isAlive != true && hasConsumerOwner) {
                startupCleanupPending.set(true)
                completeFailedStartIfIdle()
            }
            RuntimeHookGuard.rethrowFatal(error)
            throw error
        }
    }

    private fun completeFailedStartIfIdle() {
        if (!startupCleanupPending.get() || hasActivePublishers() || !startupCleanupPending.compareAndSet(true, false)) return
        val stranded = bufferedEventCount()
        if (stranded > 0L) recordAcceptedLoss(writerLoss, stranded)
        flushQuality()
        retireAllBuffers()
        writer = null
        consumer = null
        if (clearRequested.get()) clearStoppedState()
        try {
            completion.complete()
        } finally {
            hasConsumerOwner = false
            onStopped()
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
        if (!running.get()) {
            return !consumerFailed.get() && consumer?.isAlive != true
        }
        val request = flushRequest.incrementAndGet()
        consumer?.let(LockSupport::unpark)
        val deadline = monotonicDeadlineAfterMillis(timeoutMs.coerceAtLeast(1L))
        while (flushCompleted.get() < request) {
            if (consumerFailed.get() || !running.get()) return false
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

    fun requestStop() {
        closePublisherGate()
        running.set(false)
        consumer?.let(LockSupport::unpark)
    }

    fun stopAndFlush(timeoutMs: Long): Boolean {
        val active = consumer ?: return true
        closePublisherGate()
        running.set(false)
        LockSupport.unpark(active)
        val deadline = monotonicDeadlineAfterMillis(timeoutMs.coerceAtLeast(1L))
        var interrupted = false
        while (active.isAlive) {
            val remaining = deadline - System.nanoTime()
            if (remaining <= 0L) break
            val waitMs = TimeUnit.NANOSECONDS.toMillis(remaining).coerceIn(1L, JOIN_POLL_MS)
            try {
                active.join(waitMs)
            } catch (_: InterruptedException) {
                interrupted = true
                break
            }
        }
        if (interrupted) Thread.currentThread().interrupt()
        return !active.isAlive
    }

    fun ownsWriter(writer: AsyncLogWriter): Boolean = this.writer === writer

    fun clear() {
        closePublisherGate()
        running.set(false)
        clearRequested.set(true)
        val active = consumer
        active?.let(LockSupport::unpark)
        threadBuffer.remove()
        if (hasConsumerOwner && consumer != null) return
        clearStoppedState()
    }

    private fun clearStoppedState() {
        if (!stateCleared.compareAndSet(false, true)) return
        clearRegistry()
        writer = null
        clearCounters()
        clearRequested.set(false)
        // Publish the reusable state only after every field from the previous consumer is reset.
        consumer = null
    }

    internal fun acceptedForTest(): Long = producerAcceptedTotal()
    internal fun emittedForTest(): Long = emitted.get()
    internal fun acceptedLossForTest(): Long = acceptedLossTotal.get()
    internal fun backpressureCountForTest(): Long = backpressureCount.get()
    internal fun attemptedForTest(): Long = saturatingAdd(producerAcceptedTotal(), preAdmissionLossTotal.get())
    internal fun consumerForTest(): Thread? = consumer
    internal fun acceptingPublishersForTest(): Boolean = acceptingPublishers
    internal fun registeredProducerCountForTest(): Int {
        var count = 0
        buffers.forEach { count++ }
        return count
    }

    private inline fun publish(write: (EventBuffer, Int) -> Unit): Boolean {
        if (!acceptingPublishers) return false
        val buffer = producerBuffer() ?: return false
        buffer.producerActive = true
        try {
            if (!acceptingPublishers) return false
            publisherAdmissionObserver?.invoke()
            if (consumerFailed.get()) {
                recordPreAdmissionLoss(bufferLoss)
                return false
            }
            val position = claimPosition(buffer)
            if (position == SpscSlotSequencer.NO_POSITION) {
                recordPreAdmissionLoss(bufferLoss)
                return false
            }
            val slot = buffer.sequencer.slotIndex(position)
            buffer.epochs[slot] = buffer.epoch
            write(buffer, slot)
            buffer.sequencer.publish(position)
            buffer.recordAccepted()
            wakeConsumer()
            return true
        } finally {
            buffer.producerActive = false
            if (!acceptingPublishers) {
                consumer?.let(LockSupport::unpark)
                completeFailedStartIfIdle()
            }
        }
    }

    private fun claimPosition(buffer: EventBuffer): Long {
        val immediate = buffer.sequencer.tryClaimProducer()
        if (immediate != SpscSlotSequencer.NO_POSITION) return immediate
        val waitNs = if (exactAdmission.getAsBoolean()) admissionWaitNanos.getAsLong().coerceAtLeast(0L) else 0L
        if (waitNs == 0L) return SpscSlotSequencer.NO_POSITION
        val startedAtNs = System.nanoTime()
        backpressureCount.incrementAndGet()
        try {
            while (acceptingPublishers && !consumerFailed.get() && exactAdmission.getAsBoolean()) {
                // isInterrupted preserves the application's interrupt status and avoids park spinning.
                if (Thread.currentThread().isInterrupted) break
                val remaining = waitNs - (System.nanoTime() - startedAtNs)
                if (remaining <= 0L) break
                val position = buffer.sequencer.tryClaimProducer()
                if (position != SpscSlotSequencer.NO_POSITION) return position
                buffer.producerWaiting = true
                wakeConsumer()
                LockSupport.parkNanos(minOf(BACKPRESSURE_PARK_NS, remaining))
            }
            return SpscSlotSequencer.NO_POSITION
        } finally {
            buffer.producerWaiting = false
            backpressureNanos.addAndGet((System.nanoTime() - startedAtNs).coerceAtLeast(1L))
        }
    }

    private fun closePublisherGate() {
        acceptingPublishers = false
    }

    private fun producerBuffer(): EventBuffer? {
        val currentEpoch = epoch.get()
        threadBuffer.get()?.get()?.takeIf { it.epoch == currentEpoch }?.let { return it }
        val buffer = EventBuffer(Thread.currentThread(), currentEpoch)
        producerRegistrationObserver?.invoke()
        if (!acceptingPublishers || epoch.get() != currentEpoch) return null
        buffers.add(buffer)
        if (!acceptingPublishers || epoch.get() != currentEpoch) {
            buffers.remove(buffer)
            buffer.clearAll()
            return null
        }
        threadBuffer.set(WeakReference(buffer))
        return buffer
    }

    private fun runConsumerFailOpen() {
        var methods: MethodCounterAccumulator? = null
        var logs: LogSpamAccumulator? = null
        try {
            methods = MethodCounterAccumulator(maxCounterKeys.getAsInt().coerceAtLeast(1))
            logs = LogSpamAccumulator(maxLogSpamKeys.getAsInt().coerceAtLeast(1))
            try {
                Process.setThreadPriority(EVENT_CONSUMER_PRIORITY)
            } catch (error: Throwable) {
                RuntimeHookGuard.rethrowFatal(error)
            }
            var lastFlushAtNs = System.nanoTime()
            while (running.get() || hasActiveOrBufferedEvents()) {
                consumerLoopObserver?.invoke()
                producerWake.clear()
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
                if (drained == 0 && running.get()) {
                    // A publication after wake reset leaves an unpark permit; earlier events were
                    // observed by drain. Periodic aggregation/quality/retirement uses the same clock.
                    LockSupport.parkNanos(FLUSH_INTERVAL_NS - (System.nanoTime() - lastFlushAtNs))
                }
            }
            emit(methods, logs)
            flushQuality()
            reclaimDeadBuffers()
            flushCompleted.set(flushRequest.get())
        } catch (error: Throwable) {
            closePublisherGate()
            consumerFailed.set(true)
            running.set(false)
            buffers.forEach { buffer -> buffer.owner.get()?.let(LockSupport::unpark) }
            while (hasActivePublishers()) LockSupport.parkNanos(WAIT_POLL_NS)
            val stranded = saturatingAdd(bufferedEventCount(), aggregateCount(methods, logs))
            if (stranded > 0L) recordAcceptedLoss(writerLoss, stranded)
            flushQuality()
            retireAllBuffers()
            writer = null
            RuntimeHookGuard.rethrowFatal(error)
        } finally {
            buffers.forEach { buffer -> buffer.owner.get()?.let(LockSupport::unpark) }
            if (clearRequested.get()) {
                clearStoppedState()
            } else {
                consumer = null
            }
            // A clear racing the null publication either observes it or leaves this request.
            if (clearRequested.get()) clearStoppedState()
            try {
                completion.complete()
            } finally {
                hasConsumerOwner = false
                onStopped()
            }
        }
    }

    private fun drain(methods: MethodCounterAccumulator, logs: LogSpamAccumulator): Int {
        var drained = 0
        val currentEpoch = epoch.get()
        for (buffer in buffers) {
            var fromBuffer = 0
            while (fromBuffer < BUFFER_CAPACITY) {
                val position = buffer.sequencer.tryClaimConsumer()
                if (position == SpscSlotSequencer.NO_POSITION) break
                val slot = buffer.sequencer.slotIndex(position)
                if (buffer.epochs[slot] == currentEpoch) {
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
        if (producerWake.tryRequest()) consumer?.let(LockSupport::unpark)
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
        var batch = stableCounterBatchPool.acquire()
        methods.drain { id, name, count ->
            if (batch.size == MAX_BATCH_SIZE) {
                emitMethodBatch(batch)
                batch = stableCounterBatchPool.acquire()
            }
            batch.add(id, name, count)
        }
        if (batch.size > 0) emitMethodBatch(batch) else batch.recycle()
    }

    private fun emitMethodBatch(batch: StableCounterBatch) {
        val logicalCount = batch.logicalEventCount()
        val admitted = try {
            writer?.stableCounters(batch) == true
        } catch (error: Throwable) {
            RuntimeHookGuard.rethrowFatal(error)
            false
        }
        if (admitted) {
            emitted.addAndGet(logicalCount)
        } else {
            batch.recycle()
            recordAcceptedLoss(writerLoss, logicalCount)
        }
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
            } catch (error: Throwable) {
                RuntimeHookGuard.rethrowFatal(error)
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

    private fun hasActiveOrBufferedEvents(): Boolean {
        for (buffer in buffers) {
            if (buffer.producerActive || !buffer.sequencer.isEmpty()) return true
        }
        return false
    }

    private fun hasActivePublishers(): Boolean {
        for (buffer in buffers) if (buffer.producerActive) return true
        return false
    }

    private fun bufferedEventCount(): Long {
        var result = 0L
        for (buffer in buffers) {
            result = saturatingAdd(result, buffer.sequencer.pendingCount())
        }
        return result
    }

    private fun aggregateCount(methods: MethodCounterAccumulator?, logs: LogSpamAccumulator?): Long {
        return saturatingAdd(methods?.logicalEventCount() ?: 0L, logs?.logicalEventCount() ?: 0L)
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

    private fun retireAllBuffers() {
        while (true) {
            val buffer = buffers.poll() ?: return
            addSaturating(retiredProducerAccepted, buffer.acceptedCount())
            buffer.clearAll()
        }
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
        producerWake.clear()
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

        @Volatile var producerActive = false
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
            producerActive = false
            producerAccepted = 0L
        }
    }

    private companion object {
        const val CONSUMER_NAME = "JankHunterEvents"
        const val NO_FLUSH_REQUEST = -1L
        const val BUFFER_CAPACITY = 256
        const val MAX_BATCH_SIZE = 128
        const val BATCH_POOL_CAPACITY = 256
        const val TYPE_METHOD: Byte = 1
        const val TYPE_LOG_SPAM: Byte = 2
        const val WAIT_POLL_NS = 1_000_000L
        const val JOIN_POLL_MS = 50L
        const val FLUSH_INTERVAL_NS = 5_000_000_000L
        const val BACKPRESSURE_PARK_NS = 100_000L
        const val DEFAULT_ADMISSION_WAIT_NS = 5_000_000L
        const val EVENT_CONSUMER_PRIORITY = Process.THREAD_PRIORITY_DEFAULT + 1
    }
}
