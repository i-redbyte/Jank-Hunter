package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertEquals
import org.junit.Test

class CoroutineSegmentPayloadTest {
    @Test fun segmentUsesDedicatedSemanticRecord() {
        val value = CoroutineSegmentObservation(81, 4, 11, 11, 1011, 1011, 100, 120, 0, "app.FeedRepository.load")
        assertEquals(Jhlog.TYPE_COROUTINE_SEGMENT, PendingCoroutineSegment(null, value).recordType)
        encodeCoroutineSegment(BinaryPayload(), value)
    }

    @Test fun runningObservationHasNoInventedExit() {
        encodeCoroutineSegment(BinaryPayload(), CoroutineSegmentObservation(
            81, 4, 11, 0, 1011, 0, 100, 120, 4, "app.FeedRepository.load",
        ))
    }

    @Test(expected = IllegalArgumentException::class)
    fun runningObservationCannotClaimAnExitThread() {
        encodeCoroutineSegment(BinaryPayload(), CoroutineSegmentObservation(
            81, 4, 11, 11, 1011, 1011, 100, 120, 4, "app.FeedRepository.load",
        ))
    }

    @Test(expected = IllegalArgumentException::class)
    fun impossibleIntervalIsRejected() {
        encodeCoroutineSegment(BinaryPayload(), CoroutineSegmentObservation(1, 1, 11, 11, 1011, 1011, 20, 10, 0, "source"))
    }

    @Test(expected = IllegalArgumentException::class)
    fun oversizedSourceIsRejected() {
        encodeCoroutineSegment(BinaryPayload(), CoroutineSegmentObservation(1, 1, 11, 11, 1011, 1011, 10, 20, 0,
            "x".repeat(513)))
    }

    @Test(expected = IllegalStateException::class)
    fun type37CannotEnterFiveBitMicroPage() {
        ColumnarMicroPage().append(Jhlog.TYPE_COROUTINE_SEGMENT, 0, BinaryPayload(), null, null, 1)
    }
}
