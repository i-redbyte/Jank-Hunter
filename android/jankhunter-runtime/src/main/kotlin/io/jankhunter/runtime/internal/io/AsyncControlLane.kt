package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterBinaryStorage
import io.jankhunter.runtime.JankHunterStorageSwitchResult
import io.jankhunter.runtime.RuntimeHookGuard
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicIntegerFieldUpdater

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

    fun remove(request: AsyncControlRequest): Boolean = queue.remove(request)

    fun hasPending(): Boolean = queue.isNotEmpty()
}

internal class AsyncControlRequest(
    val targetSequence: Long,
    val writeLogGrowth: Boolean,
    val requireLogGrowth: Boolean = false,
    val sealSnapshot: Boolean = false,
    val storageSwitch: StorageSwitchRequest? = null,
    blocking: Boolean,
    private val onCompleted: ((AsyncControlRequest) -> Unit)? = null,
) {
    private val completion = if (blocking) CountDownLatch(1) else null

    @JvmSynthetic
    @JvmField
    @Volatile
    internal var lifecycleState = STATE_PENDING

    var snapshot: LogSnapshotResult? = null
    var storageSwitchResult: JankHunterStorageSwitchResult = JankHunterStorageSwitchResult.FAILED

    @Volatile
    var succeeded = false
        private set

    fun tryClaim(): Boolean = STATE.compareAndSet(this, STATE_PENDING, STATE_CLAIMED)

    fun cancel(): Boolean {
        if (!STATE.compareAndSet(this, STATE_PENDING, STATE_CANCELLED)) return false
        completion?.countDown()
        return true
    }

    fun complete(success: Boolean) {
        if (!STATE.compareAndSet(this, STATE_CLAIMED, STATE_COMPLETED)) return
        succeeded = success
        completion?.countDown()
        try {
            onCompleted?.invoke(this)
        } catch (error: Throwable) {
            RuntimeHookGuard.rethrowFatal(error)
            // Completion observers are diagnostic state reconciliation and must not kill the writer.
        }
    }

    @Throws(InterruptedException::class)
    fun await(timeoutNs: Long): Boolean = completion?.await(timeoutNs, TimeUnit.NANOSECONDS) ?: true

    fun isComplete(): Boolean = completion?.count == 0L

    fun isInProgress(): Boolean {
        val current = lifecycleState
        return current == STATE_CLAIMED || current == STATE_COMPLETED && !isComplete()
    }

    private companion object {
        const val STATE_PENDING = 0
        const val STATE_CLAIMED = 1
        const val STATE_COMPLETED = 2
        const val STATE_CANCELLED = 3

        val STATE = AtomicIntegerFieldUpdater.newUpdater(
            AsyncControlRequest::class.java,
            "lifecycleState",
        )
    }
}

internal class StorageSwitchRequest(val storage: JankHunterBinaryStorage?)

internal data class LogSnapshotResult(
    val capturedAtMs: Long,
    val logPaths: List<String>,
)
