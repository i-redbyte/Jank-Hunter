package io.jankhunter.runtime.internal.io

import java.util.concurrent.TimeUnit
import java.util.concurrent.locks.LockSupport

/** Admission, bounded queuing and completion waits for cold-path writer controls. */
internal class AsyncWriterControlCoordinator(
    private val exactEventCollection: Boolean,
    private val quality: LogQualityCounters,
    private val beginSubmission: (startIfNeeded: Boolean) -> Long,
    private val wakeWorker: () -> Unit,
) {
    private val lane = AsyncControlLane(CONTROL_QUEUE_CAPACITY)

    fun flush() {
        val target = beginSubmission(false)
        if (target < 0L) return
        try {
            if (!lane.offer(AsyncControlRequest(target, writeLogGrowth = false, blocking = false))) {
                quality.add(QualityCounterId.CONTROL_LANE_FULL_TOTAL)
            }
        } finally {
            lane.finishSubmission()
        }
    }

    fun submitBlocking(
        timeoutMs: Long,
        writeLogGrowth: Boolean,
        waitForExactFrontier: Boolean,
        sealSnapshot: Boolean = false,
        storageSwitch: StorageSwitchRequest? = null,
        onComplete: ((AsyncControlRequest) -> Unit)? = null,
    ): Boolean {
        val needsWorker = writeLogGrowth || sealSnapshot || storageSwitch != null
        val target = beginSubmission(needsWorker)
        if (target == NO_WORK) return !needsWorker
        if (target == NOT_ACCEPTING) return false
        if (exactEventCollection && waitForExactFrontier) {
            return submitExact(target, writeLogGrowth, sealSnapshot, storageSwitch, onComplete)
        }
        val timeoutNs = TimeUnit.MILLISECONDS.toNanos(timeoutMs.coerceAtLeast(1L))
        val startedAtNs = System.nanoTime()
        val request = AsyncControlRequest(target, writeLogGrowth, sealSnapshot, storageSwitch, blocking = true)
        val admitted = try {
            lane.offer(request, timeoutNs, TimeUnit.NANOSECONDS)
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
            quality.add(QualityCounterId.CONTROL_INTERRUPTED_TOTAL)
            return false
        } finally {
            lane.finishSubmission()
        }
        if (!admitted) {
            quality.add(QualityCounterId.CONTROL_TIMEOUT_TOTAL)
            return false
        }
        wakeWorker()

        val remainingNs = timeoutNs - (System.nanoTime() - startedAtNs).coerceAtLeast(0L)
        if (remainingNs <= 0L) {
            if (request.isComplete()) {
                onComplete?.invoke(request)
                return request.succeeded
            }
            quality.add(QualityCounterId.CONTROL_TIMEOUT_TOTAL)
            return false
        }
        val completed = try {
            request.await(remainingNs)
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
            quality.add(QualityCounterId.CONTROL_INTERRUPTED_TOTAL)
            return false
        }
        if (!completed) {
            quality.add(QualityCounterId.CONTROL_TIMEOUT_TOTAL)
            return false
        }
        onComplete?.invoke(request)
        return request.succeeded
    }

    fun beginLaneSubmission() = lane.beginSubmission()

    fun hasPending(): Boolean = lane.hasPending()

    fun hasSubmitters(): Boolean = lane.hasSubmitters()

    fun peek(): AsyncControlRequest? = lane.peek()

    fun poll(): AsyncControlRequest? = lane.poll()

    fun failPending() {
        while (true) {
            val request = lane.poll() ?: return
            request.complete(success = false)
        }
    }

    fun failAfterAdmissionClosed() {
        while (lane.hasSubmitters()) {
            failPending()
            LockSupport.parkNanos(CONTROL_DRAIN_PARK_NS)
        }
        failPending()
    }

    private fun submitExact(
        target: Long,
        writeLogGrowth: Boolean,
        sealSnapshot: Boolean,
        storageSwitch: StorageSwitchRequest?,
        onComplete: ((AsyncControlRequest) -> Unit)?,
    ): Boolean {
        val request = AsyncControlRequest(target, writeLogGrowth, sealSnapshot, storageSwitch, blocking = true)
        var interrupted = false
        try {
            var admitted = false
            while (!admitted) {
                try {
                    admitted = lane.offer(request, CONTROL_WAIT_POLL_MS, TimeUnit.MILLISECONDS)
                } catch (_: InterruptedException) {
                    interrupted = true
                }
            }
        } finally {
            lane.finishSubmission()
        }
        wakeWorker()
        while (!request.isComplete()) {
            try {
                request.await(TimeUnit.MILLISECONDS.toNanos(CONTROL_WAIT_POLL_MS))
            } catch (_: InterruptedException) {
                interrupted = true
            }
        }
        if (interrupted) Thread.currentThread().interrupt()
        onComplete?.invoke(request)
        return request.succeeded
    }

    companion object {
        internal const val NOT_ACCEPTING = -2L
        internal const val NO_WORK = -1L
        private const val CONTROL_QUEUE_CAPACITY = 16
        private const val CONTROL_WAIT_POLL_MS = 50L
        private const val CONTROL_DRAIN_PARK_NS = 100_000L
    }
}
