package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterBinaryStorage
import io.jankhunter.runtime.JankHunterConfig
import io.jankhunter.runtime.internal.concurrent.CoalescedWakeSignal
import java.io.File
import java.util.concurrent.Semaphore
import java.util.concurrent.locks.ReentrantLock

private const val DATABASE_POOL_CAPACITY = 1_024
private const val RUNTIME_CALL_POOL_CAPACITY = 256
private const val STABLE_COUNTER_POOL_CAPACITY = 256
private const val AGENT_BATCH_POOL_CAPACITY = 64

/** Producer-owned transport state. No field in this object is touched by session I/O code. */
internal class AsyncWriterProducer(config: JankHunterConfig) {
    private val workerWake = CoalescedWakeSignal()
    @JvmField
    val queuedEvents = Semaphore(0)
    @JvmField
    val admissionLock = ReentrantLock()
    @JvmField
    val context = ProducerContextTracker()
    @JvmField
    val eventLanes = AsyncEventLanes(config.maxQueueSize())
    @JvmField
    val databaseEventPool = PendingDatabaseEventPool(
        minOf(config.maxQueueSize(), DATABASE_POOL_CAPACITY),
    )
    @JvmField
    val databaseTransactionEventPool = PendingDatabaseTransactionEventPool(
        minOf(config.maxQueueSize(), DATABASE_POOL_CAPACITY),
    )
    @JvmField
    val runtimeCallsEventPool = PendingRuntimeCallsEventPool(
        minOf(config.maxQueueSize(), RUNTIME_CALL_POOL_CAPACITY),
    )
    @JvmField
    val stableCountersEventPool = PendingStableCountersEventPool(
        minOf(config.maxQueueSize(), STABLE_COUNTER_POOL_CAPACITY),
    )
    @JvmField
    val agentBatchEventPool = PendingAgentBatchEventPool(
        minOf(config.maxQueueSize(), AGENT_BATCH_POOL_CAPACITY),
    )
    @JvmField
    var acceptedSequence = 0L

    fun requestWorkerWake(): Boolean {
        if (!workerWake.tryRequest()) return false
        queuedEvents.release()
        return true
    }

    fun prepareWorkerWait() {
        // A wake may arrive after its event was already consumed. Drain old permits before
        // disarming coalescing; otherwise that late wake can leave pending=true with no permit,
        // suppressing every later producer notification until periodic flush.
        queuedEvents.drainPermits()
        workerWake.clear()
    }
}

/** Consumer-owned session and file state. It is mutated only by the writer worker. */
internal class AsyncWriterConsumer(
    directory: File,
    config: JankHunterConfig,
    processName: String,
    sessionLocalDate: String,
    quality: LogQualityCounters,
) {
    @JvmField
    @Volatile
    var binaryStorage: JankHunterBinaryStorage? = config.binaryStorage()

    @JvmField
    var completedSequence = 0L
    @JvmField
    val segmentLedger = SessionSegmentLedger(directory, processName)
    @JvmField
    var writer: BinaryLogWriter? = null
    @JvmField
    var runCohortLease: ProcessRunCohort.Lease? = null
    @JvmField
    var runId = ByteArray(0)
    @JvmField
    var runLocalDate = sessionLocalDate
    @JvmField
    var dailySessionIndex = 0L
    @JvmField
    val sessionId = BinaryLogFileHeader.randomId()
    @JvmField
    var segmentIndex = 0L
    @JvmField
    var previousSegmentDigest = ByteArray(0)
    @JvmField
    var completedSegmentStats = LogContainerStats.EMPTY
    @JvmField
    var lastFlushAtNs = System.nanoTime()
    @JvmField
    val runtimeHookFailures = RuntimeHookFailureQualitySynchronizer(quality)
    @JvmField
    val retention = SessionLogRetentionCoordinator(directory, config, quality)

    @JvmField
    @Volatile
    var worker: Thread? = null
}
