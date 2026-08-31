package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterBinaryStorage
import io.jankhunter.runtime.JankHunterStorageSwitchResult
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

/** Bounded, allocation-stable command lane between synchronous callers and the writer worker. */
internal class AsyncControlLane(capacity: Int) {
    private val queue = ArrayBlockingQueue<AsyncControlRequest>(capacity)
    private val submitters = AtomicInteger()

    fun beginSubmission() {
        submitters.incrementAndGet()
    }

    fun finishSubmission() {
        submitters.decrementAndGet()
    }

    fun hasSubmitters(): Boolean = submitters.get() > 0

    fun offer(request: AsyncControlRequest): Boolean = queue.offer(request)

    @Throws(InterruptedException::class)
    fun offer(request: AsyncControlRequest, timeout: Long, unit: TimeUnit): Boolean {
        return queue.offer(request, timeout, unit)
    }

    fun peek(): AsyncControlRequest? = queue.peek()

    fun poll(): AsyncControlRequest? = queue.poll()

    fun hasPending(): Boolean = queue.isNotEmpty()
}

internal class AsyncControlRequest(
    val targetSequence: Long,
    val writeLogGrowth: Boolean,
    val sealSnapshot: Boolean = false,
    val storageSwitch: StorageSwitchRequest? = null,
    blocking: Boolean,
) {
    private val completion = if (blocking) CountDownLatch(1) else null

    var snapshot: LogSnapshotResult? = null
    var storageSwitchResult: JankHunterStorageSwitchResult = JankHunterStorageSwitchResult.FAILED

    @Volatile
    var succeeded = false
        private set

    fun complete(success: Boolean) {
        succeeded = success
        completion?.countDown()
    }

    @Throws(InterruptedException::class)
    fun await(timeoutNs: Long): Boolean = completion?.await(timeoutNs, TimeUnit.NANOSECONDS) ?: true

    fun isComplete(): Boolean = completion?.count == 0L
}

internal class StorageSwitchRequest(val storage: JankHunterBinaryStorage?)

internal data class LogSnapshotResult(
    val capturedAtMs: Long,
    val logPaths: List<String>,
)
