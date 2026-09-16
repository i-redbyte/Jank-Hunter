package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.internal.monotonicDeadlineAfterMillis
import java.util.concurrent.TimeUnit
import java.util.concurrent.locks.LockSupport

/** Admission, bounded queuing and completion waits for cold-path writer controls. */
internal class AsyncWriterControlCoordinator(
    private val quality: LogQualityCounters,
    private val beginSubmission: (startIfNeeded: Boolean, deadlineNs: Long) -> Long,
    private val wakeWorker: () -> Unit,
) {
    private val lane = AsyncControlLane(CONTROL_QUEUE_CAPACITY)

    fun flush() {
        val target = try {
            beginSubmission(false, System.nanoTime())
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
            quality.add(QualityCounterId.CONTROL_INTERRUPTED_TOTAL)
            return
        }
        if (target == ADMISSION_TIMED_OUT) quality.add(QualityCounterId.CONTROL_TIMEOUT_TOTAL)
        if (target < 0L) return
        var admitted = false
        try {
            admitted = lane.offer(AsyncControlRequest(target, writeLogGrowth = false, blocking = false))
            if (!admitted) {
                quality.add(QualityCounterId.CONTROL_LANE_FULL_TOTAL)
            }
        } finally {
            lane.finishSubmission()
        }
        if (admitted) wakeWorker()
    }

    fun submitBlocking(
        timeoutMs: Long,
        writeLogGrowth: Boolean,
        requireLogGrowth: Boolean = false,
        sealSnapshot: Boolean = false,
        storageSwitch: StorageSwitchRequest? = null,
        onComplete: ((AsyncControlRequest) -> Unit)? = null,
    ): Boolean {
        return submitBlockingOutcome(
            timeoutMs,
            writeLogGrowth,
            requireLogGrowth,
            sealSnapshot,
            storageSwitch,
            onComplete,
        ) == AsyncControlSubmissionOutcome.SUCCEEDED
    }

    fun submitBlockingOutcome(
        timeoutMs: Long,
        writeLogGrowth: Boolean,
        requireLogGrowth: Boolean = false,
        sealSnapshot: Boolean = false,
        storageSwitch: StorageSwitchRequest? = null,
        onComplete: ((AsyncControlRequest) -> Unit)? = null,
        onDeferredComplete: ((AsyncControlRequest) -> Unit)? = null,
    ): AsyncControlSubmissionOutcome {
        val deadlineNs = monotonicDeadlineAfterMillis(timeoutMs.coerceAtLeast(0L))
        val needsWorker = writeLogGrowth || sealSnapshot || storageSwitch != null
        val target = try {
            beginSubmission(needsWorker, deadlineNs)
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
            quality.add(QualityCounterId.CONTROL_INTERRUPTED_TOTAL)
            return AsyncControlSubmissionOutcome.TIMED_OUT
        }
        if (target == ADMISSION_TIMED_OUT) {
            quality.add(QualityCounterId.CONTROL_TIMEOUT_TOTAL)
            return AsyncControlSubmissionOutcome.TIMED_OUT
        }
        if (target == NO_WORK) {
            return if (needsWorker) AsyncControlSubmissionOutcome.FAILED else AsyncControlSubmissionOutcome.SUCCEEDED
        }
        if (target == NOT_ACCEPTING) return AsyncControlSubmissionOutcome.FAILED
        val request = AsyncControlRequest(
            target,
            writeLogGrowth,
            requireLogGrowth,
            sealSnapshot,
            storageSwitch,
            blocking = true,
            onCompleted = onDeferredComplete,
        )
        val admitted = try {
            lane.offer(request, (deadlineNs - System.nanoTime()).coerceAtLeast(0L), TimeUnit.NANOSECONDS)
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
            quality.add(QualityCounterId.CONTROL_INTERRUPTED_TOTAL)
            return AsyncControlSubmissionOutcome.TIMED_OUT
        } finally {
            lane.finishSubmission()
        }
        if (!admitted) {
            quality.add(QualityCounterId.CONTROL_TIMEOUT_TOTAL)
            return AsyncControlSubmissionOutcome.TIMED_OUT
        }
        wakeWorker()

        val remainingNs = deadlineNs - System.nanoTime()
        if (remainingNs <= 0L) {
            if (request.isComplete()) {
                onComplete?.invoke(request)
                return completedOutcome(request)
            }
            quality.add(QualityCounterId.CONTROL_TIMEOUT_TOTAL)
            return timeoutOutcome(request, onComplete)
        }
        val completed = try {
            request.await(remainingNs)
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
            quality.add(QualityCounterId.CONTROL_INTERRUPTED_TOTAL)
            return timeoutOutcome(request, onComplete)
        }
        if (!completed) {
            quality.add(QualityCounterId.CONTROL_TIMEOUT_TOTAL)
            return timeoutOutcome(request, onComplete)
        }
        onComplete?.invoke(request)
        return completedOutcome(request)
    }

    fun beginLaneSubmission() = lane.beginSubmission()

    fun hasPending(): Boolean = lane.hasPending()

    fun hasSubmitters(): Boolean = lane.hasSubmitters()

    fun claimReady(completedSequence: Long): AsyncControlRequest? {
        while (true) {
            val request = lane.peek() ?: return null
            if (completedSequence < request.targetSequence) return null
            if (!request.tryClaim()) {
                lane.remove(request)
                continue
            }
            if (lane.remove(request)) return request
            request.complete(success = false)
        }
    }

    fun failPending() {
        while (true) {
            val request = lane.poll() ?: return
            if (request.tryClaim()) request.complete(success = false)
        }
    }

    fun failAfterAdmissionClosed() {
        while (lane.hasSubmitters()) {
            failPending()
            LockSupport.parkNanos(CONTROL_DRAIN_PARK_NS)
        }
        failPending()
    }

    private fun timeoutOutcome(
        request: AsyncControlRequest,
        onComplete: ((AsyncControlRequest) -> Unit)?,
    ): AsyncControlSubmissionOutcome {
        if (request.cancel()) {
            lane.remove(request)
            return AsyncControlSubmissionOutcome.TIMED_OUT
        }
        if (request.isComplete()) {
            onComplete?.invoke(request)
            return completedOutcome(request)
        }
        return if (request.isInProgress()) {
            AsyncControlSubmissionOutcome.IN_PROGRESS
        } else {
            AsyncControlSubmissionOutcome.TIMED_OUT
        }
    }

    private fun completedOutcome(request: AsyncControlRequest): AsyncControlSubmissionOutcome =
        if (request.succeeded) AsyncControlSubmissionOutcome.SUCCEEDED else AsyncControlSubmissionOutcome.FAILED

    companion object {
        internal const val ADMISSION_TIMED_OUT = -3L
        internal const val NOT_ACCEPTING = -2L
        internal const val NO_WORK = -1L
        private const val CONTROL_QUEUE_CAPACITY = 16
        private const val CONTROL_DRAIN_PARK_NS = 100_000L
    }
}

internal enum class AsyncControlSubmissionOutcome {
    SUCCEEDED,
    FAILED,
    TIMED_OUT,
    IN_PROGRESS,
}
