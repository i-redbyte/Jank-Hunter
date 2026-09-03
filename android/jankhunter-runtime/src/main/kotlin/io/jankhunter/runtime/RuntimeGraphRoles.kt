package io.jankhunter.runtime

import io.jankhunter.runtime.internal.concurrent.CoalescedWakeSignal
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.RuntimeCallBatchPool
import java.lang.ref.WeakReference
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong

/** State written by application producer threads. */
internal class RuntimeGraphProducer {
    @JvmField
    val threadState = ThreadLocal<WeakReference<RuntimeGraphProducerState>>()
    @JvmField
    val registry = ConcurrentLinkedQueue<RuntimeGraphProducerState>()
    @JvmField
    val epoch = AtomicLong(1L)
    @JvmField
    val retiredAccepted = AtomicLong()
    @JvmField
    val retiredAttempted = AtomicLong()
    @JvmField
    val capacityLoss = AtomicLong()
    @JvmField
    val stackMismatch = AtomicLong()
    @JvmField
    val backpressureCount = AtomicLong()
    @JvmField
    val backpressureNanos = AtomicLong()
}

/** State written by the single graph consumer thread. */
internal class RuntimeGraphConsumer(batchPoolCapacity: Int, batchCapacity: Int) {
    @JvmField
    val emitted = AtomicLong()
    @JvmField
    val aggregatedEdgeKeys = AtomicLong()
    @JvmField
    val writerRejectionLoss = AtomicLong()
    @JvmField
    val batchPool = RuntimeCallBatchPool(batchPoolCapacity, batchCapacity)
}

/** Cross-thread lifecycle gates and flush frontiers. */
internal class RuntimeGraphLifecycle {
    @JvmField
    val running = AtomicBoolean(false)
    @JvmField
    val consumerFailed = AtomicBoolean(false)
    @JvmField
    val producerWake = CoalescedWakeSignal()
    @JvmField
    val acceptedEventLoss = AtomicLong()
    @JvmField
    val shutdownLoss = AtomicLong()
    @JvmField
    val flushRequest = AtomicLong()
    @JvmField
    val flushCompleted = AtomicLong()
    @JvmField
    val reportedAttempted = AtomicLong()
    @JvmField
    val reportedEmitted = AtomicLong()

    @JvmField
    @Volatile
    var consumerThread: Thread? = null

    @JvmField
    @Volatile
    var activeWriter: AsyncLogWriter? = null

    @JvmField
    @Volatile
    var acceptingPublishers = false
}

internal class RuntimeGraphProducerState(
    val epoch: Long,
    val stack: RuntimeCallStack,
    val buffer: RuntimeGraphAggregateBuffer,
)
