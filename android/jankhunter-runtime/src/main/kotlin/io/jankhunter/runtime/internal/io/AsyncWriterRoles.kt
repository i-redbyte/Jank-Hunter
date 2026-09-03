package io.jankhunter.runtime.internal.io

import android.os.SystemClock
import io.jankhunter.runtime.JankHunterBinaryStorage
import io.jankhunter.runtime.JankHunterConfig
import java.io.File
import java.util.concurrent.Semaphore
import java.util.concurrent.locks.ReentrantLock

private const val DATABASE_POOL_CAPACITY = 1_024
private const val RUNTIME_CALL_POOL_CAPACITY = 256
private const val STABLE_COUNTER_POOL_CAPACITY = 256

/** Producer-owned transport state. No field in this object is touched by session I/O code. */
internal class AsyncWriterProducer(config: JankHunterConfig) {
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
    var acceptedSequence = 0L
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
    var lastFlushAtMs = SystemClock.elapsedRealtime()
    @JvmField
    val runtimeHookFailures = RuntimeHookFailureQualitySynchronizer(quality)
    @JvmField
    val retention = SessionLogRetentionCoordinator(directory, config, quality)

    @JvmField
    @Volatile
    var worker: Thread? = null
}
